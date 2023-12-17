/*
Copyright © 2022 NAME HERE <EMAIL ADDRESS>
*/
package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"

	"github.com/pkg/errors"

	"github.com/spf13/cobra"

	"github.com/meln5674/gosh"
	"github.com/meln5674/rflag"

	"github.com/meln5674/kink/pkg/kubectl"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	clientcmdv1 "k8s.io/client-go/tools/clientcmd/api/v1"
	"k8s.io/klog/v2"
	"sigs.k8s.io/yaml"
)

// exportKubeconfigCmd represents the export kubeconfig command
var exportKubeconfigCmd = &cobra.Command{
	Use:          "kubeconfig",
	Short:        "Exports cluster kubeconfig",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		return exportKubeconfigToPath(context.Background(), &exportKubeconfigArgs, &resolvedConfig)
	},
}

type exportKubeconfigCommonArgsT struct {
	ControlplaneIngressURL string `rflag:"usage=If ingress is used for the controlplane,, instead use this URL,, and set the tls-server-name to the expected ingress hostname. Ignored if controlplane ingress is not used."`
}

func (exportKubeconfigCommonArgsT) Defaults() exportKubeconfigCommonArgsT {
	return exportKubeconfigCommonArgsT{}
}

type exportKubeconfigArgsT struct {
	Common                 exportKubeconfigCommonArgsT `rflag:""`
	KubeconfigToExportPath string                      `rflag:"name=out-kubeconfig,usage=Path to export kubeconfig to"`
}

func (exportKubeconfigArgsT) Defaults() exportKubeconfigArgsT {
	return exportKubeconfigArgsT{
		KubeconfigToExportPath: "./kink.kubeconfig",
		Common:                 exportKubeconfigCommonArgsT{}.Defaults(),
	}
}

var exportKubeconfigArgs = exportKubeconfigArgsT{}.Defaults()

func init() {
	exportCmd.AddCommand(exportKubeconfigCmd)
	rflag.MustRegister(rflag.ForPFlag(exportKubeconfigCmd.Flags()), "", &exportKubeconfigArgs)
}

func exportKubeconfig(ctx context.Context, w io.Writer, args *exportKubeconfigCommonArgsT, cfg *resolvedConfigT) error {
	f, err := os.CreateTemp("", "*-kubeconfig")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	err = f.Close()
	if err != nil {
		return err
	}

	err = fetchKubeconfig(ctx, cfg, f.Name())
	if err != nil {
		return err
	}
	return buildAndWriteCompleteKubeconfig(
		ctx, cfg,
		f.Name(), w,
		cfg.ReleaseConfig.ControlplaneHostname, "api", "controlplane", int(cfg.ReleaseConfig.ControlplanePort),
		args.ControlplaneIngressURL, "",
	)
}

func exportKubeconfigToPath(ctx context.Context, args *exportKubeconfigArgsT, cfg *resolvedConfigT) error {
	f, err := os.Create(args.KubeconfigToExportPath)
	if err != nil {
		return err
	}
	defer f.Close()
	return exportKubeconfig(ctx, f, &args.Common, cfg)
}

func externalControlplaneURL(ctx context.Context, cfg *resolvedConfigT, hostname, nodeportName, errName string, serverURLOverride, tlsServerNameOverride string) (serverURL string, tlsServerName string, _ error) {
	if hostname == "" {
		return "", "", fmt.Errorf("Neither ingress nor a nodeport host has been set for the %s, kubeconfig will not have an external context", errName)
	}
	if cfg.ReleaseConfig.ControlplaneIsNodePort {
		k8sClient, err := kubernetes.NewForConfig(cfg.Kubeconfig)
		if err != nil {
			return "", "", errors.Wrapf(err, "Could not retrieve %s service to get nodePort, kubeconfig will not have an external context", errName)
		}
		port, err := getNodePort(ctx, k8sClient, cfg, cfg.ReleaseNamespace, cfg.ReleaseConfig.ControlplaneFullname, nodeportName, errName)
		if err != nil {
			return "", "", errors.Wrapf(err, "Could not retrieve %s service to get nodePort, kubeconfig will not have an external context", errName)
		}
		if port == 0 {
			return "", "", fmt.Errorf("%s service has not been assigned a NodePort yet, kubeconfig will not have an external context", errName)
		}

		return fmt.Sprintf("https://%s:%d", hostname, port), hostname, nil
	}
	serverURL = serverURLOverride
	tlsServerName = tlsServerNameOverride
	if serverURL == "" {
		serverURL = fmt.Sprintf("https://%s", hostname)
	}
	if tlsServerName == "" {
		if hostname == "" {
			tlsServerName = cfg.ReleaseConfig.ControlplaneFullname
		} else {
			tlsServerName = hostname
		}
	}

	return serverURL, tlsServerName, nil
}

func buildCompleteKubeconfig(ctx context.Context, cfg *resolvedConfigT, path string, hostname, nodeportName, errName string, port int, serverURL, tlsServerName string) (*clientcmdapi.Config, error) {
	exportedKubeconfig, err := clientcmd.LoadFromFile(path)
	if err != nil {
		return nil, err
	}
	defaultCluster, ok := exportedKubeconfig.Clusters["default"]
	if !ok {
		return nil, fmt.Errorf("Extracted kubeconfig did not contain expected cluster")
	}
	inClusterCluster := defaultCluster.DeepCopy()
	inClusterHostname := fmt.Sprintf("%s.%s.svc.cluster.local", cfg.ReleaseConfig.ControlplaneFullname, cfg.ReleaseNamespace)
	inClusterURL := fmt.Sprintf("https://%s:%v", inClusterHostname, port)
	inClusterCluster.Server = inClusterURL
	inClusterCluster.TLSServerName = inClusterHostname

	exportedKubeconfig.Clusters["in-cluster"] = inClusterCluster

	defaultContext, ok := exportedKubeconfig.Contexts["default"]
	if !ok {
		return nil, fmt.Errorf("Extracted kubeconfig did not contain expected context")
	}

	inClusterContext := defaultContext.DeepCopy()
	inClusterContext.Cluster = "in-cluster"
	exportedKubeconfig.Contexts["in-cluster"] = inClusterContext

	externalClusterURL, externalClusterTLSServerName, err := externalControlplaneURL(
		ctx, cfg,
		hostname, nodeportName, errName, serverURL, tlsServerName,
	)
	if err != nil {
		klog.Warning(err.Error())
	}
	if externalClusterURL != "" {
		externalCluster := defaultCluster.DeepCopy()
		externalCluster.Server = externalClusterURL
		externalCluster.TLSServerName = externalClusterTLSServerName
		exportedKubeconfig.Clusters["external"] = externalCluster
		externalContext := defaultContext.DeepCopy()
		externalContext.Cluster = "external"
		exportedKubeconfig.Contexts["external"] = externalContext
		exportedKubeconfig.CurrentContext = "external"
	}

	return exportedKubeconfig, nil
}

func buildAndWriteCompleteKubeconfig(ctx context.Context, cfg *resolvedConfigT, path string, w io.Writer, hostname, nodeportName, errName string, port int, serverURL, tlsServerName string) error {
	exportedKubeconfig, err := buildCompleteKubeconfig(ctx, cfg, path, hostname, nodeportName, errName, port, serverURL, tlsServerName)
	if err != nil {
		return err
	}

	klog.V(4).Infof("Saving exported kubeconfig to %#v", exportedKubeconfig)

	var serializableKubeconfig clientcmdv1.Config
	err = clientcmdv1.Convert_api_Config_To_v1_Config(exportedKubeconfig, &serializableKubeconfig, nil)
	if err != nil {
		return err
	}
	bytes, err := yaml.Marshal(&serializableKubeconfig)
	if err != nil {
		return err
	}
	_, err = w.Write(bytes)
	if err != nil {
		return err
	}
	return nil
}

func fetchKubeconfig(ctx context.Context, cfg *resolvedConfigT, path string) error {
	kubeconfigPath := k3sKubeconfigPath
	if cfg.ReleaseConfig.RKE2Enabled {
		kubeconfigPath = rke2KubeconfigPath
	}
	// Absolute windows paths confuse kubectl, so we turn it into a relative path
	if runtime.GOOS == "windows" && filepath.IsAbs(path) {
		pwd, err := os.Getwd()
		if err != nil {
			return errors.Wrap(err, "get pwd")
		}
		rel, err := filepath.Rel(pwd, path)
		if err != nil {
			return errors.Wrap(err, fmt.Sprintf("converting %s to a relative path to %s", path, pwd))
		}
		path = rel
	}
	// TODO: Find a live pod first
	kubectlCp := kubectl.Cp(&cfg.KinkConfig.Kubectl, &cfg.KinkConfig.Kubernetes, fmt.Sprintf("%s-0", cfg.ReleaseConfig.ControlplaneFullname), kubeconfigPath, path)
	err := gosh.
		Command(kubectlCp...).
		WithContext(ctx).
		WithStreams(gosh.ForwardOutErr).
		Run()
	if err != nil {
		return errors.Wrap(err, "Could not extract kubeconfig from controlplane pod, make sure controlplane is healthy")
	}
	return nil
}

func getNodePort(ctx context.Context, k8sClient *kubernetes.Clientset, cfg *resolvedConfigT, namespace, name, portName, errName string) (int32, error) {
	svc, err := k8sClient.CoreV1().Services(namespace).Get(ctx, cfg.ReleaseConfig.ControlplaneFullname, metav1.GetOptions{})
	if err != nil {
		return 0, err
	}
	for _, portElem := range svc.Spec.Ports {
		if portElem.Name == portName {
			return portElem.NodePort, nil
		}
	}

	return 0, fmt.Errorf("%s service %s/%s has no port %s", errName, namespace, name, portName)
}

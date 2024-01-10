/*
Copyright © 2022 NAME HERE <EMAIL ADDRESS>
*/
package cmd

import (
	"context"
	"fmt"
	"io"
	"net/url"
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
	Use:   "kubeconfig",
	Short: "Exports cluster kubeconfig",
	Long: `This command will extract the k3s.yaml or rke2.yaml kubeconfig, and modify it to contain the following contexts:

* default: Use localhost, expect a port-forward command to be running.
* in-cluster: Use the FQDN (svc-name.namespace.svc.cluster.local) of the controlplane service, expect to be running in the host cluster.
* external: If ingress is enabled, use the ingress hostname and assume port 443. If nodeport is enabled, retrieve the allocated nodeport, and use the provided host.
            Omitted if neither ingress nor nodeport is enabled for the controlplane.

All contexts use the same authentication and TLS information, and the tls-server-name will be set as appropriate.
`,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		return exportKubeconfigToPath(context.Background(), &exportKubeconfigArgs, &resolvedConfig)
	},
}

type exportKubeconfigCommonArgsT struct {
	ControlplaneIngressURL string           `rflag:"usage=If ingress is used for the controlplane,, instead use this URL,, and set the tls-server-name to the expected ingress hostname. Ignored if controlplane ingress is not used."`
	PortForward            portForwardArgsT `rflag:""`
	InCluster              bool             `rflag:"usage=If present,, the generated kubeconfig will use the in-cluster context intead of default or external"`
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
		&kubeconfigBuilderArgs{
			errName:           "controlplane",
			externalHostname:  cfg.ReleaseConfig.ControlplaneHostname,
			inClusterPort:     int(cfg.ReleaseConfig.ControlplanePort),
			portForwardPort:   args.PortForward.ControlplanePort,
			serverURLOverride: args.ControlplaneIngressURL,
			nodeportName:      "api",
			inCluster:         args.InCluster,
		},
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

type kubeconfigBuilderArgs struct {
	errName               string
	externalHostname      string
	nodeportName          string
	inClusterPort         int
	portForwardPort       int
	serverURLOverride     string
	tlsServerNameOverride string
	inCluster             bool
}

func externalControlplaneURL(ctx context.Context, cfg *resolvedConfigT, args *kubeconfigBuilderArgs) (serverURL string, tlsServerName string, _ error) {
	if args.externalHostname == "" {
		return "", "", fmt.Errorf("Neither ingress nor a nodeport host has been set for the %s, kubeconfig will not have an external context", args.errName)
	}
	if cfg.ReleaseConfig.ControlplaneIsNodePort {
		k8sClient, err := kubernetes.NewForConfig(cfg.Kubeconfig)
		if err != nil {
			return "", "", errors.Wrapf(err, "Could not retrieve %s service to get nodePort, kubeconfig will not have an external context", args.errName)
		}
		port, err := getNodePort(ctx, k8sClient, cfg, cfg.ReleaseNamespace, cfg.ReleaseConfig.ControlplaneFullname, args.nodeportName, args.errName)
		if err != nil {
			return "", "", errors.Wrapf(err, "Could not retrieve %s service to get nodePort, kubeconfig will not have an external context", args.errName)
		}
		if port == 0 {
			return "", "", fmt.Errorf("%s service has not been assigned a NodePort yet, kubeconfig will not have an external context", args.errName)
		}

		return fmt.Sprintf("https://%s:%d", args.externalHostname, port), args.externalHostname, nil
	}
	serverURL = args.serverURLOverride
	tlsServerName = args.tlsServerNameOverride
	if serverURL == "" {
		serverURL = fmt.Sprintf("https://%s", args.externalHostname)
	}
	if tlsServerName == "" {
		if args.externalHostname == "" {
			tlsServerName = cfg.ReleaseConfig.ControlplaneFullname
		} else {
			tlsServerName = args.externalHostname
		}
	}

	return serverURL, tlsServerName, nil
}

func buildCompleteKubeconfig(ctx context.Context, cfg *resolvedConfigT, path string, args *kubeconfigBuilderArgs) (*clientcmdapi.Config, error) {
	exportedKubeconfig, err := clientcmd.LoadFromFile(path)
	if err != nil {
		return nil, err
	}
	defaultCluster, ok := exportedKubeconfig.Clusters["default"]
	if !ok {
		return nil, fmt.Errorf("Extracted kubeconfig did not contain expected cluster")
	}
	defaultContext, ok := exportedKubeconfig.Contexts["default"]
	if !ok {
		return nil, fmt.Errorf("Extracted kubeconfig did not contain expected context")
	}

	if args.portForwardPort != 0 {
		defaultClusterURL, err := url.Parse(defaultCluster.Server)
		if err != nil {
			return nil, errors.Wrap(err, "Provided kubeconfig had invalid default cluster server URL")
		}
		defaultClusterURL.Host = fmt.Sprintf("%s:%d", defaultClusterURL.Hostname(), args.portForwardPort)
		defaultCluster.Server = defaultClusterURL.String()
		exportedKubeconfig.Clusters["default"] = defaultCluster
	}

	inClusterCluster := defaultCluster.DeepCopy()
	inClusterHostname := fmt.Sprintf("%s.%s.svc.cluster.local", cfg.ReleaseConfig.ControlplaneFullname, cfg.ReleaseNamespace)
	inClusterURL := fmt.Sprintf("https://%s:%v", inClusterHostname, args.inClusterPort)
	inClusterCluster.Server = inClusterURL
	inClusterCluster.TLSServerName = inClusterHostname

	exportedKubeconfig.Clusters["in-cluster"] = inClusterCluster

	inClusterContext := defaultContext.DeepCopy()
	inClusterContext.Cluster = "in-cluster"
	exportedKubeconfig.Contexts["in-cluster"] = inClusterContext

	externalClusterURL, externalClusterTLSServerName, err := externalControlplaneURL(ctx, cfg, args)
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
	if args.inCluster {
		exportedKubeconfig.CurrentContext = "in-cluster"
	}

	return exportedKubeconfig, nil
}

func saveKubeconfig(w io.Writer, kubeconfig *clientcmdapi.Config) error {
	var serializableKubeconfig clientcmdv1.Config
	err := clientcmdv1.Convert_api_Config_To_v1_Config(kubeconfig, &serializableKubeconfig, nil)
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

func buildAndWriteCompleteKubeconfig(ctx context.Context, cfg *resolvedConfigT, path string, w io.Writer, args *kubeconfigBuilderArgs) error {
	exportedKubeconfig, err := buildCompleteKubeconfig(ctx, cfg, path, args)
	if err != nil {
		return err
	}

	klog.V(4).Infof("Saving exported kubeconfig to %s", path)

	return saveKubeconfig(w, exportedKubeconfig)
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

	return 0, fmt.Errorf("%s service %s/%s has no port '%s'", errName, namespace, name, portName)
}

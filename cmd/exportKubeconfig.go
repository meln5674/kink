/*
Copyright © 2022 NAME HERE <EMAIL ADDRESS>
*/
package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/pkg/errors"

	"github.com/spf13/cobra"

	"github.com/meln5674/gosh"

	"github.com/meln5674/kink/pkg/kubectl"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	clientcmdv1 "k8s.io/client-go/tools/clientcmd/api/v1"
	"k8s.io/klog/v2"
	"sigs.k8s.io/yaml"
)

var (
	kubeconfigToExportPath string
	controlplaneIngressURL string
)

// exportKubeconfigCmd represents the exportKubeconfig command
var exportKubeconfigCmd = &cobra.Command{
	Use:   "kubeconfig",
	Short: "Exports cluster kubeconfig",
	Run: func(cmd *cobra.Command, args []string) {
		err := func() error {
			var err error
			ctx := context.TODO()
			err = fetchKubeconfig(ctx, kubeconfigToExportPath)
			if err != nil {
				return err
			}
			err = buildCompleteKubeconfig(ctx, kubeconfigToExportPath)
			if err != nil {
				return err
			}
			return nil
		}()

		if err != nil {
			klog.Fatal(err)
		}
	},
}

func init() {
	exportCmd.AddCommand(exportKubeconfigCmd)

	// Here you will define your flags and configuration settings.

	// Cobra supports Persistent Flags which will work for this command
	// and all subcommands, e.g.:
	// exportKubeconfigCmd.PersistentFlags().String("foo", "", "A help for foo")

	// Cobra supports local flags which will only run when this command
	// is called directly, e.g.:
	exportKubeconfigCmd.Flags().StringVar(&kubeconfigToExportPath, "out-kubeconfig", "./kink.kubeconfig", "Path to export kubeconfig to")
	exportKubeconfigCmd.Flags().StringVar(&controlplaneIngressURL, "controlplane-ingress-url", "", "If ingress is used for the controlplane, instead use this URL, and set the tls-server-name to the expected ingress hostname. Ignored if controlplane ingress is not used.")
}

func buildCompleteKubeconfig(ctx context.Context, path string) error {
	exportedKubeconfig, err := clientcmd.LoadFromFile(path)
	if err != nil {
		return err
	}
	defaultCluster, ok := exportedKubeconfig.Clusters["default"]
	if !ok {
		return fmt.Errorf("Extracted kubeconfig did not contain expected cluster")
	}
	inClusterCluster := defaultCluster.DeepCopy()
	inClusterHostname := fmt.Sprintf("%s.%s.svc.cluster.local", releaseConfig.ControlplaneFullname, releaseNamespace)
	inClusterURL := fmt.Sprintf("https://%s:%v", inClusterHostname, releaseConfig.ControlplanePort)
	inClusterCluster.Server = inClusterURL
	inClusterCluster.TLSServerName = inClusterHostname

	exportedKubeconfig.Clusters["in-cluster"] = inClusterCluster

	defaultContext, ok := exportedKubeconfig.Contexts["default"]
	if !ok {
		return fmt.Errorf("Extracted kubeconfig did not contain expected context")
	}

	inClusterContext := defaultContext.DeepCopy()
	inClusterContext.Cluster = "in-cluster"
	exportedKubeconfig.Contexts["in-cluster"] = inClusterContext

	externalCluster, err := func() (*clientcmdapi.Cluster, error) {
		if releaseConfig.ControlplaneHostname == "" {
			klog.Warningf("Neither ingress nor a nodeport host has been set for the controlplane, kubeconfig will not have an external context: %v", err)
			return nil, nil
		} else {
			externalCluster := defaultCluster.DeepCopy()
			if releaseConfig.ControlplaneIsNodePort {
				k8sClient, err := kubernetes.NewForConfig(kubeconfig)
				if err != nil {
					klog.Warningf("Could not retrieve controlplane service to get nodePort, kubeconfig will not have an external context: %v", err)
					return nil, nil
				}
				svc, err := k8sClient.CoreV1().Services(releaseNamespace).Get(ctx, releaseConfig.ControlplaneFullname, metav1.GetOptions{})
				if err != nil {
					klog.Warningf("Could not retrieve controlplane service to get nodePort, kubeconfig will not have an external context: %v", err)
					return nil, nil
				}
				var port int32
				for _, portElem := range svc.Spec.Ports {
					if portElem.Name == "api" {
						port = portElem.NodePort
						break
					}
				}
				if port == 0 {
					klog.Warningf("Controlplane service has not been assigned a NodePort yet, kubeconfig will not have an external context: %v", err)
					return nil, nil
				}
				externalCluster.Server = fmt.Sprintf("https://%s:%d", releaseConfig.ControlplaneHostname, port)
				externalCluster.TLSServerName = releaseConfig.ControlplaneHostname
			} else if controlplaneIngressURL != "" {
				externalCluster.Server = controlplaneIngressURL
				externalCluster.TLSServerName = releaseConfig.ControlplaneHostname
			} else {
				externalCluster.Server = fmt.Sprintf("https://%s", releaseConfig.ControlplaneHostname)
				externalCluster.TLSServerName = ""
			}

			return externalCluster, nil
		}
	}()
	if err != nil {
		return err
	}

	if externalCluster != nil {
		exportedKubeconfig.Clusters["external"] = externalCluster
		externalContext := defaultContext.DeepCopy()
		externalContext.Cluster = "external"
		exportedKubeconfig.Contexts["external"] = externalContext
		exportedKubeconfig.CurrentContext = "external"
	}

	klog.V(4).Infof("%#v", exportedKubeconfig)

	var serializableKubeconfig clientcmdv1.Config
	err = clientcmdv1.Convert_api_Config_To_v1_Config(exportedKubeconfig, &serializableKubeconfig, nil)
	if err != nil {
		return err
	}

	f, err := os.Create(path)
	if err != nil {
		return err
	}

	defer f.Close()
	bytes, err := yaml.Marshal(&serializableKubeconfig)
	if err != nil {
		return err
	}
	_, err = f.Write(bytes)
	if err != nil {
		return err
	}
	return nil
}

func fetchKubeconfig(ctx context.Context, path string) error {
	kubeconfigPath := k3sKubeconfigPath
	if releaseConfig.RKE2Enabled {
		kubeconfigPath = rke2KubeconfigPath
	}
	// TODO: Find a live pod first
	kubectlCp := kubectl.Cp(&config.Kubectl, &config.Kubernetes, fmt.Sprintf("%s-0", releaseConfig.ControlplaneFullname), kubeconfigPath, path)
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

/*
Copyright © 2022 NAME HERE <EMAIL ADDRESS>
*/
package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/meln5674/gosh"
	"github.com/meln5674/kink/pkg/kubectl"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdv1 "k8s.io/client-go/tools/clientcmd/api/v1"
	"k8s.io/klog/v2"
	"sigs.k8s.io/yaml"
)

var (
	externalControlplaneURL string
	kubeconfigToExportPath  string
)

// exportKubeconfigCmd represents the exportKubeconfig command
var exportKubeconfigCmd = &cobra.Command{
	Use:   "kubeconfig",
	Short: "Exports cluster kubeconfig",
	Run: func(cmd *cobra.Command, args []string) {
		err := func() error {
			var err error
			err = fetchKubeconfig(context.TODO(), kubeconfigToExportPath)
			if err != nil {
				return err
			}
			err = buildCompleteKubeconfig(kubeconfigToExportPath)
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
	exportKubeconfigCmd.Flags().StringVar(&externalControlplaneURL, "external-controlplane-url", "", "A URL external to the parent cluster which the new controlplane will be accessible at. If present, an extra context called \"external\" will be added with this URL. It is assumed that the external endpoint has the same controlplane TLS within the host cluster")
}

func buildCompleteKubeconfig(path string) error {
	kubeconfig, err := clientcmd.LoadFromFile(path)
	if err != nil {
		return err
	}
	defaultCluster, ok := kubeconfig.Clusters["default"]
	if !ok {
		return fmt.Errorf("Extracted kubeconfig did not contain expected cluster")
	}
	inClusterCluster := defaultCluster.DeepCopy()
	inClusterHostname := fmt.Sprintf("%s.%s.svc.cluster.local", releaseConfig.ControlplaneFullname, releaseNamespace)
	inClusterURL := fmt.Sprintf("https://%s:%v", inClusterHostname, releaseConfig.ControlplanePort)
	inClusterCluster.Server = inClusterURL
	inClusterCluster.TLSServerName = inClusterHostname

	kubeconfig.Clusters["in-cluster"] = inClusterCluster

	defaultContext, ok := kubeconfig.Contexts["default"]
	if !ok {
		return fmt.Errorf("Extracted kubeconfig did not contain expected context")
	}

	inClusterContext := defaultContext.DeepCopy()
	inClusterContext.Cluster = "in-cluster"
	kubeconfig.Contexts["in-cluster"] = inClusterContext

	if externalControlplaneURL != "" {
		externalCluster := defaultCluster.DeepCopy()
		externalCluster.Server = externalControlplaneURL
		externalCluster.TLSServerName = inClusterHostname
		kubeconfig.Clusters["external"] = externalCluster

		externalContext := defaultContext.DeepCopy()
		externalContext.Cluster = "external"
		kubeconfig.Contexts["external"] = externalContext
	}
	var serializableKubeconfig clientcmdv1.Config
	err = clientcmdv1.Convert_api_Config_To_v1_Config(kubeconfig, &serializableKubeconfig, nil)
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

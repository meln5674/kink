/*
Copyright © 2022 NAME HERE <EMAIL ADDRESS>

*/
package cmd

import (
	"context"
	"fmt"
	"net/url"
	"os"

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
		shCmd.Run(cmd, []string{fmt.Sprintf("cp ${KUBECONFIG} %s", kubeconfigToExportPath)})
		err := func() error {
			ctx := context.TODO()

			var err error
			err = loadConfig()
			if err != nil {
				return err
			}
			err = getReleaseValues(ctx)
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
	exportKubeconfigCmd.Flags().StringVar(&externalControlplaneURL, "external-controlplane-url", "", "A URL external to the parent cluster which the new controlplane will be accessible at. If present, an extra context called \"external\" will be added with this URL")

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
	// TODO: Resolve the namespace from the outer kubeconfig
	// inClusterHostname := fmt.Sprintf("kink-%s-controlplane.%s.svc.cluster.local", config.Release.ClusterName, config.Release.Namespace)
	inClusterHostname := fmt.Sprintf("kink-%s-controlplane", config.Release.ClusterName)
	inClusterURL := fmt.Sprintf("https://%s:%v", inClusterHostname, releaseValues["controlplane"].(map[string]interface{})["service"].(map[string]interface{})["api"].(map[string]interface{})["port"])
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
		externalControlplaneURLParsed, err := url.Parse(externalControlplaneURL)
		if err != nil {
			return err
		}

		externalCluster := defaultCluster.DeepCopy()
		externalHostname := externalControlplaneURLParsed.Hostname()
		externalCluster.Server = externalControlplaneURL
		externalCluster.TLSServerName = externalHostname
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

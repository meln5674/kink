/*
Copyright © 2022 NAME HERE <EMAIL ADDRESS>

*/
package cmd

import (
	"github.com/meln5674/gosh"
	"github.com/meln5674/kink/pkg/kubectl"
	"github.com/spf13/cobra"

	"context"
	"fmt"
	"k8s.io/klog/v2"
)

var (
	exportedKubeconfigInCluster    bool
	exportedKubeconfigHostOverride string
	exportedKubeconfigPath         string
)

// exportKubeconfigCmd represents the exportKubeconfig command
var exportKubeconfigCmd = &cobra.Command{
	Use:   "kubeconfig",
	Short: "Exports cluster kubeconfig",
	Run: func(cmd *cobra.Command, args []string) {
		shCmd.Run(cmd, []string{fmt.Sprintf("cp ${KUBECONFIG} %s", exportedKubeconfigPath)})
		err := func() error {
			ctx := context.TODO()

			var err error
			err = loadConfig()
			if err != nil {
				return err
			}
			kubeFlagsCopy := config.Kubernetes
			var setCluster []string
			if exportedKubeconfigInCluster {
				// TODO: Pull this out of the chart
				// TODO: Figure out the current namespace with the context
				setCluster = kubectl.ConfigSetCluster(&config.Kubectl, &kubeFlagsCopy, "default", map[string]string{"server": fmt.Sprintf("kink-%s.%s.svc.cluster.local:6443", config.Release.ClusterName, config.Release.Namespace)})
			}
			if exportedKubeconfigHostOverride != "" {
				setCluster = kubectl.ConfigSetCluster(&config.Kubectl, &kubeFlagsCopy, "default", map[string]string{"server": exportedKubeconfigHostOverride})
			}
			if len(setCluster) == 0 {
				return nil
			}
			err = gosh.
				Command(setCluster...).
				WithContext(ctx).
				WithStreams(gosh.ForwardOutErr).
				Run()
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
	exportKubeconfigCmd.Flags().StringVar(&exportedKubeconfigPath, "out-kubeconfig", "./kink.kubeconfig", "Path to export kubeconfig to")
	exportKubeconfigCmd.Flags().BoolVar(&exportedKubeconfigInCluster, "conrolplane-in-cluster", false, "Replace the api server address with the address to use if in the same cluster")
	exportKubeconfigCmd.Flags().StringVar(&exportedKubeconfigHostOverride, "controlplane-server", "", "Override server name for kubeconfig")

}

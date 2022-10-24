/*
Copyright © 2022 NAME HERE <EMAIL ADDRESS>

*/
package cmd

import (
	"context"
	"k8s.io/klog/v2"

	"github.com/spf13/cobra"

	"github.com/meln5674/gosh"
	"github.com/meln5674/kink/pkg/helm"
)

// createClusterCmd represents the createCluster command
var createClusterCmd = &cobra.Command{
	Use:   "cluster",
	Short: "Create a cluster in another cluster",
	Long:  `Creates an isolated Kubernetes cluster in an existing cluster using pod 'nodes'`,
	Run: func(cmd *cobra.Command, args []string) {
		err := func() error {
			ctx := context.TODO()
			var err error
			if chartFlags.IsLocalChart() {
				klog.Info("Using local chart, skipping `repo add`...")
			} else {
				klog.Info("Ensuring helm repo exists...")
				repoAdd := helm.RepoAdd(&helmFlags, &chartFlags, &releaseFlags)
				err = gosh.
					Command(repoAdd...).
					WithContext(ctx).
					WithStreams(gosh.ForwardOutErr).
					Run()
				if err != nil {
					return err
				}
			}
			klog.Info("Deploying chart...")
			helmUpgrade := helm.Upgrade(&helmFlags, &chartFlags, &releaseFlags, &kubeFlags)
			err = gosh.
				Command(helmUpgrade...).
				WithContext(ctx).
				WithStreams(gosh.ForwardOutErr).
				Run()
			if err != nil {
				return err
			}
			klog.Info("Deployed chart, your cluster is now ready to use")
			return nil
		}()
		if err != nil {
			klog.Fatal(err)
		}
	},
}

func init() {
	createCmd.AddCommand(createClusterCmd)

	// Here you will define your flags and configuration settings.

	// Cobra supports Persistent Flags which will work for this command
	// and all subcommands, e.g.:
	// createClusterCmd.PersistentFlags().String("foo", "", "A help for foo")

	// Cobra supports local flags which will only run when this command
	// is called directly, e.g.:
	// createClusterCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")
}

/*
Copyright © 2022 NAME HERE <EMAIL ADDRESS>
*/
package cmd

import (
	"context"

	"k8s.io/klog/v2"

	"github.com/meln5674/gosh"
	"github.com/meln5674/kink/pkg/helm"

	"github.com/spf13/cobra"
)

// deleteClusterCmd represents the deleteCluster command
var deleteClusterCmd = &cobra.Command{
	Use:   "cluster",
	Short: "Deletes a cluster",
	Run: func(cmd *cobra.Command, args []string) {
		err := func() error {

			ctx := context.TODO()
			var err error
			klog.Info("Deleting release...")
			// TODO: Add flag to also delete PVCs
			raw := config.Release.Raw()
			helmDelete := helm.Delete(&config.Helm, &config.Chart, &raw, &config.Kubernetes)
			err = gosh.
				Command(helmDelete...).
				WithContext(ctx).
				WithStreams(gosh.ForwardOutErr).
				Run()
			if err != nil {
				return err
			}
			klog.Info("Cluster deleted")
			return nil
		}()
		if err != nil {
			klog.Fatal(err)
		}

	},
}

func init() {
	deleteCmd.AddCommand(deleteClusterCmd)

	// Here you will define your flags and configuration settings.

	// Cobra supports Persistent Flags which will work for this command
	// and all subcommands, e.g.:
	// deleteClusterCmd.PersistentFlags().String("foo", "", "A help for foo")

	// Cobra supports local flags which will only run when this command
	// is called directly, e.g.:
	// deleteClusterCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")
}

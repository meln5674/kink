/*
Copyright © 2022 NAME HERE <EMAIL ADDRESS>
*/
package cmd

import (
	"context"
	"fmt"

	"k8s.io/klog/v2"

	"github.com/meln5674/gosh"
	"github.com/meln5674/kink/pkg/helm"
	"github.com/meln5674/kink/pkg/kubectl"
	"github.com/meln5674/rflag"

	"github.com/spf13/cobra"
)

// deleteClusterCmd represents the delete cluster command
var deleteClusterCmd = &cobra.Command{
	Use:          "cluster",
	Short:        "Deletes a cluster",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		return deleteCluster(context.Background(), &deleteClusterArgs, &resolvedConfig)
	},
}

type deleteClusterArgsT struct {
	DeletePVCs bool `rflag:"name=delete-pvcs,usage=Delete the PVCs backing the cluster. By default,, these are not deleted"`
}

func (deleteClusterArgsT) Defaults() deleteClusterArgsT {
	return deleteClusterArgsT{}
}

var deleteClusterArgs = deleteClusterArgsT{}.Defaults()

func init() {
	deleteCmd.AddCommand(deleteClusterCmd)
	rflag.MustRegister(rflag.ForPFlag(deleteClusterCmd.Flags()), "", &deleteClusterArgs)
}

func deleteCluster(ctx context.Context, args *deleteClusterArgsT, cfg *resolvedConfigT) error {
	var err error
	klog.Info("Deleting release...")
	// TODO: Add flag to also delete PVCs
	raw := cfg.KinkConfig.Release.Raw()
	helmDelete := helm.Delete(&cfg.KinkConfig.Helm, &cfg.KinkConfig.Chart, &raw, &cfg.KinkConfig.Kubernetes)
	err = gosh.
		Command(helmDelete...).
		WithContext(ctx).
		WithStreams(gosh.ForwardOutErr).
		Run()
	if err != nil {
		return err
	}
	klog.Info("Cluster deleted")
	if !args.DeletePVCs {
		klog.Info("PVCs have been kept. Use --delete-pvcs to delete these as well")
		return nil
	}
	klog.Info("Deleting PVCs...")
	deletePVCs := kubectl.Delete(&cfg.KinkConfig.Kubectl, &cfg.KinkConfig.Kubernetes, "persistentvolumeclaim", fmt.Sprintf("-l%s=%s", helm.ClusterLabel, cfg.KinkConfig.Release.ClusterName))
	err = gosh.
		Command(deletePVCs...).
		WithContext(ctx).
		WithStreams(gosh.ForwardOutErr).
		Run()
	if err != nil {
		return err
	}
	klog.Info("Cluster deleted")
	return nil
}

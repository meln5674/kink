/*
Copyright © 2022 NAME HERE <EMAIL ADDRESS>
*/
package cmd

import (
	"context"
	"fmt"

	"k8s.io/klog/v2"

	"github.com/spf13/cobra"

	"github.com/meln5674/gosh"
	"github.com/meln5674/rflag"

	"github.com/meln5674/kink/pkg/helm"
	"github.com/meln5674/kink/pkg/kubectl"
)

// createClusterCmd represents the create cluster command
var createClusterCmd = &cobra.Command{
	Use:          "cluster",
	Short:        "Create a cluster in another cluster",
	Long:         `Creates an isolated Kubernetes cluster in an existing cluster using pod 'nodes'`,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		return createCluster(context.Background(), &createClusterArgs, &resolvedConfig)
	},
}

type createClusterArgsT struct {
	ExportKubeconfigArgs exportKubeconfigArgsT `rflag:""`
}

func (createClusterArgsT) Defaults() createClusterArgsT {
	return createClusterArgsT{
		ExportKubeconfigArgs: exportKubeconfigArgsT{}.Defaults(),
	}
}

var createClusterArgs = createClusterArgsT{}.Defaults()

func init() {
	createCmd.AddCommand(createClusterCmd)

	rflag.MustRegister(rflag.ForPFlag(createClusterCmd.Flags()), "", &createClusterArgs)
}

func createCluster(ctx context.Context, args *createClusterArgsT, cfg *resolvedConfigT) error {
	if cfg.KinkConfig.Chart.IsLocalChart() {
		klog.Info("Using local chart, skipping `repo add`...")
	} else {
		klog.Info("Ensuring helm repo exists...")
		repoAdd := helm.RepoAdd(&cfg.KinkConfig.Helm, &cfg.KinkConfig.Chart)
		err := gosh.
			Command(repoAdd...).
			WithContext(ctx).
			WithStreams(gosh.ForwardOutErr).
			Run()
		if err != nil {
			return err
		}

	}

	klog.Info("Deploying chart...")
	helmUpgrade := helm.UpgradeCluster(&cfg.KinkConfig.Helm, &cfg.KinkConfig.Chart, &cfg.KinkConfig.Release, &cfg.KinkConfig.Kubernetes)
	err := gosh.
		Command(helmUpgrade...).
		WithContext(ctx).
		WithStreams(gosh.ForwardOutErr).
		Run()
	if err != nil {
		return err
	}
	klog.Info("Deployed chart, waiting for controlplane to be healthy")

	controlplaneRollout := kubectl.RolloutStatus(&cfg.KinkConfig.Kubectl, &cfg.KinkConfig.Kubernetes, "statefulset", cfg.ReleaseConfig.ControlplaneFullname)
	err = gosh.
		Command(controlplaneRollout...).
		WithContext(ctx).
		WithStreams(gosh.ForwardOutErr).
		Run()
	if err != nil {
		return err
	}

	klog.Info("Controlplane is healthy, your cluster is now ready to use")
	if args.ExportKubeconfigArgs.KubeconfigToExportPath == "" {
		return nil
	}
	err = exportKubeconfigToPath(ctx, &args.ExportKubeconfigArgs, cfg)
	if err != nil {
		return fmt.Errorf("failed to export kubeconfig: %w", err)
	}
	return nil
}

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

var (
	doRepoUpdate bool
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
			if config.Chart.IsLocalChart() {
				klog.Info("Using local chart, skipping `repo add`...")
			} else {
				klog.Info("Ensuring helm repo exists...")
				repoAdd := helm.RepoAdd(&config.Helm, &config.Chart)
				err = gosh.
					Command(repoAdd...).
					WithContext(ctx).
					WithStreams(gosh.ForwardOutErr).
					Run()
				if err != nil {
					return err
				}
				if doRepoUpdate {
					repoUpdate := helm.RepoUpdate(&config.Helm, config.Chart.RepoName())
					klog.Info("Updating chart repo...")
					err = gosh.
						Command(repoUpdate...).
						WithContext(ctx).
						WithStreams(gosh.ForwardOutErr).
						Run()
					if err != nil {
						return err
					}
				} else {
					klog.Info("Chart repo update skipped by flag")
				}
			}

			klog.Info("Deploying chart...")
			helmUpgrade := helm.UpgradeCluster(&config.Helm, &config.Chart, &config.Release, &config.Kubernetes)
			err = gosh.
				Command(helmUpgrade...).
				WithContext(ctx).
				WithStreams(gosh.ForwardOutErr).
				Run()
			if err != nil {
				return err
			}
			klog.Info("Deployed chart, your cluster is now ready to use")
			if kubeconfigToExportPath != "" {
				exportKubeconfigCmd.Run(cmd, args)
			}
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
	createClusterCmd.Flags().StringVar(&kubeconfigToExportPath, "out-kubeconfig", "./kink.kubeconfig", "Path to export kubeconfig to")
	createClusterCmd.Flags().StringVar(&externalControlplaneURL, "external-controlplane-url", "", "A URL external to the parent cluster which the new controlplane will be accessible at. If present, an extra context called \"external\" will be added with this URL")
	createClusterCmd.Flags().BoolVar(&doRepoUpdate, "repo-update", true, "Update the helm repo before upgrading. Note that if a new chart version has become availabe since install or last upgrade, this will result in upgrading the chart. If this unacceptable, set this to false, or use --chart-version to pin a specific version")
}

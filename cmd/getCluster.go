/*
Copyright © 2022 NAME HERE <EMAIL ADDRESS>
*/
package cmd

import (
	"context"
	"fmt"

	"github.com/meln5674/gosh"
	"github.com/meln5674/kink/pkg/helm"

	"github.com/spf13/cobra"
)

// getClusterCmd represents the get cluster command
var getClusterCmd = &cobra.Command{
	Use:          "cluster",
	Aliases:      []string{"clusters"},
	Short:        "Lists existing kink clusters by their name",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		return getClusters(context.Background(), &resolvedConfig)
	},
}

func init() {
	getCmd.AddCommand(getClusterCmd)
}

func getClusters(ctx context.Context, cfg *resolvedConfigT) error {
	releases := make([]map[string]interface{}, 0)
	helmList := helm.List(&cfg.KinkConfig.Helm, &cfg.KinkConfig.Kubernetes)
	err := gosh.
		Command(helmList...).
		WithContext(ctx).
		WithStreams(
			gosh.ForwardErr,
			gosh.FuncOut(gosh.SaveJSON(&releases)),
		).
		Run()
	if err != nil {
		return err
	}

	for _, release := range releases {
		clusterName, isCluster := helm.GetReleaseClusterName(release["name"].(string))
		if !isCluster {
			continue
		}
		fmt.Println(clusterName)
	}

	return nil
}

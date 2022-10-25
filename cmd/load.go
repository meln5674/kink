/*
Copyright © 2022 NAME HERE <EMAIL ADDRESS>

*/
package cmd

import (
	"context"

	"github.com/meln5674/gosh"
	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"

	"github.com/meln5674/kink/pkg/kubectl"
)

var (
	parallelLoads int
)

// loadCmd represents the load command
var loadCmd = &cobra.Command{
	Use:   "load",
	Short: "Loads images into nodes from an archive or docker daemon on this host",
}

func init() {
	rootCmd.AddCommand(loadCmd)

	// Here you will define your flags and configuration settings.

	// Cobra supports Persistent Flags which will work for this command
	// and all subcommands, e.g.:
	// loadCmd.PersistentFlags().String("foo", "", "A help for foo")
	loadCmd.PersistentFlags().IntVar(&parallelLoads, "parallel-loads", 1, "How many image/artifact loads to run in parallel")

	// Cobra supports local flags which will only run when this command
	// is called directly, e.g.:
	// loadCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")
}

func getPods(ctx context.Context) (*corev1.PodList, error) {
	var pods corev1.PodList
	getPods := kubectl.GetPods(&config.Kubectl, &config.Kubernetes, config.Release.Namespace, config.Release.ExtraLabels())
	err := gosh.
		Command(getPods...).
		WithContext(ctx).
		WithStreams(
			gosh.ForwardErr,
			gosh.FuncOut(gosh.SaveJSON(&pods)),
		).
		Run()
	return &pods, err
}

func importParallel(imports ...gosh.Commander) error {
	cmd := gosh.FanOut(imports...)
	if parallelLoads > 0 {
		cmd = cmd.WithMaxConcurrency(parallelLoads)
	}
	return cmd.Run()

}

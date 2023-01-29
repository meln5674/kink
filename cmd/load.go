/*
Copyright © 2022 NAME HERE <EMAIL ADDRESS>
*/
package cmd

import (
	"context"

	"github.com/meln5674/gosh"
	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"

	"github.com/meln5674/kink/pkg/containerd"
	"github.com/meln5674/kink/pkg/kubectl"
)

var (
	parallelLoads     int
	onlyLoadToWorkers bool
	importImageFlags  containerd.CtrFlags

	k3sDefaultImportImageFlags = containerd.CtrFlags{
		Command: []string{"k3s", "ctr"},
	}

	rke2DefaultImportImageFlags = containerd.CtrFlags{
		Command:   []string{"/var/lib/rancher/rke2/bin/ctr"},
		Namespace: "k8s.io",
		Address:   "/run/k3s/containerd/containerd.sock",
	}
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
	loadCmd.PersistentFlags().BoolVar(&onlyLoadToWorkers, "only-load-workers", true, "If true, only load images to worker nodes, if false, also load to controlplane nodes")
	loadCmd.PersistentFlags().StringArrayVar(&importImageFlags.Command, "ctr-command", []string{}, "Command to run within node pods to load images. Default is based on which distribution is used")
	loadCmd.PersistentFlags().StringVar(&importImageFlags.Namespace, "ctr-namespace", "", "Containerd namespace to to load images to. Default is based on which distribution is used")
	loadCmd.PersistentFlags().StringVar(&importImageFlags.Address, "ctr-address", "", "Containerd socket address to to load images to. Default is based on which distribution is used")

	// Cobra supports local flags which will only run when this command
	// is called directly, e.g.:
	// loadCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")
}

func parseImportImageFlags() {
	var defaults *containerd.CtrFlags
	if rke2Enabled() {
		defaults = &rke2DefaultImportImageFlags
	} else {
		defaults = &k3sDefaultImportImageFlags
	}
	importImageFlags.Override(defaults)
}

func getPods(ctx context.Context) (*corev1.PodList, error) {
	var pods corev1.PodList
	labels := config.Release.ExtraLabels()
	if onlyLoadToWorkers {
		labels["app.kubernetes.io/component"] = "worker"
	}
	getPods := kubectl.GetPods(&config.Kubectl, &config.Kubernetes, labels)
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

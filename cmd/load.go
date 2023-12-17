/*
Copyright © 2022 NAME HERE <EMAIL ADDRESS>
*/
package cmd

import (
	"context"
	"fmt"

	"github.com/meln5674/gosh"
	"github.com/meln5674/rflag"
	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"

	"github.com/meln5674/kink/pkg/containerd"
	"github.com/meln5674/kink/pkg/helm"
	"github.com/meln5674/kink/pkg/kubectl"
)

var (
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

type loadArgsT struct {
	ParallelLoads     int      `rflag:"usage=How many image/artifact loads to run in parallel"`
	OnlyLoadToWorkers bool     `rflag:"name=only-load-workers,usage=If true,, only load images to worker nodes,, if false,, also load to controlplane nodes"`
	CtrCommand        []string `rflag:"slice-type=slice,usage=Command to run within node pods to load images. Default is based on which distribution is used"`
	CtrNamespace      string   `rflag:"usage=Containerd namespace to to load images to. Default is based on which distribution is used"`
	CtrAddress        string   `rflag:"usage=Containerd socket address to to load images to. Default is based on which distribution is used"`
}

func (loadArgsT) Defaults() loadArgsT {
	return loadArgsT{
		ParallelLoads:     1,
		OnlyLoadToWorkers: true,
		CtrCommand:        []string{},
	}
}

func (l *loadArgsT) importImageOverrides() *containerd.CtrFlags {
	return &containerd.CtrFlags{
		Command:   l.CtrCommand,
		Namespace: l.CtrNamespace,
		Address:   l.CtrAddress,
	}
}

func (l *loadArgsT) parseImportImageFlags(cfg *resolvedConfigT) *containerd.CtrFlags {
	var defaults *containerd.CtrFlags
	if cfg.ReleaseConfig.RKE2Enabled {
		defaults = &rke2DefaultImportImageFlags
	} else {
		defaults = &k3sDefaultImportImageFlags
	}
	l.importImageOverrides().Override(defaults)

	return defaults
}

var loadArgs = loadArgsT{}.Defaults()

func init() {
	rootCmd.AddCommand(loadCmd)
	rflag.MustRegister(rflag.ForPFlag(loadCmd.PersistentFlags()), "", &loadArgs)
}

func getPods(ctx context.Context, args *loadArgsT, cfg *resolvedConfigT) (*corev1.PodList, error) {
	var pods corev1.PodList
	labels := map[string]string{
		helm.ClusterLabel: cfg.KinkConfig.Release.ClusterName,
	}
	if args.OnlyLoadToWorkers {
		labels["app.kubernetes.io/component"] = "worker"
	} else {
		labels[helm.ClusterNodeLabel] = "true"
	}
	getPods := kubectl.GetPods(&cfg.KinkConfig.Kubectl, &cfg.KinkConfig.Kubernetes, labels)
	err := gosh.
		Command(getPods...).
		WithContext(ctx).
		WithStreams(
			gosh.ForwardErr,
			gosh.FuncOut(gosh.SaveJSON(&pods)),
		).
		Run()
	if len(pods.Items) == 0 {
		return nil, fmt.Errorf("No cluster pods matched, this is likely a bug")
	}
	return &pods, err
}

func importParallel(args *loadArgsT, imports ...gosh.Commander) error {
	cmd := gosh.FanOut(imports...)
	if args.ParallelLoads > 0 {
		cmd = cmd.WithMaxConcurrency(args.ParallelLoads)
	}
	return cmd.Run()
}

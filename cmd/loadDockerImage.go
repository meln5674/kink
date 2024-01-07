/*
Copyright © 2022 NAME HERE <EMAIL ADDRESS>
*/
package cmd

import (
	"bytes"
	"context"

	"github.com/meln5674/gosh"
	"github.com/meln5674/rflag"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"

	"github.com/meln5674/kink/pkg/containerd"
	"github.com/meln5674/kink/pkg/docker"
	"github.com/meln5674/kink/pkg/kubectl"
)

// loadDockerImageCmd represents the load docker-image command
var loadDockerImageCmd = &cobra.Command{
	Use:          "docker-image",
	Short:        "Loads docker images from host docker daemon to all nodes",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(loadDockerImageArgs.Images) == 0 {
			return errors.New("No images specified")
		}
		return loadImages(context.Background(), &loadArgs, &resolvedConfig, loadDockerImageArgs.Images...)
	},
}

type loadDockerImageArgsT struct {
	Images []string `rflag:"name=image,usage=images to load"`
}

func (loadDockerImageArgsT) Defaults() loadDockerImageArgsT {
	return loadDockerImageArgsT{
		Images: []string{},
	}
}

var loadDockerImageArgs = loadDockerImageArgsT{}.Defaults()

func init() {
	loadCmd.AddCommand(loadDockerImageCmd)
	rflag.MustRegister(rflag.ForPFlag(loadDockerImageCmd.Flags()), "", &loadDockerImageArgs)
}

func loadImages(ctx context.Context, args *loadArgsT, cfg *resolvedConfigT, images ...string) error {
	pods, err := getPods(ctx, args, cfg)
	if err != nil {
		return err
	}
	var ctrImport bytes.Buffer
	err = ctrImportScriptTpl.Execute(&ctrImport, containerd.ImportImage(args.parseImportImageFlags(cfg), "-"))
	if err != nil {
		return err
	}
	imports := make([]gosh.Commander, 0, len(pods.Items))
	for _, pod := range pods.Items {
		kubectlExec := kubectl.Exec(
			&cfg.KinkConfig.Kubectl, &cfg.KinkConfig.Kubernetes,
			pod.Name,
			true, false,
			"sh", "-c", ctrImport.String(),
		)
		dockerSave := docker.Save(&cfg.KinkConfig.Docker, images...)
		pipeline := gosh.Pipeline(
			gosh.Command(dockerSave...).WithContext(ctx).WithStreams(gosh.ForwardErr),
			gosh.Command(kubectlExec...).WithContext(ctx).WithStreams(gosh.ForwardOutErr),
		).WithStreams(gosh.ForwardErr)
		imports = append(imports, pipeline)
	}
	// TODO: Replace this with a goroutine that copies from one docker save to each kubectl exec
	err = importParallel(args, imports...)
	if err != nil {
		return err
	}
	return nil
}

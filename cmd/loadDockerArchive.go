/*
Copyright © 2022 NAME HERE <EMAIL ADDRESS>
*/
package cmd

import (
	"bytes"
	"context"

	"github.com/pkg/errors"
	"github.com/spf13/cobra"

	"github.com/meln5674/gosh"
	"github.com/meln5674/kink/pkg/containerd"
	"github.com/meln5674/kink/pkg/kubectl"
	"github.com/meln5674/rflag"
)

var ()

// loadDockerArchiveCmd represents the load docker-archive command
var loadDockerArchiveCmd = &cobra.Command{
	Use:          "docker-archive",
	Short:        "Loads docker-formatted archives from the host to all nodes",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(loadDockerArchiveArgs.Archives) == 0 {
			return errors.New("No archives specified")
		}
		return loadArchives(context.Background(), &loadArgs, &resolvedConfig, loadDockerArchiveArgs.Archives...)
	},
}

type loadDockerArchiveArgsT struct {
	Archives []string `rflag:"name=archive,usage=Paths to archives to load"`
}

func (loadDockerArchiveArgsT) Defaults() loadDockerArchiveArgsT {
	return loadDockerArchiveArgsT{
		Archives: []string{},
	}
}

var loadDockerArchiveArgs = loadDockerArchiveArgsT{}.Defaults()

func init() {
	loadCmd.AddCommand(loadDockerArchiveCmd)
	rflag.MustRegister(rflag.ForPFlag(loadDockerArchiveCmd.Flags()), "", &loadDockerArchiveArgs)
}

func loadArchives(ctx context.Context, args *loadArgsT, cfg *resolvedConfigT, archives ...string) (err error) {
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
	for _, archive := range archives {
		for _, pod := range pods.Items {
			kubectlExec := kubectl.Exec(
				&cfg.KinkConfig.Kubectl, &cfg.KinkConfig.Kubernetes,
				pod.Name,
				true, false,
				"sh", "-c", ctrImport.String(),
			)
			cmd := gosh.
				Command(kubectlExec...).
				WithContext(ctx).
				WithStreams(
					gosh.FileIn(archive),
					gosh.ForwardOutErr,
				)
			imports = append(imports, cmd)
		}
	}
	err = importParallel(args, imports...)
	if err != nil {
		return err
	}
	return nil
}

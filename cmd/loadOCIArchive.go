/*
Copyright © 2022 NAME HERE <EMAIL ADDRESS>
*/
package cmd

import (
	"context"

	"github.com/meln5674/rflag"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
)

var (
	ociArchivesToLoad []string
)

// loadOCIArchiveCmd represents the load oci-archive command
var loadOCIArchiveCmd = &cobra.Command{
	Use:          "oci-archive",
	Short:        "Loads OCI image archive (e.g. from buildah) to all nodes in your cluster",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		// These are actually identical operation to load docker-archives, but this is here just in case that
		// ceases to be the case
		if len(loadOCIArchiveArgs.Archives) == 0 {
			return errors.New("No archives specified")
		}
		return loadArchives(context.Background(), &loadArgs, &resolvedConfig, loadOCIArchiveArgs.Archives...)
	},
}

type loadOCIArchiveArgsT struct {
	Archives []string `rflag:"name=archive,usage=Paths to archives to load"`
}

func (loadOCIArchiveArgsT) Defaults() loadOCIArchiveArgsT {
	return loadOCIArchiveArgsT{
		Archives: []string{},
	}
}

var loadOCIArchiveArgs = loadOCIArchiveArgsT{}.Defaults()

func init() {
	loadCmd.AddCommand(loadOCIArchiveCmd)
	rflag.MustRegister(rflag.ForPFlag(loadOCIArchiveCmd.Flags()), "", &loadOCIArchiveArgs)
}

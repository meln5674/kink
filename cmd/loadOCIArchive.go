/*
Copyright © 2022 NAME HERE <EMAIL ADDRESS>

*/
package cmd

import (
	"github.com/spf13/cobra"
)

var (
	ociArchivesToLoad []string
)

// ociArchiveCmd represents the ociArchive command
var loadOCIArchiveCmd = &cobra.Command{
	Use:   "oci-archive",
	Short: "Loads OCI image archive (e.g. from buildah) to all nodes in your cluster",
	Run: func(cmd *cobra.Command, args []string) {
		// These are actually identical operations, but this is here just in case that
		// ceases to be the case
		loadArchives(ociArchivesToLoad...)
	},
}

func init() {
	loadCmd.AddCommand(loadOCIArchiveCmd)

	// Here you will define your flags and configuration settings.

	// Cobra supports Persistent Flags which will work for this command
	// and all subcommands, e.g.:
	// ociArchiveCmd.PersistentFlags().String("foo", "", "A help for foo")

	// Cobra supports local flags which will only run when this command
	// is called directly, e.g.:
	// ociArchiveCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")
	loadOCIArchiveCmd.Flags().StringArrayVar(&ociArchivesToLoad, "archive", []string{}, "Paths to archives to load")
}

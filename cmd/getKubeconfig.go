/*
Copyright © 2022 NAME HERE <EMAIL ADDRESS>
*/
package cmd

import (
	"context"
	"os"

	"github.com/meln5674/rflag"
	"github.com/spf13/cobra"
)

var ()

// getKubeconfigCmd represents the get kubeconfig command
var getKubeconfigCmd = &cobra.Command{
	Use:          "kubeconfig",
	Short:        "Prints cluster kubeconfig",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		return exportKubeconfig(context.Background(), os.Stdout, &getKubeconfigArgs.Export, &resolvedConfig)
	},
}

type getKubeconfigArgsT struct {
	Export exportKubeconfigCommonArgsT `rflag:""`
}

func (getKubeconfigArgsT) Defaults() getKubeconfigArgsT {
	return getKubeconfigArgsT{
		Export: exportKubeconfigCommonArgsT{}.Defaults(),
	}
}

var getKubeconfigArgs = getKubeconfigArgsT{}.Defaults()

func init() {
	getCmd.AddCommand(getKubeconfigCmd)
	rflag.MustRegister(rflag.ForPFlag(getKubeconfigCmd.Flags()), "", &getKubeconfigArgs)
}

/*
Copyright © 2022 NAME HERE <EMAIL ADDRESS>
*/
package cmd

import (
	"context"

	"github.com/meln5674/gosh"
	"github.com/meln5674/rflag"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
)

// getNodeCmd represents the get node command
var getNodeCmd = &cobra.Command{
	Use:     "node",
	Aliases: []string{"nodes"},
	Short:   "Lists existing kink nodes by their name",
	Long: `This is functionally equivalent to running

kink exec -- kubectl get nodes`,
	RunE: func(cmd *cobra.Command, args []string) error {
		maybeExitCode, err := execWithGateway(context.Background(), gosh.Command("kubectl", "get", "nodes"), &getNodeArgs.ExecArgs, &resolvedConfig)
		if err != nil {
			return err
		}
		if maybeExitCode != nil {
			return errors.New("Failed list nodes")
		}
		return nil
	},
}

type getNodeArgsT struct {
	ExecArgs execArgsT `rflag:""`
}

func (getNodeArgsT) Defaults() getNodeArgsT {
	return getNodeArgsT{
		ExecArgs: execArgsT{}.Defaults(),
	}
}

var getNodeArgs = getNodeArgsT{}.Defaults()

func init() {
	getCmd.AddCommand(getNodeCmd)
	rflag.MustRegister(rflag.ForPFlag(getNodeCmd.Flags()), "", &getNodeArgs)
}

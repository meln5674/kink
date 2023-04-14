/*
Copyright © 2023 NAME HERE <EMAIL ADDRESS>
*/
package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

// fileGatewayCmd represents the fileGateway command
var fileGatewayCmd = &cobra.Command{
	Use:   "file-gateway",
	Short: "Operations involving the file gateway",
	Long: `Unlike with KinD, there is no reliable, consistent way to mount directories into your cluster.
Instead, KinK provides an optional component called the file gateway, which allows you to send a tar archive
to a NodePort or ingress and have it be extracted into a directory shared with the rest of your cluster.
This avoids having to funnel your archive through the control plane via port-forward or kubectl cp, which
may be bandwidth-constrained`,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("fileGateway called")
	},
}

func init() {
	rootCmd.AddCommand(fileGatewayCmd)
}

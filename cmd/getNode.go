/*
Copyright © 2022 NAME HERE <EMAIL ADDRESS>

*/
package cmd

import (
	"github.com/spf13/cobra"
)

// getNodeCmd represents the getNode command
var getNodeCmd = &cobra.Command{
	Use:   "node",
	Short: "Lists existing kink nodes by their name",
	Long: `This is functionally equivalent to running

kink exec -- kubectl get nodes`,
	Run: func(cmd *cobra.Command, args []string) {
		// TODO: This feels dirty, somehow
		execCmd.Run(cmd, []string{"kubectl", "get", "nodes"})
	},
}

func init() {
	getCmd.AddCommand(getNodeCmd)

	// Here you will define your flags and configuration settings.

	// Cobra supports Persistent Flags which will work for this command
	// and all subcommands, e.g.:
	// getNodeCmd.PersistentFlags().String("foo", "", "A help for foo")

	// Cobra supports local flags which will only run when this command
	// is called directly, e.g.:
	// getNodeCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")
}

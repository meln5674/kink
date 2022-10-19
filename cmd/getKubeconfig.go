/*
Copyright © 2022 NAME HERE <EMAIL ADDRESS>

*/
package cmd

import (
	"github.com/spf13/cobra"
)

// getKubeconfigCmd represents the getKubeconfig command
var getKubeconfigCmd = &cobra.Command{
	Use:   "kubeconfig",
	Short: "Prints cluster kubeconfig",
	Run: func(cmd *cobra.Command, args []string) {
		shCmd.Run(cmd, []string{"--", "cat", "${KUBECONFIG}"})
	},
}

func init() {
	getCmd.AddCommand(getKubeconfigCmd)

	// Here you will define your flags and configuration settings.

	// Cobra supports Persistent Flags which will work for this command
	// and all subcommands, e.g.:
	// getKubeconfigCmd.PersistentFlags().String("foo", "", "A help for foo")

	// Cobra supports local flags which will only run when this command
	// is called directly, e.g.:
	// getKubeconfigCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")
}

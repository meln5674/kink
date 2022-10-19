/*
Copyright © 2022 NAME HERE <EMAIL ADDRESS>

*/
package cmd

import (
	"github.com/spf13/cobra"
)

var (
	exportedKubeconfigPath string
)

// exportKubeconfigCmd represents the exportKubeconfig command
var exportKubeconfigCmd = &cobra.Command{
	Use:   "exportKubeconfig",
	Short: "Exports cluster kubeconfig",
	Run: func(cmd *cobra.Command, args []string) {
		shCmd.Run(cmd, []string{"--", "cp", "${KUBECONFIG}", exportedKubeconfigPath})
	},
}

func init() {
	exportCmd.AddCommand(exportKubeconfigCmd)

	// Here you will define your flags and configuration settings.

	// Cobra supports Persistent Flags which will work for this command
	// and all subcommands, e.g.:
	// exportKubeconfigCmd.PersistentFlags().String("foo", "", "A help for foo")

	// Cobra supports local flags which will only run when this command
	// is called directly, e.g.:
	exportKubeconfigCmd.Flags().StringVar(&exportedKubeconfigPath, "--out-kubeconfig", "./kink.kubeconfig", "Path to export kubeconfig to")
}

/*
Copyright © 2022 NAME HERE <EMAIL ADDRESS>

*/
package cmd

import (
	"fmt"

	"github.com/meln5674/kink/pkg/command"
	"github.com/spf13/cobra"
	"io"
	corev1 "k8s.io/api/core/v1"

	"encoding/json"
)

var (
	parallelLoads int
)

// loadCmd represents the load command
var loadCmd = &cobra.Command{
	Use:   "load",
	Short: "A brief description of your command",
	Long: `A longer description that spans multiple lines and likely contains examples
and usage of using your command. For example:

Cobra is a CLI library for Go that empowers applications.
This application is a tool to generate the needed files
to quickly create a Cobra application.`,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("load called")
	},
}

func init() {
	rootCmd.AddCommand(loadCmd)

	// Here you will define your flags and configuration settings.

	// Cobra supports Persistent Flags which will work for this command
	// and all subcommands, e.g.:
	// loadCmd.PersistentFlags().String("foo", "", "A help for foo")
	loadCmd.PersistentFlags().IntVar(&parallelLoads, "parallel-loads", 1, "How many image/artifact loads to run in parallel")

	// Cobra supports local flags which will only run when this command
	// is called directly, e.g.:
	// loadCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")
}

func findWorkerPods(podNames *[]string) command.PipeProcessor {
	return func(stdout io.Reader) error {
		var pods corev1.PodList
		err := json.NewDecoder(stdout).Decode(&pods)
		if err != nil {
			return err
		}
		// TODO: detect unhealthy pods and alert the user
		*podNames = make([]string, 0, len(pods.Items))
		for _, pod := range pods.Items {
			*podNames = append(*podNames, pod.Name)
		}
		return nil
	}
}

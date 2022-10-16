/*
Copyright © 2022 NAME HERE <EMAIL ADDRESS>

*/
package cmd

import (
	"os"

	"github.com/spf13/cobra"

	"github.com/meln5674/kink/pkg/docker"
	"github.com/meln5674/kink/pkg/helm"
	"github.com/meln5674/kink/pkg/kubectl"
)

var (
	helmFlags    = helm.HelmFlags{}
	kubectlFlags = kubectl.KubectlFlags{}
	kubeFlags    = kubectl.KubeFlags{}
	dockerFlags  = docker.DockerFlags{}
	chartFlags   = helm.ChartFlags{}
	releaseFlags = helm.ReleaseFlags{}
)

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "kink",
	Short: "A brief description of your application",
	Long: `A longer description that spans multiple lines and likely contains
examples and usage of using your application. For example:

Cobra is a CLI library for Go that empowers applications.
This application is a tool to generate the needed files
to quickly create a Cobra application.`,
	// Uncomment the following line if your bare application
	// has an action associated with it:
	// Run: func(cmd *cobra.Command, args []string) { },
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}

func init() {
	// Here you will define your flags and configuration settings.
	// Cobra supports persistent flags, which, if defined here,
	// will be global for your application.

	// rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is $HOME/.kink.yaml)")

	// Cobra also supports local flags, which will only run
	// when this action is called directly.
	// TODO: Add .exe to default binaries if running on windows
	rootCmd.PersistentFlags().StringSliceVar(&helmFlags.Command, "helm-command", []string{"helm"}, "Command to execute for helm")
	rootCmd.PersistentFlags().StringSliceVar(&kubectlFlags.Command, "kubectl-command", []string{"kubectl"}, "Command to execute for kubectl")
	rootCmd.PersistentFlags().StringSliceVar(&dockerFlags.Command, "docker-command", []string{"docker"}, "Command to execute for docker")

	rootCmd.PersistentFlags().StringVar(&chartFlags.ChartName, "chart", "kink", "Name of KinK Helm Chart")
	rootCmd.PersistentFlags().StringVar(&chartFlags.RepositoryURL, "repository-url", "https://meln5674.github.io/kink", "URL of KinK Helm Chart repository")
	rootCmd.PersistentFlags().StringVar(&releaseFlags.ClusterName, "cluster", "kink", "Command to execute for docker")
	rootCmd.PersistentFlags().StringArrayVar(&releaseFlags.Values, "values", []string{}, "Extra values.yaml files to use when creating cluster")
	rootCmd.PersistentFlags().StringArrayVar(&releaseFlags.Set, "set", []string{}, "Extra field overrides to use when creating cluster")
	// TODO: Add flags for kubeconfig/helm
	// TODO: Add flags for docker
}

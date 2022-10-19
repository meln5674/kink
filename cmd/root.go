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

	"k8s.io/client-go/tools/clientcmd"
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
	Short: "Kubernetes in Kubernetes",
	Long:  `Deploy Kubernetes clusters within other Kubernetes clusters`,
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
	rootCmd.PersistentFlags().StringSliceVar(&helmFlags.Command, "helm-command", []string{"helm"}, "Command to execute for helm")
	rootCmd.PersistentFlags().StringSliceVar(&kubectlFlags.Command, "kubectl-command", []string{"kubectl"}, "Command to execute for kubectl")
	rootCmd.PersistentFlags().StringSliceVar(&dockerFlags.Command, "docker-command", []string{"docker"}, "Command to execute for docker")

	rootCmd.PersistentFlags().StringVar(&chartFlags.ChartName, "chart", "kink", "Name of KinK Helm Chart")
	rootCmd.PersistentFlags().StringVar(&chartFlags.RepositoryURL, "repository-url", "https://meln5674.github.io/kink", "URL of KinK Helm Chart repository")
	rootCmd.PersistentFlags().StringVar(&releaseFlags.ClusterName, "name", "kink", "Name of the kink cluster")
	rootCmd.PersistentFlags().StringArrayVar(&releaseFlags.Values, "values", []string{}, "Extra values.yaml files to use when creating cluster")
	rootCmd.PersistentFlags().StringArrayVar(&releaseFlags.Set, "set", []string{}, "Extra field overrides to use when creating cluster")
	// TODO: Add flags for docker
	rootCmd.PersistentFlags().StringVar(&kubeFlags.Kubeconfig, "kubeconfig", "", "Path to the kubeconfig file to use for CLI requests.")
	clientcmd.BindOverrideFlags(&kubeFlags.ConfigOverrides, rootCmd.PersistentFlags(), clientcmd.RecommendedConfigOverrideFlags(""))
}

/*
Copyright © 2022 NAME HERE <EMAIL ADDRESS>

*/
package cmd

import (
	"context"
	goflag "flag"
	"fmt"
	"io/ioutil"
	"os"

	"github.com/spf13/cobra"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"
	"sigs.k8s.io/yaml"

	"github.com/meln5674/gosh"

	cfg "github.com/meln5674/kink/pkg/config"
	"github.com/meln5674/kink/pkg/helm"
)

var (
	config          cfg.Config
	configOverrides cfg.Config
	configPath      string
	releaseValues   map[string]interface{}
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
		klog.Fatal(err)
	}
}

func init() {
	klogFlags := goflag.NewFlagSet("", goflag.PanicOnError)
	klog.InitFlags(klogFlags)
	rootCmd.PersistentFlags().AddGoFlagSet(klogFlags)

	rootCmd.PersistentFlags().StringVar(&configPath, "config", "", "Path to KinK config file to use instead of arguments")

	rootCmd.PersistentFlags().StringSliceVar(&configOverrides.Helm.Command, "helm-command", []string{"helm"}, "Command to execute for helm")
	rootCmd.PersistentFlags().StringSliceVar(&configOverrides.Kubectl.Command, "kubectl-command", []string{"kubectl"}, "Command to execute for kubectl")
	rootCmd.PersistentFlags().StringSliceVar(&configOverrides.Docker.Command, "docker-command", []string{"docker"}, "Command to execute for docker")

	rootCmd.PersistentFlags().StringVar(&configOverrides.Chart.ChartName, "chart", "kink", "Name of KinK Helm Chart")
	rootCmd.PersistentFlags().StringVar(&configOverrides.Chart.Version, "chart-version", "", "Version of the chart to install")
	rootCmd.PersistentFlags().StringVar(&configOverrides.Chart.RepositoryURL, "repository-url", "https://meln5674.github.io/kink", "URL of KinK Helm Chart repository")
	rootCmd.PersistentFlags().StringVar(&configOverrides.Release.ClusterName, "name", cfg.DefaultClusterName, "Name of the kink cluster")
	rootCmd.PersistentFlags().StringArrayVar(&configOverrides.Release.Values, "values", []string{}, "Extra values.yaml files to use when creating cluster")
	rootCmd.PersistentFlags().StringToStringVar(&configOverrides.Release.Set, "set", map[string]string{}, "Extra field overrides to use when creating cluster")
	rootCmd.PersistentFlags().StringToStringVar(&configOverrides.Release.SetString, "set-string", map[string]string{}, "Extra field overrides to use when creating cluster, forced interpreted as strings")
	// TODO: Add flags for docker
	rootCmd.PersistentFlags().StringVar(&configOverrides.Kubernetes.Kubeconfig, "kubeconfig", "", "Path to the kubeconfig file to use for CLI requests.")
	clientcmd.BindOverrideFlags(&configOverrides.Kubernetes.ConfigOverrides, rootCmd.PersistentFlags(), clientcmd.RecommendedConfigOverrideFlags(""))
}

func loadConfig() error {
	err := func() error {
		if configPath == "" {
			return nil
		}
		f, err := os.Open(configPath)
		if err != nil {
			return err
		}
		defer f.Close()
		bytes, err := ioutil.ReadAll(f)
		if err != nil {
			return err
		}
		err = yaml.Unmarshal(bytes, &config, yaml.DisallowUnknownFields)
		if err != nil {
			return err
		}
		//klog.Infof("%#v", config)
		validAPIVersion := false
		for _, version := range cfg.APIVersions {
			if config.APIVersion == version {
				validAPIVersion = true
				break
			}
		}
		if !validAPIVersion {
			return fmt.Errorf("Unsupported APIVersion %s, supported: %v", config.APIVersion, cfg.APIVersions)
		}
		if config.Kind != cfg.Kind {
			return fmt.Errorf("Unsupported Kind %s, must be %s", config.Kind, cfg.Kind)
		}
		return nil
	}()
	if err != nil {
		return err
	}

	//klog.Infof("%#v", configOverrides)
	config.Override(&configOverrides)
	//klog.Infof("%#v", config)

	return nil
}

func getReleaseValues(ctx context.Context) error {
	return gosh.
		Command(helm.GetValues(&config.Helm, &config.Release, &config.Kubernetes, true)...).
		WithContext(ctx).
		WithStreams(gosh.FuncOut(gosh.SaveJSON(&releaseValues))).
		Run()
}

func rke2Enabled() bool {
	rke2, ok := releaseValues["rke2"].(map[string]interface{})
	if !ok {
		return false
	}
	enabled, ok := rke2["enabled"].(bool)
	if !ok {
		return false
	}
	return enabled
}

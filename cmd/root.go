/*
Copyright © 2022 NAME HERE <EMAIL ADDRESS>
*/
package cmd

import (
	"context"
	"errors"
	goflag "flag"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/yaml"
	k8srest "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"

	"github.com/meln5674/gosh"

	cfg "github.com/meln5674/kink/pkg/config"
	"github.com/meln5674/kink/pkg/helm"
)

const (
	ClusterConfigEnv = "KINKCONFIG"
	ClusterNameEnv   = "KINK_CLUSTER_NAME"
)

var (
	// configPath is the path to the config file provided as a flag
	configPath string
	// rawConfig is the configuration as parsed from configPath
	rawConfig cfg.RawConfig
	// configOverrides are the overrides provided via the command line
	configOverrides cfg.Config
	// config is the fully realized config ready to be passed to module functions
	config cfg.Config

	// releaseConfigMount is the location of the mounted configmap as provided on the command line
	releaseConfigMount string

	// releaseNamespace is the namespace of the helm release for the guest cluster
	releaseNamespace string

	// releaseConfig are the details of the last release of the guest cluster needed by local commands
	releaseConfig cfg.ReleaseConfig

	kubeconfig *k8srest.Config

	doRepoUpdate bool

// TODO: Get this by parsing config
)

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "kink",
	Short: "Kubernetes in Kubernetes",
	Long:  `Deploy Kubernetes clusters within other Kubernetes clusters`,
	// Uncomment the following line if your bare application
	// has an action associated with it:
	// Run: func(cmd *cobra.Command, args []string) { },
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error { return loadConfig() },
	SilenceUsage:      true,
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

	kinkconfig := os.Getenv(ClusterConfigEnv)
	clusterName := os.Getenv(ClusterNameEnv)
	if clusterName == "" {
		clusterName = cfg.DefaultClusterName
	}

	rootCmd.PersistentFlags().StringVar(&configPath, "config", kinkconfig, "Path to KinK config file to use instead of arguments")
	rootCmd.PersistentFlags().StringVar(&releaseConfigMount, "release-config-mount", "", "Path to where release configmap is mounted. If provided, this will be used instead of fetching from helm")

	rootCmd.PersistentFlags().StringSliceVar(&configOverrides.Helm.Command, "helm-command", []string{"helm"}, "Command to execute for helm")
	rootCmd.PersistentFlags().StringSliceVar(&configOverrides.Kubectl.Command, "kubectl-command", []string{"kubectl"}, "Command to execute for kubectl")
	rootCmd.PersistentFlags().StringSliceVar(&configOverrides.Docker.Command, "docker-command", []string{"docker"}, "Command to execute for docker")

	rootCmd.PersistentFlags().StringVar(&configOverrides.Chart.ChartName, "chart", "kink", "Name of KinK Helm Chart")
	rootCmd.PersistentFlags().StringVar(&configOverrides.Chart.Version, "chart-version", "", "Version of the chart to install")
	rootCmd.PersistentFlags().StringVar(&configOverrides.Chart.RepositoryURL, "repository-url", "https://meln5674.github.io/kink", "URL of KinK Helm Chart repository")
	rootCmd.Flags().BoolVar(&doRepoUpdate, "repo-update", true, "Update the helm repo before upgrading. Note that if a new chart version has become availabe since install or last upgrade, this will result in upgrading the chart. If this unacceptable, set this to false, or use --chart-version to pin a specific version")
	rootCmd.PersistentFlags().StringVar(&configOverrides.Release.ClusterName, "name", clusterName, "Name of the kink cluster")
	rootCmd.PersistentFlags().StringArrayVar(&configOverrides.Release.Values, "values", []string{}, "Extra values.yaml files to use when creating cluster")
	rootCmd.PersistentFlags().StringToStringVar(&configOverrides.Release.Set, "set", map[string]string{}, "Extra field overrides to use when creating cluster")
	rootCmd.PersistentFlags().StringToStringVar(&configOverrides.Release.SetString, "set-string", map[string]string{}, "Extra field overrides to use when creating cluster, forced interpreted as strings")
	// TODO: Add flags for docker
	rootCmd.PersistentFlags().StringVar(&configOverrides.Kubernetes.Kubeconfig, "kubeconfig", "", "Path to the kubeconfig file to use for CLI requests.")
	clientcmd.BindOverrideFlags(&configOverrides.Kubernetes.ConfigOverrides, rootCmd.PersistentFlags(), clientcmd.RecommendedConfigOverrideFlags(""))
}

func loadConfig() error {
	var err error
	if configPath != "" {
		err = rawConfig.LoadFromFile(configPath)
		if err != nil {
			return fmt.Errorf("Configuration file %s is invalid or missing: %s", configPath, err)
		}
		config = rawConfig.Format()
	}

	klog.V(1).Infof("%#v", configOverrides)
	config.Override(&configOverrides)
	klog.V(1).Infof("%#v", config)

	kubeconfigPath := config.Kubernetes.Kubeconfig
	if kubeconfigPath == "" {
		kubeconfigPath = os.Getenv(clientcmd.RecommendedConfigPathEnvVar)
	}
	releaseNamespace, _, err = clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		&clientcmd.ClientConfigLoadingRules{
			ExplicitPath: kubeconfigPath,
		},
		&config.Kubernetes.ConfigOverrides,
	).Namespace()
	if err != nil {
		return err
	}

	if releaseConfigMount != "" {
		err = releaseConfig.LoadFromMount(releaseConfigMount)
		if err != nil {
			return err
		}
	} else {
		doc := corev1.ConfigMap{}
		if !config.Chart.IsLocalChart() {
			klog.Info("Ensuring helm repo exists...")
			repoAdd := helm.RepoAdd(&config.Helm, &config.Chart)
			err = gosh.
				Command(repoAdd...).
				WithStreams(gosh.ForwardOutErr).
				Run()
			if err != nil {
				return err
			}
			if doRepoUpdate {
				repoUpdate := helm.RepoUpdate(&config.Helm, config.Chart.RepoName())
				klog.Info("Updating chart repo...")
				err = gosh.
					Command(repoUpdate...).
					WithContext(context.TODO()).
					WithStreams(gosh.ForwardOutErr).
					Run()
				if err != nil {
					return err
				}
			} else {
				klog.Info("Chart repo update skipped by flag")
			}
		}
		loadedConfig := false
		err = gosh.
			Command(helm.TemplateCluster(&config.Helm, &config.Chart, &config.Release, &config.Kubernetes)...).
			WithStreams(
				gosh.ForwardErr,
				gosh.FuncOut(func(r io.Reader) error {
					decoder := yaml.NewYAMLOrJSONDecoder(r, 1024)
					for {
						doc = corev1.ConfigMap{}
						err := decoder.Decode(&doc)
						if errors.Is(err, io.EOF) {
							return nil
						}
						if err != nil {
							// TODO: find a way to distinguish I/O errors and syntax errors from "not a configmap" errors
							klog.Warning(err)
							continue
						}
						klog.V(1).Infof("%s/%s/%s/%s", doc.APIVersion, doc.Kind, doc.Namespace, doc.Name)
						if doc.APIVersion != "v1" || doc.Kind != "ConfigMap" {
							continue
						}
						if doc.Namespace != releaseNamespace && doc.Namespace != "" {
							klog.Warning("Found a configmap other than the one we're looking for")
							continue
						}
						ok, err := releaseConfig.LoadFromConfigMap(&doc)
						if err != nil {
							return err
						}
						if ok {
							loadedConfig = true
							return nil
						}
						klog.Warning("Found a configmap other than the one we're looking for")
					}
				}),
			).
			Run()
		if err != nil {
			return err
		}
		if !loadedConfig {
			return fmt.Errorf("Did not find the expected cluster configmap in the helm template output. This could be a bug or your release values are invalid")
		}
	}
	klog.V(1).Infof("%#v", releaseConfig)
	kubeconfig, err = clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		&clientcmd.ClientConfigLoadingRules{
			ExplicitPath: kubeconfigPath,
		},
		&config.Kubernetes.ConfigOverrides,
	).ClientConfig()
	if err != nil {
		return err
	}

	return nil
}

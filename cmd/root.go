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
	"github.com/meln5674/rflag"

	"github.com/meln5674/kink/pkg/config"
	cfg "github.com/meln5674/kink/pkg/config"
	"github.com/meln5674/kink/pkg/docker"
	"github.com/meln5674/kink/pkg/helm"
	"github.com/meln5674/kink/pkg/kubectl"
)

const (
	ClusterConfigEnv = "KINKCONFIG"
	ClusterNameEnv   = "KINK_CLUSTER_NAME"
)

type kinkArgsT struct {
	ConfigPath         string `rflag:"name=config,usage=Path to KinK config file to use instead of arguments"`
	ReleaseConfigMount string `rflag:"usage=Path to where release configmap is mounted. If provided,, this will be used instead of fetching from helm"`

	HelmCommand    []string `rflag:"usage=Command to execute for helm"`
	KubectlCommand []string `rflag:"usage=Command to execute for kubectl"`
	DockerCommand  []string `rflag:"usage=Command to execute for docker"`

	ChartName              string `rflag:"name=chart,usage=Name of KinK Helm Chart"`
	ChartVersion           string `rflag:"usage=Version of the chart to install"`
	ChartRepository        string `rflag:"name=repository-url,usage=URL of KinK Helm Chart repository"`
	ChartRegistryPlainHTTP bool   `rflag:"name=registry-plain-http,usage=Use insecure HTTP to pull KinK Helm Chart from OCI"`
	DoRepoUpdate           bool   `rflag:"usage=Update the helm repo before upgrading. Note that if a new chart version has become availabe since install or last upgrade,, this will result in upgrading the chart. If this unacceptable,, set this to false,, or use --chart-version to pin a specific version"`

	ClusterName string `rflag:"name=name,usage=Name of the kink cluster"`

	ValuesFiles []string          `rflag:"usage=Extra values.yaml files to use when creating cluster"`
	Set         map[string]string `rflag:"usage=Extra field overrides to use when creating cluster"`
	SetString   map[string]string `rflag:"usage=Extra field overrides to use when creating cluster,, forced interpreted as strings"`

	Kubeconfig          string `rflag:"usage=Path to the kubeconfig file to use for CLI requests."`
	KubernetesOverrides clientcmd.ConfigOverrides

	// TODO: Add flags for docker
}

func (kinkArgsT) Defaults() kinkArgsT {
	clusterName := os.Getenv(ClusterNameEnv)
	if clusterName == "" {
		clusterName = cfg.DefaultClusterName
	}
	return kinkArgsT{
		ConfigPath: os.Getenv(ClusterConfigEnv),

		HelmCommand:    []string{"helm"},
		KubectlCommand: []string{"kubectl"},
		DockerCommand:  []string{"docker"},

		ChartName:       "kink",
		ChartRepository: "https://meln5674.github.io/kink",
		DoRepoUpdate:    true,

		ClusterName: clusterName,

		ValuesFiles: []string{},
		Set:         map[string]string{},
		SetString:   map[string]string{},

		Kubeconfig: os.Getenv(clientcmd.RecommendedConfigPathEnvVar),
	}
}

func (k kinkArgsT) ConfigOverrides() cfg.Config {
	return cfg.Config{
		Helm: helm.HelmFlags{
			Command: k.HelmCommand,
		},
		Kubectl: kubectl.KubectlFlags{
			Command: k.KubectlCommand,
		},
		Docker: docker.DockerFlags{
			Command: k.DockerCommand,
		},
		Chart: helm.ChartFlags{
			ChartName:     k.ChartName,
			Version:       k.ChartVersion,
			RepositoryURL: k.ChartRepository,
			PlainHTTP:     k.ChartRegistryPlainHTTP,
		},
		Release: helm.ClusterReleaseFlags{
			ClusterName: k.ClusterName,
			Values:      k.ValuesFiles,
			Set:         k.Set,
			SetString:   k.SetString,
		},
		Kubernetes: kubectl.KubeFlags{
			Kubeconfig:      k.Kubeconfig,
			ConfigOverrides: k.KubernetesOverrides,
		},
	}
}

var kinkArgs = kinkArgsT{}.Defaults()

type resolvedConfigT struct {
	KinkConfig       cfg.Config
	ReleaseNamespace string
	ReleaseConfig    cfg.ReleaseConfig
	Kubeconfig       *k8srest.Config
}

var resolvedConfig resolvedConfigT

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "kink",
	Short: "Kubernetes in Kubernetes",
	Long:  `Deploy Kubernetes clusters within other Kubernetes clusters`,
	// Uncomment the following line if your bare application
	// has an action associated with it:
	// Run: func(cmd *cobra.Command, args []string) { },
	SilenceUsage: true,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		gosh.GlobalLog = klog.Background()

		resolved, err := loadConfig(&kinkArgs)
		if err != nil {
			return err
		}
		resolvedConfig = *resolved

		return nil
	},
}

// exitCode is the exit code that main() will exit with.
// This is a global variable so that os.Exit does not need to be called until main would otherwise exit
// If an error is returned by any command, this value is ignored and instead klog.Fatal is used instead.
// As a result, this is mainly used when a specific error code needs to be forwarded, such as from exec
// and sh.
var exitCode int

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		klog.Fatal(err)
	}
	os.Exit(exitCode)
}

func init() {
	klogFlags := goflag.NewFlagSet("", goflag.PanicOnError)
	klog.InitFlags(klogFlags)
	rootCmd.PersistentFlags().AddGoFlagSet(klogFlags)

	rflag.MustRegister(rflag.ForPFlag(rootCmd.PersistentFlags()), "", &kinkArgs)
	clientcmd.BindOverrideFlags(&kinkArgs.KubernetesOverrides, rootCmd.PersistentFlags(), clientcmd.RecommendedConfigOverrideFlags(""))
}

type devNullT struct{}

var devNull = devNullT{}

var _ = io.Writer(devNull)

func (d devNullT) Write(b []byte) (int, error) {
	return len(b), nil
}

func loadConfig(args *kinkArgsT) (*resolvedConfigT, error) {
	var cfg resolvedConfigT
	var err error

	if args.ConfigPath != "" {
		var rawConfig config.RawConfig
		err = rawConfig.LoadFromFile(args.ConfigPath)
		if err != nil {
			return nil, fmt.Errorf("Configuration file %s is invalid or missing: %s", args.ConfigPath, err)
		}
		cfg.KinkConfig = rawConfig.Format()
	}

	overrides := args.ConfigOverrides()
	klog.V(1).Infof("%#v", &overrides)
	cfg.KinkConfig.Override(&overrides)
	klog.V(1).Infof("%#v", cfg.KinkConfig)

	clientConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		&clientcmd.ClientConfigLoadingRules{
			ExplicitPath: args.Kubeconfig,
		},
		&cfg.KinkConfig.Kubernetes.ConfigOverrides,
	)
	cfg.Kubeconfig, err = clientConfig.ClientConfig()
	if err != nil {
		return nil, err
	}

	cfg.ReleaseNamespace, _, err = clientConfig.Namespace()
	if err != nil {
		return nil, err
	}

	if args.ReleaseConfigMount != "" {
		err = cfg.ReleaseConfig.LoadFromMount(args.ReleaseConfigMount)
		if err != nil {
			return nil, err
		}
	} else {
		doc := corev1.ConfigMap{}
		if !cfg.KinkConfig.Chart.IsLocalChart() && !cfg.KinkConfig.Chart.IsOCIChart() {
			klog.Info("Ensuring helm repo exists...")
			repoAdd := helm.RepoAdd(&cfg.KinkConfig.Helm, &cfg.KinkConfig.Chart)
			err = gosh.
				Command(repoAdd...).
				WithStreams(gosh.ForwardOutErr).
				Run()
			if err != nil {
				return nil, err
			}
			if args.DoRepoUpdate {
				repoUpdate := helm.RepoUpdate(&cfg.KinkConfig.Helm, cfg.KinkConfig.Chart.RepoName())
				klog.Info("Updating chart repo...")
				err = gosh.
					Command(repoUpdate...).
					WithContext(context.TODO()).
					WithStreams(gosh.ForwardOutErr).
					Run()
				if err != nil {
					return nil, err
				}
			} else {
				klog.Info("Chart repo update skipped by flag")
			}
		}
		loadedConfig := false
		err = gosh.
			Command(helm.TemplateCluster(
				&cfg.KinkConfig.Helm,
				&cfg.KinkConfig.Chart,
				&cfg.KinkConfig.Release,
				&cfg.KinkConfig.Kubernetes,
			)...).
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
						if doc.Namespace != cfg.ReleaseNamespace && doc.Namespace != "" {
							klog.Warning("Found a configmap other than the one we're looking for")
							continue
						}
						ok, err := cfg.ReleaseConfig.LoadFromConfigMap(&doc)
						if err != nil {
							// If we don't flush its stdout, the helm template process never exits on windows
							io.Copy(devNull, r)
							return err
						}
						if ok {
							// See above
							io.Copy(devNull, r)
							loadedConfig = true
							return nil
						}
						klog.Warning("Found a configmap other than the one we're looking for")
					}
				}),
			).
			Run()
		if err != nil {
			return nil, err
		}
		if !loadedConfig {
			return nil, fmt.Errorf("Did not find the expected cluster configmap in the helm template output. This could be a bug or your release values are invalid")
		}
	}
	klog.V(1).Infof("%#v", cfg.ReleaseConfig)

	return &cfg, nil
}

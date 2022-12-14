/*
Copyright © 2022 NAME HERE <EMAIL ADDRESS>

*/
package cmd

import (
	"context"
	"fmt"
	"github.com/pkg/errors"
	"os"
	"os/exec"

	"github.com/meln5674/gosh"
	"github.com/spf13/cobra"
	"k8s.io/klog/v2"

	"github.com/meln5674/kink/pkg/kubectl"
)

var (
	execCommand []string
)

const (
	k3sKubeconfigPath  = "/etc/rancher/k3s/k3s.yaml"
	rke2KubeconfigPath = "/etc/rancher/rke2/rke2.yaml"
)

var (
	exportedKubeconfigPath string
	portForwardForExec     bool
)

// execCmd represents the exec command
var execCmd = &cobra.Command{
	Use:   "exec",
	Short: "Execute a new process with access to a cluster",
	Long: `Because kink clusters are contained within another cluster, their controlplane may not
be accessible from where you are running kink, unless you have made extra provisions such as Ingress
or a LoadBalancer Service.

To work around this, kink can use Kubernetes port-forwarding to provide
access to that controlplane. This command sets up that port forwarding, sets the KUBECONFIG variable
to a temporary file that will connect to it, and executes your shell command, then stop forwarding and
clean up the temporary kubeconfig once it has exited.

This command does not perform variable replacements or glob expansions. To do this, use 'kink sh'`,
	Run: func(cmd *cobra.Command, args []string) {
		if len(args) == 0 {
			klog.Fatal("A command is required")
		}

		execWithGateway(gosh.Command(args...))
	},
}

func init() {
	rootCmd.AddCommand(execCmd)

	// Here you will define your flags and configuration settings.

	// Cobra supports Persistent Flags which will work for this command
	// and all subcommands, e.g.:
	// execCmd.PersistentFlags().String("foo", "", "A help for foo")

	// Cobra supports local flags which will only run when this command
	// is called directly, e.g.:
	// execCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")
	execCmd.Flags().StringVar(&exportedKubeconfigPath, "exported-kubeconfig", "", "Path to kubeconfig exported during `create cluster` or `export kubeconfig` instead of copying it again")
	execCmd.Flags().BoolVar(&portForwardForExec, "port-forward", true, "Set up a localhost port forward for the controlplane during execution. Set to false if using a background `kink port-forward` command.")
}

func execWithGateway(toExec *gosh.Cmd) {
	ec, err := func() (*int, error) {
		ctx := context.TODO()

		var err error
		err = loadConfig()
		if err != nil {
			return nil, err
		}

		err = getReleaseValues(ctx)
		if err != nil {
			return nil, err
		}

		kubeconfigPath := k3sKubeconfigPath
		if rke2Enabled() {
			kubeconfigPath = rke2KubeconfigPath
		}

		if exportedKubeconfigPath == "" {
			kubeconfig, err := os.CreateTemp("", "kink-kubeconfig-*")
			defer kubeconfig.Close()
			defer os.Remove(kubeconfig.Name())
			if err != nil {
				return nil, err
			}
			// TODO: Find a live pod first
			kubectlCp := kubectl.Cp(&config.Kubectl, &config.Kubernetes, config.Release.Namespace, fmt.Sprintf("kink-%s-controlplane-0", config.Release.ClusterName), kubeconfigPath, kubeconfig.Name())
			err = gosh.
				Command(kubectlCp...).
				WithContext(ctx).
				WithStreams(gosh.ForwardOutErr).
				Run()
			if err != nil {
				return nil, errors.Wrap(err, "Could not extract kubeconfig from controlplane pod, make sure controlplane is healthy")
			}
			exportedKubeconfigPath = kubeconfig.Name()
		}

		if portForwardForExec {
			stopPortForward, err := portForward(ctx)
			if err != nil {
				return nil, err
			}
			defer stopPortForward()
		}

		err = toExec.
			WithContext(ctx).
			WithParentEnvAnd(map[string]string{
				"KUBECONFIG": exportedKubeconfigPath,
			}).
			WithStreams(gosh.ForwardAll).
			Run()
		var exitError *exec.ExitError
		if errors.As(err, &exitError) {
			ec := exitError.ProcessState.ExitCode()
			return &ec, nil
		}
		if err != nil {
			return nil, err
		}
		return nil, nil
	}()
	if err != nil {
		klog.Fatal(err)
	}
	if ec != nil {
		os.Exit(*ec)
	}
}

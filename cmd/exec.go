/*
Copyright © 2022 NAME HERE <EMAIL ADDRESS>

*/
package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/meln5674/gosh"
	"github.com/spf13/cobra"
	"k8s.io/klog/v2"

	"github.com/meln5674/kink/pkg/kubectl"
)

var (
	execCommand []string
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
}

func execWithGateway(toExec *gosh.Cmd) {
	ec, err := func() (*int, error) {
		ctx := context.TODO()

		var err error
		err = loadConfig()
		if err != nil {
			return nil, err
		}
		kubeconfig, err := os.CreateTemp("", "kink-kubeconfig-*")
		defer kubeconfig.Close()
		defer os.Remove(kubeconfig.Name())
		if err != nil {
			return nil, err
		}
		kubectlCp := kubectl.Cp(&config.Kubectl, &config.Kubernetes, config.Release.Namespace, fmt.Sprintf("kink-%s-controlplane-0", config.Release.ClusterName), "/etc/rancher/k3s/k3s.yaml", kubeconfig.Name())
		err = gosh.
			Command(kubectlCp...).
			WithContext(ctx).
			WithStreams(gosh.ForwardOutErr).
			Run()
		if err != nil {
			return nil, err
		}

		// TODO: Get service name/remote port from chart (helm get manifest)
		// TODO: Make local port configurable with flag
		kubectlPortForward := kubectl.PortForward(&config.Kubectl, &config.Kubernetes, config.Release.Namespace, fmt.Sprintf("svc/kink-%s-controlplane", config.Release.ClusterName), map[string]string{"6443": "6443"})
		kubectlPortForwardCmd := gosh.
			Command(kubectlPortForward...).
			WithContext(ctx).
			WithStreams(gosh.ForwardOutErr)

		err = kubectlPortForwardCmd.Start()

		if err != nil {
			return nil, err
		}
		defer func() {
			// Deliberately ignoing the errors here
			kubectlPortForwardCmd.Kill()
			kubectlPortForwardCmd.Wait()
		}()

		klog.Info("Waiting for cluster to be accessible on localhost...")
		kubectlVersion := kubectl.Version(&config.Kubectl, &config.Kubernetes)
		for err = errors.New("dummy"); err != nil; err = gosh.
			Command(kubectlVersion...).
			WithContext(ctx).
			WithStreams(gosh.ForwardOutErr).
			Run() {
			time.Sleep(5 * time.Second)
		}

		err = toExec.
			WithContext(ctx).
			WithParentEnvAnd(map[string]string{
				"KUBECONFIG": kubeconfig.Name(),
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

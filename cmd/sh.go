/*
Copyright © 2022 NAME HERE <EMAIL ADDRESS>

*/
package cmd

import (
	"context"
	"errors"
	"log"
	"os"
	"os/exec"
	"time"

	"github.com/meln5674/kink/pkg/command"
	"github.com/meln5674/kink/pkg/kubectl"
	"github.com/spf13/cobra"
)

var (
	shCommand = make([]string, 0)
)

// shCmd represents the sh command
var shCmd = &cobra.Command{
	Use:   "sh",
	Short: "Execute a shell command with access to a cluster",
	Long: `Because kink clusters are contained within another cluster, their controlplane may not
be accessible from where you are running kink, unless you have made extra provisions such as Ingress
or a LoadBalancer Service.

To work around this, kink can use Kubernetes port-forwarding to provide
access to that controlplane. This command sets up that port forwarding, sets the KUBECONFIG variable
to a temporary file that will connect to it, and executes your shell command, then stop forwarding and
clean up the temporary kubeconfig once it has exited.

If no arguments are provided, this instead runs an interactive shell, allowing you to, for example
interactively use tools like kubectl and helm to interact with your isolated cluster.`,
	Run: func(cmd *cobra.Command, args []string) {
		ec, err := func() (*int, error) {
			ctx := context.TODO()

			kubeconfig, err := os.CreateTemp("", "kink-kubeconfig-*")
			defer kubeconfig.Close()
			defer os.Remove(kubeconfig.Name())
			if err != nil {
				return nil, err
			}
			kubectlCp := kubectl.Cp(&kubectlFlags, &kubeFlags, releaseFlags.Namespace, "kink-kink-controlplane-0", "/etc/rancher/k3s/k3s.yaml", kubeconfig.Name())
			err = command.Command(ctx, kubectlCp...).ForwardOutErr().Run()
			if err != nil {
				return nil, err
			}

			// TODO: Get remote port from chart (configmap?)
			// TODO: Make local port configurable with flag
			// TOOD: Get service name from chart (configmap?)
			// TODO: defer killing this process
			kubectlPortForward := kubectl.PortForward(&kubectlFlags, &kubeFlags, releaseFlags.Namespace, "svc/kink-kink-controlplane", map[string]string{"6443": "6443"})
			kubectlPortForwardCmd := command.
				Command(ctx, kubectlPortForward...).
				ForwardOutErr()

			err = kubectlPortForwardCmd.Start()
			if err != nil {
				return nil, err
			}
			defer func() {
				// Deliberately ignoing the errors here
				kubectlPortForwardCmd.Kill()
				kubectlPortForwardCmd.Wait()
			}()

			log.Println("Waiting for cluster to be accessible on localhost...")
			kubectlVersion := kubectl.Version(&kubectlFlags, &kubeFlags)
			for err = errors.New("dummy"); err != nil; err = command.Command(ctx, kubectlVersion...).ForwardOutErr().Run() {
				time.Sleep(5 * time.Second)
			}

			// TODO: Make shell configurable with flag and default to the SHELL variable
			bash := []string{"/bin/bash"}
			if len(args) != 0 {
				bash = append(bash, "-c")
				bash = append(bash, args...)
			}
			err = command.
				Command(ctx, bash...).
				WithParentEnv().
				WithEnv(map[string]string{
					"KUBECONFIG": kubeconfig.Name(),
				}).
				ForwardAll().
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
			log.Fatal(err)
		}
		if ec != nil {
			os.Exit(*ec)
		}

	},
}

func init() {
	rootCmd.AddCommand(shCmd)

	// Here you will define your flags and configuration settings.

	// Cobra supports Persistent Flags which will work for this command
	// and all subcommands, e.g.:
	// shCmd.PersistentFlags().String("foo", "", "A help for foo")

	// Cobra supports local flags which will only run when this command
	// is called directly, e.g.:
	// shCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")
}

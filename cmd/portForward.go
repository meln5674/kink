/*
Copyright © 2022 NAME HERE <EMAIL ADDRESS>

*/
package cmd

import (
	"context"
	"fmt"
	"time"

	"github.com/meln5674/gosh"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"k8s.io/klog/v2"

	"github.com/meln5674/kink/pkg/kubectl"
)

// portForwardCmd represents the portForward command
var portForwardCmd = &cobra.Command{
	Use:   "port-forward",
	Short: "A brief description of your command",
	Long: `A longer description that spans multiple lines and likely contains examples
and usage of using your command. For example:

Cobra is a CLI library for Go that empowers applications.
This application is a tool to generate the needed files
to quickly create a Cobra application.`,
	Run: func(cmd *cobra.Command, args []string) {
		err := func() error {
			ctx := context.TODO()

			var err error
			err = loadConfig()
			if err != nil {
				return err
			}

			err = getReleaseValues(ctx)
			if err != nil {
				return err
			}

			stop, err := portForward(ctx)
			if err != nil {
				return err
			}
			defer stop()

			klog.Info("Started port-forwarding to controlplane")

			// TODO: Sleep until sigint
			chan interface{}(nil) <- nil

			klog.Info("Stopped port-forwarding to controlplane")
			return nil
		}()
		if err != nil {
			klog.Fatal(err)
		}
	},
}

func init() {
	rootCmd.AddCommand(portForwardCmd)

	// Here you will define your flags and configuration settings.

	// Cobra supports Persistent Flags which will work for this command
	// and all subcommands, e.g.:
	// portForwardCmd.PersistentFlags().String("foo", "", "A help for foo")

	// Cobra supports local flags which will only run when this command
	// is called directly, e.g.:
	// portForwardCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")
}

func portForward(ctx context.Context) (stop func() error, err error) {
	// TODO: Get service name/remote port from chart (helm get manifest)
	// TODO: Make local port configurable with flag
	kubectlPortForward := kubectl.PortForward(&config.Kubectl, &config.Kubernetes, config.Release.Namespace, fmt.Sprintf("svc/kink-%s-controlplane", config.Release.ClusterName), map[string]string{"6443": "6443"})
	kubectlPortForwardCmd := gosh.
		Command(kubectlPortForward...).
		WithContext(ctx).
		WithStreams(gosh.ForwardOutErr)

	err = kubectlPortForwardCmd.Start()
	if err != nil {
		return nil, errors.Wrap(err, "Failed to start port-forwarding to controlplane")
	}

	klog.Info("Waiting for cluster to be accessible on localhost...")
	kubectlVersion := kubectl.Version(&config.Kubectl, &config.Kubernetes)
	for err = errors.New("dummy"); err != nil; err = gosh.
		Command(kubectlVersion...).
		WithContext(ctx).
		WithStreams(gosh.ForwardOutErr).
		Run() {
		time.Sleep(5 * time.Second)
	}
	// TODO: Also forward ingress ports, if enabled

	return func() error {
		var err error
		err = kubectlPortForwardCmd.Kill()
		if err != nil {
			return err
		}
		err = kubectlPortForwardCmd.Wait()
		if err != nil {
			return err
		}
		return nil
	}, nil
}

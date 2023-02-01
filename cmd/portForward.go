/*
Copyright © 2022 NAME HERE <EMAIL ADDRESS>
*/
package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
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

			ctx2, stop, err := portForward(ctx, true)
			if err != nil {
				return err
			}
			defer stop()

			<-ctx2.Done()

			klog.Info("Started port-forwarding to controlplane")

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

func portForward(ctx context.Context, retry bool) (ctx2 context.Context, stop func() error, err error) {
	// TODO: Get service name/remote port from chart (helm get manifest)
	// TODO: Make local port configurable with flag
	ctx2, stopCtx := context.WithCancel(ctx)

	sigChan := make(chan os.Signal, 2)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig, ok := <-sigChan
		if !ok {
			klog.Info("Signal channel closed, exiting signal watch")
			return
		}
		klog.Infof("Got signal %s, stopping port forward", sig)
		stopCtx()
	}()

	lock := make(chan struct{}, 1)

	start := func() (*gosh.Cmd, error) {
		kubectlPortForward := kubectl.PortForward(&config.Kubectl, &config.Kubernetes, fmt.Sprintf("svc/kink-%s-controlplane", config.Release.ClusterName), map[string]string{"6443": "6443"})
		cmd := gosh.
			Command(kubectlPortForward...).
			WithContext(ctx2).
			WithStreams(gosh.ForwardOutErr)
		return cmd, cmd.Start()
	}

	kubectlPortForwardCmd, err := start()
	if err != nil {
		return nil, nil, errors.Wrap(err, "Failed to start port-forwarding to controlplane")
	}

	if retry {
		go func() {
			for {
				var err error
				func() {
					lock <- struct{}{}
					defer func() { <-lock }()
					if kubectlPortForwardCmd != nil {
						err = kubectlPortForwardCmd.Wait()
					}
				}()
				select {
				case _, ok := <-ctx2.Done():
					if !ok {
						klog.Info("Context canceled, stopping retry loop")
						return
					}
				default:
					func() {
						lock <- struct{}{}
						defer func() { <-lock }()
						if kubectlPortForwardCmd != nil {
							if err != nil {
								klog.Warning("Port-forwarding to controlplane failed, retrying...: ", err)
							} else {
								klog.Warning("Port-forwarding to controlplane stopped without error, retrying...")
							}
						}
						kubectlPortForwardCmd, err = start()
						if err != nil {
							klog.Error("Failed to start port-forwarding to controlplane", err)
							kubectlPortForwardCmd = nil
						}
					}()
				}
			}
		}()
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
	// TODO: Also forward ingress ports, if enabled, and any nodeport service ports

	return ctx2, func() error {
		var err error
		stopCtx()
		signal.Stop(sigChan)
		close(sigChan)
		lock <- struct{}{}
		defer func() { <-lock }()
		klog.Info("Stopping port-forward to controlplane...")
		err = kubectlPortForwardCmd.Kill()
		if err != nil {
			klog.Warning("Failed to kill port-forward, wait may never finish: ", err)
		}
		err = kubectlPortForwardCmd.Wait()
		klog.Info("Stopped port-forward to controlplane")
		if err != nil {
			return err
		}
		return nil
	}, nil
}

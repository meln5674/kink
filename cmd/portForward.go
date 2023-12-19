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
	"github.com/meln5674/rflag"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"k8s.io/klog/v2"

	"github.com/meln5674/kink/pkg/kubectl"
)

// portForwardCmd represents the port-forward command
var portForwardCmd = &cobra.Command{
	Use:          "port-forward",
	Short:        "Forard the controlplane and (if enabled) file-gateway to local ports",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx, stopSignals := WithCancelOnInterrupt(context.Background())
		defer stopSignals()

		stopPortForward, err := startPortForward(ctx, true, &portForwardArgs, &resolvedConfig)
		if err != nil {
			return err
		}
		defer klog.Info("Stopped port-forwarding to controlplane")
		defer stopPortForward()
		defer klog.Info("Stopping port-forwarding to controlplane")

		klog.Info("Started port-forwarding to controlplane")

		<-ctx.Done()

		return nil
	},
}

type portForwardArgsT struct {
	ControlplanePort int `rflag:"usage=The local port to forward from for controlplane (api server) connections"`
	FileGatewayPort  int `rflag:"usage=The local port to forward from for file gateway connections"`
}

func (portForwardArgsT) Defaults() portForwardArgsT {
	return portForwardArgsT{
		ControlplanePort: 6443,
		FileGatewayPort:  8443,
	}
}

var portForwardArgs = portForwardArgsT{}.Defaults()

func init() {
	rootCmd.AddCommand(portForwardCmd)

	rflag.MustRegister(rflag.ForPFlag(portForwardCmd.Flags()), "", &portForwardArgs)
}

func WithCancelOnInterrupt(ctx context.Context) (_ context.Context, cancel func()) {
	ctx2, cancelInner := context.WithCancel(ctx)

	sigChan := make(chan os.Signal, 2)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig, ok := <-sigChan
		if !ok {
			klog.Info("Signal channel closed, exiting signal watch")
			return
		}
		klog.Infof("Got signal %s, stopping port forward", sig)
		cancelInner()
	}()

	return ctx2, func() {
		signal.Stop(sigChan)
		cancelInner()
		close(sigChan)
	}
}

func makePortForwardCmd(ctx context.Context, args *portForwardArgsT, cfg *resolvedConfigT) (*gosh.Cmd, error) {
	ports := map[string]string{
		fmt.Sprintf("%d", args.ControlplanePort): fmt.Sprintf("%d", cfg.ReleaseConfig.ControlplanePort),
	}
	if cfg.ReleaseConfig.FileGatewayEnabled {
		ports[fmt.Sprintf("%d", args.FileGatewayPort)] = fmt.Sprintf("%d", cfg.ReleaseConfig.FileGatewayContainerPort)
	}
	kubectlPortForward := kubectl.PortForward(
		&cfg.KinkConfig.Kubectl, &cfg.KinkConfig.Kubernetes,
		fmt.Sprintf("svc/%s", cfg.ReleaseConfig.ControlplaneFullname),
		ports,
	)
	cmd := gosh.
		Command(kubectlPortForward...).
		WithContext(ctx).
		WithStreams(gosh.ForwardOutErr)
	return cmd, cmd.Start()
}

func cmdRetryLoop(ctx context.Context, lock chan struct{}, logMsg string, cmdPtr **gosh.Cmd, mkCmd func() (*gosh.Cmd, error)) error {
	for {
		var err error
		func() {
			lock <- struct{}{}
			defer func() { <-lock }()
			if *cmdPtr != nil {
				err = (*cmdPtr).Wait()
			}
		}()
		select {
		case _, ok := <-ctx.Done():
			if !ok {
				klog.Info("Context canceled, stopping retry loop")
				return nil
			}
		default:
			func() {
				lock <- struct{}{}
				defer func() { <-lock }()
				if *cmdPtr != nil {
					if err != nil {
						klog.Warningf("%s failed, retrying...: %s", logMsg, err)
					} else {
						klog.Warningf("%s stopped without error, retrying...: %s", logMsg, err)
					}
				}
				*cmdPtr, err = mkCmd()
				if err != nil {
					klog.Errorf("Failed to start %s: %s", logMsg, err)
					*cmdPtr = nil
				}
			}()
		}
	}
}

func startAndRetryInBackground(ctx context.Context, retry bool, lock chan struct{}, logMsg string, cmdPtr **gosh.Cmd, mkCmd func() (*gosh.Cmd, error)) (stop func() error, err error) {
	*cmdPtr, err = mkCmd()
	if err != nil {
		return nil, errors.Wrapf(err, "Failed to %s", logMsg)
	}

	if !retry {
		return func() error { return nil }, nil
	}

	go cmdRetryLoop(ctx, lock, logMsg, cmdPtr, mkCmd)

	return func() error {
		lock <- struct{}{}
		defer func() { <-lock }()
		if *cmdPtr == nil {
			return nil
		}
		klog.Infof("Stopping %s...", logMsg)
		err := (*cmdPtr).Kill()
		if err != nil {
			klog.Warningf("Failed to kill %s, wait may never finish: %s", logMsg, err)
		}
		err = (*cmdPtr).Wait()
		klog.Infof("Stopped %s", logMsg)
		if err != nil {
			return err
		}
		return nil
	}, nil

}

func startPortForward(ctx context.Context, retry bool, args *portForwardArgsT, cfg *resolvedConfigT) (stop func() error, err error) {
	lock := make(chan struct{}, 1)

	var kubectlPortForwardCmd *gosh.Cmd
	stop, err = startAndRetryInBackground(ctx, retry, lock, "Port-forwarding to controlplane", &kubectlPortForwardCmd, func() (*gosh.Cmd, error) {
		return makePortForwardCmd(ctx, args, cfg)
	})
	if err != nil {
		return nil, err
	}

	klog.Info("Waiting for cluster to be accessible on localhost...")
	kubectlVersion := kubectl.Version(&cfg.KinkConfig.Kubectl, &cfg.KinkConfig.Kubernetes)
	for err = errors.New("dummy"); err != nil; err = gosh.
		Command(kubectlVersion...).
		WithContext(ctx).
		WithStreams(gosh.ForwardOutErr).
		Run() {
		time.Sleep(5 * time.Second)
	}
	// TODO: Also forward ingress ports, if enabled, and any nodeport service ports

	return stop, err
}

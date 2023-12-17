/*
Copyright © 2022 NAME HERE <EMAIL ADDRESS>
*/
package cmd

import (
	"context"
	"os"
	"os/exec"

	"github.com/pkg/errors"

	"github.com/meln5674/gosh"
	"github.com/meln5674/rflag"
	"github.com/spf13/cobra"
)

var (
	execCommand []string
)

const (
	k3sKubeconfigPath  = "/etc/rancher/k3s/k3s.yaml"
	rke2KubeconfigPath = "/etc/rancher/rke2/rke2.yaml"
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
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			return errors.New("A command is required")
		}

		maybeExitCode, err := execWithGateway(context.Background(), gosh.Command(args...), &execArgs, &resolvedConfig)

		if err != nil {
			return err
		}
		if maybeExitCode != nil {
			exitCode = *maybeExitCode
		}
		return nil
	},
}

type execArgsT struct {
	PortForwardArgs        portForwardArgsT `rflag:""`
	PortForward            bool             `rflag:"usage=Set up a localhost port forward for the controlplane during execution. Set to false if using a background 'kink port-forward' command or running in-cluster"`
	ExportedKubeconfigPath string           `rflag:"name=exported-kubeconfig,usage=Path to kubeconfig exported during 'create cluster' or 'export kubeconfig' instead of copying it again"`
}

func (execArgsT) Defaults() execArgsT {
	return execArgsT{
		PortForwardArgs: portForwardArgsT{}.Defaults(),
		PortForward:     true,
	}
}

var execArgs = execArgsT{}.Defaults()

func init() {
	rootCmd.AddCommand(execCmd)
	rflag.MustRegister(rflag.ForPFlag(execCmd.Flags()), "", &execArgs)
}

func execWithGateway(ctx context.Context, toExec *gosh.Cmd, args *execArgsT, cfg *resolvedConfigT) (exitCode *int, err error) {
	exportedKubeconfigPath := args.ExportedKubeconfigPath
	if exportedKubeconfigPath == "" {
		kubeconfig, err := os.CreateTemp("", "kink-kubeconfig-*")
		if err != nil {
			return nil, err
		}
		defer kubeconfig.Close()
		defer os.Remove(kubeconfig.Name())
		err = fetchKubeconfig(ctx, cfg, kubeconfig.Name())
		if err != nil {
			return nil, err
		}
		exportedKubeconfigPath = kubeconfig.Name()
	}

	if args.PortForward {
		portForwardCtx, cancelPortForward := context.WithCancel(ctx)
		stopPortForward, err := startPortForward(portForwardCtx, true, &args.PortForwardArgs, cfg)
		if err != nil {
			cancelPortForward()
			return nil, err
		}
		defer stopPortForward()
		defer cancelPortForward()
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
}

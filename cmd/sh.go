/*
Copyright © 2022 NAME HERE <EMAIL ADDRESS>
*/
package cmd

import (
	"context"
	"strings"

	"github.com/meln5674/gosh"
	"github.com/meln5674/rflag"
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
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		var sh *gosh.Cmd
		if len(args) == 0 {
			sh = gosh.Shell("")
			sh = gosh.Command(sh.Cmd.Args[0])
		} else {
			sh = gosh.Shell(strings.Join(args, " "))
		}
		maybeExitCode, err := execWithGateway(context.Background(), sh, &shArgs.ExecArgs, &resolvedConfig)
		if err != nil {
			return err
		}
		if maybeExitCode != nil {
			exitCode = *maybeExitCode
		}
		return nil
	},
}

type shArgsT struct {
	ExecArgs execArgsT `rflag:""`
}

func (shArgsT) Defaults() shArgsT {
	return shArgsT{
		ExecArgs: execArgsT{}.Defaults(),
	}
}

var shArgs = shArgsT{}.Defaults()

func init() {
	rootCmd.AddCommand(shCmd)
	rflag.MustRegister(rflag.ForPFlag(shCmd.Flags()), "", &shArgs)
}

/*
Copyright © 2022 NAME HERE <EMAIL ADDRESS>

*/
package cmd

import (
	"strings"

	"github.com/meln5674/gosh"
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
		var sh *gosh.Cmd
		if len(args) == 0 {
			sh = gosh.Shell("")
			sh = gosh.Command(sh.Cmd.Args[0])
		} else {
			sh = gosh.Shell(strings.Join(args, " "))
		}
		execWithGateway(sh)
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
	shCmd.Flags().StringVar(&exportedKubeconfigPath, "exported-kubeconfig", "", "Path to kubeconfig exported during `create cluster` or `export kubeconfig` instead of copying it again")
}

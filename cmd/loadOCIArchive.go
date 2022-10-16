/*
Copyright © 2022 NAME HERE <EMAIL ADDRESS>

*/
package cmd

import (
	"context"
	"os"

	"github.com/meln5674/kink/pkg/command"
	"github.com/meln5674/kink/pkg/kubectl"
	"log"

	"github.com/spf13/cobra"
)

var (
	ociArchivesToLoad []string
)

// ociArchiveCmd represents the ociArchive command
var ociArchiveCmd = &cobra.Command{
	Use:   "oci-archive",
	Short: "A brief description of your command",
	Long: `A longer description that spans multiple lines and likely contains examples
and usage of using your command. For example:

Cobra is a CLI library for Go that empowers applications.
This application is a tool to generate the needed files
to quickly create a Cobra application.`,
	Run: func(cmd *cobra.Command, args []string) {
		err := func() error {
			if len(ociArchivesToLoad) == 0 {
				log.Println("No images specified")
				os.Exit(1)
			}
			ctx := context.TODO()
			podNames := make([]string, 0)
			getPods := kubectl.GetPods(&kubectlFlags, &kubeFlags, releaseFlags.Namespace, releaseFlags.ExtraLabels())
			err := command.
				Command(ctx, getPods...).
				ForwardErr().
				ProcessOut(findWorkerPods(&podNames)).
				Run()
			if err != nil {
				return err
			}
			imports := make([]command.Commander, 0, len(podNames)*len(ociArchivesToLoad))
			for _, archive := range ociArchivesToLoad {
				for _, podName := range podNames {
					kubectlExec := kubectl.Exec(&kubectlFlags, &kubeFlags, releaseFlags.Namespace, podName, true, false, "k3s", "ctr", "image", "import", "-")
					cmd := command.
						Command(ctx, kubectlExec...).
						ForwardOutErr()
					err = cmd.FileIn(archive)
					if err != nil {
						return err
					}
					imports = append(imports, cmd)
				}
			}
			// TODO: Replace this with a goroutine that copies from one docker save to each kubectl exec
			parallelCount := parallelLoads
			if parallelCount == -1 {
				parallelCount = len(imports)
			}
			err = command.FanOut(parallelCount, imports...)
			if err != nil {
				return err
			}
			return nil
		}()
		if err != nil {
			log.Fatal(err)
		}

	},
}

func init() {
	loadCmd.AddCommand(ociArchiveCmd)

	// Here you will define your flags and configuration settings.

	// Cobra supports Persistent Flags which will work for this command
	// and all subcommands, e.g.:
	// ociArchiveCmd.PersistentFlags().String("foo", "", "A help for foo")

	// Cobra supports local flags which will only run when this command
	// is called directly, e.g.:
	// ociArchiveCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")
	ociArchiveCmd.Flags().StringArrayVar(&ociArchivesToLoad, "archive", []string{}, "Paths to archives to load")
}

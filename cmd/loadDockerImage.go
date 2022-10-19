/*
Copyright © 2022 NAME HERE <EMAIL ADDRESS>

*/
package cmd

import (
	"context"
	"os"

	"github.com/meln5674/gosh/pkg/command"
	"github.com/meln5674/kink/pkg/docker"
	"github.com/meln5674/kink/pkg/kubectl"
	"log"

	"github.com/spf13/cobra"
)

var (
	dockerImagesToLoad []string
)

// dockerImageCmd represents the dockerImage command
var dockerImageCmd = &cobra.Command{
	Use:   "docker-image",
	Short: "Loads docker images from host docker daemon to all nodes",
	Run: func(cmd *cobra.Command, args []string) {
		err := func() error {
			if len(dockerImagesToLoad) == 0 {
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
			imports := make([]command.Commander, 0, len(podNames))
			for _, podName := range podNames {
				kubectlExec := kubectl.Exec(&kubectlFlags, &kubeFlags, releaseFlags.Namespace, podName, true, false, "k3s", "ctr", "image", "import", "-")
				dockerSave := docker.Save(&dockerFlags, dockerImagesToLoad...)
				pipeline, err := command.
					NewPipeline(
						command.Command(ctx, dockerSave...),
						command.Command(ctx, kubectlExec...),
					)
				if err != nil {
					return err
				}
				pipeline.ForwardErr()
				imports = append(imports, pipeline)
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
	loadCmd.AddCommand(dockerImageCmd)

	// Here you will define your flags and configuration settings.

	// Cobra supports Persistent Flags which will work for this command
	// and all subcommands, e.g.:
	// dockerImageCmd.PersistentFlags().String("foo", "", "A help for foo")

	// Cobra supports local flags which will only run when this command
	// is called directly, e.g.:
	// dockerImageCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")
	dockerImageCmd.Flags().StringArrayVar(&dockerImagesToLoad, "image", []string{}, "Images to load")
}

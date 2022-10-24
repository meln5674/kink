/*
Copyright © 2022 NAME HERE <EMAIL ADDRESS>

*/
package cmd

import (
	"context"

	"github.com/meln5674/gosh"
	"github.com/spf13/cobra"
	"k8s.io/klog/v2"

	"github.com/meln5674/kink/pkg/docker"
	"github.com/meln5674/kink/pkg/kubectl"
)

var (
	dockerImagesToLoad []string
)

// dockerImageCmd represents the dockerImage command
var dockerImageCmd = &cobra.Command{
	Use:   "docker-image",
	Short: "Loads docker images from host docker daemon to all nodes",
	Run: func(cmd *cobra.Command, args []string) {
		if len(dockerImagesToLoad) == 0 {
			klog.Fatal("No images specified")
		}
		err := func() error {
			ctx := context.TODO()
			pods, err := getPods(ctx)
			if err != nil {
				return err
			}
			imports := make([]gosh.Commander, 0, len(pods.Items))
			for _, pod := range pods.Items {
				kubectlExec := kubectl.Exec(
					&kubectlFlags, &kubeFlags,
					releaseFlags.Namespace, pod.Name,
					true, false,
					"k3s", "ctr", "image", "import", "-",
				)
				dockerSave := docker.Save(&dockerFlags, dockerImagesToLoad...)
				pipeline := gosh.Pipeline(
					gosh.Command(dockerSave...).WithContext(ctx),
					gosh.Command(kubectlExec...).WithContext(ctx),
				).WithStreams(gosh.ForwardErr)
				imports = append(imports, pipeline)
			}
			// TODO: Replace this with a goroutine that copies from one docker save to each kubectl exec
			err = importParallel(imports...)
			if err != nil {
				return err
			}
			return nil
		}()
		if err != nil {
			klog.Fatal(err)
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

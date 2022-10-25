/*
Copyright © 2022 NAME HERE <EMAIL ADDRESS>

*/
package cmd

import (
	"context"
	"k8s.io/klog/v2"

	"github.com/spf13/cobra"

	"github.com/meln5674/gosh"
	"github.com/meln5674/kink/pkg/kubectl"
)

var (
	dockerArchivesToLoad []string
)

// loadDockerArchiveCmd represents the dockerArchive command
var loadDockerArchiveCmd = &cobra.Command{
	Use:   "docker-archive",
	Short: "Loads docker-formatted archives from the host to all nodes",
	Run: func(cmd *cobra.Command, args []string) {
		loadArchives(dockerArchivesToLoad...)
	},
}

func init() {
	loadCmd.AddCommand(loadDockerArchiveCmd)

	// Here you will define your flags and configuration settings.

	// Cobra supports Persistent Flags which will work for this command
	// and all subcommands, e.g.:
	// dockerArchiveCmd.PersistentFlags().String("foo", "", "A help for foo")

	// Cobra supports local flags which will only run when this command
	// is called directly, e.g.:
	// dockerArchiveCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")
	loadDockerArchiveCmd.Flags().StringArrayVar(&dockerArchivesToLoad, "archive", []string{}, "Paths to archives to load")
}

func loadArchives(archives ...string) {
	err := func() error {

		err := loadConfig()
		if err != nil {
			return err
		}
		if len(archives) == 0 {
			klog.Fatal("No images specified")
		}
		ctx := context.TODO()
		pods, err := getPods(ctx)
		if err != nil {
			return err
		}
		imports := make([]gosh.Commander, 0, len(pods.Items)*len(dockerArchivesToLoad))
		for _, archive := range dockerArchivesToLoad {
			for _, pod := range pods.Items {
				kubectlExec := kubectl.Exec(
					&config.Kubectl, &config.Kubernetes,
					config.Release.Namespace, pod.Name,
					true, false,
					"k3s", "ctr", "image", "import", "-",
				)
				cmd := gosh.
					Command(kubectlExec...).
					WithContext(ctx).
					WithStreams(
						gosh.FileIn(archive),
						gosh.ForwardOutErr,
					)
				imports = append(imports, cmd)
			}
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

}

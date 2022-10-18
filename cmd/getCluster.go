/*
Copyright © 2022 NAME HERE <EMAIL ADDRESS>

*/
package cmd

import (
	"context"
	"fmt"
	"io"
	"log"

	"encoding/json"

	"github.com/meln5674/kink/pkg/command"
	"github.com/meln5674/kink/pkg/helm"

	"github.com/spf13/cobra"
)

func findKinkReleases(releases *[]map[string]interface{}) func(io.Reader) error {
	return func(stdout io.Reader) error {
		decoder := json.NewDecoder(stdout)
		allReleases := make([]map[string]interface{}, 0)

		err := decoder.Decode(&allReleases)
		if err != nil {
			return err
		}

		for _, release := range allReleases {
			name := release["name"].(string)
			if helm.IsKinkRelease(name) {
				*releases = append(*releases, release)
			}
		}
		return nil
	}
}

// getClusterCmd represents the getCluster command
var getClusterCmd = &cobra.Command{
	Use:   "cluster",
	Short: "Lists existing kink clusters by their name",
	Run: func(cmd *cobra.Command, args []string) {
		err := func() error {

			ctx := context.TODO()

			releases := make([]map[string]interface{}, 0)

			helmList := helm.List(&helmFlags, &chartFlags, &releaseFlags, &kubeFlags)
			err := command.
				Command(ctx, helmList...).
				ForwardErr().
				ProcessOut(findKinkReleases(&releases)).
				Run()
			if err != nil {
				return err
			}

			for _, release := range releases {
				if releaseFlags.Namespace != "" {
					fmt.Printf("%s %s\n", release["namespace"], release["name"])
				} else {
					fmt.Println(release["name"])
				}
			}

			return nil
		}()
		if err != nil {
			log.Fatal(err)
		}

	},
}

func init() {
	getCmd.AddCommand(getClusterCmd)

	// Here you will define your flags and configuration settings.

	// Cobra supports Persistent Flags which will work for this command
	// and all subcommands, e.g.:
	// getClusterCmd.PersistentFlags().String("foo", "", "A help for foo")

	// Cobra supports local flags which will only run when this command
	// is called directly, e.g.:
	// getClusterCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")
}

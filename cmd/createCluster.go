/*
Copyright © 2022 NAME HERE <EMAIL ADDRESS>

*/
package cmd

import (
	"context"
	"log"

	"github.com/spf13/cobra"

	"github.com/meln5674/kink/pkg/command"
	"github.com/meln5674/kink/pkg/helm"
)

// createClusterCmd represents the createCluster command
var createClusterCmd = &cobra.Command{
	Use:   "cluster",
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
			if chartFlags.IsLocalChart() {
				log.Println("Using local chart, skipping `repo add`...")
			} else {
				log.Println("Ensuring helm repo exists...")
				repoAdd := helm.RepoAdd(&helmFlags, &chartFlags, &releaseFlags)
				err = command.
					Command(ctx, repoAdd...).
					ForwardOutErr().
					Run()
				if err != nil {
					return err
				}
			}
			log.Println("Deploying chart...")
			helmUpgrade := helm.Upgrade(&helmFlags, &chartFlags, &releaseFlags, &kubeFlags)
			err = command.
				Command(ctx, helmUpgrade...).
				ForwardOutErr().
				Run()
			if err != nil {
				return err
			}
			log.Println("Deployed chart, your cluster is now ready to use")
			return nil
		}()
		if err != nil {
			log.Fatal(err)
		}
	},
}

func init() {
	createCmd.AddCommand(createClusterCmd)

	// Here you will define your flags and configuration settings.

	// Cobra supports Persistent Flags which will work for this command
	// and all subcommands, e.g.:
	// createClusterCmd.PersistentFlags().String("foo", "", "A help for foo")

	// Cobra supports local flags which will only run when this command
	// is called directly, e.g.:
	// createClusterCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")
}

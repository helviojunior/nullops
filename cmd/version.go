package cmd

import (
	"fmt"

	"github.com/helviojunior/nullops/internal/ascii"
	"github.com/helviojunior/nullops/internal/version"
	"github.com/spf13/cobra"
)

var releaseOnly = false

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Get the nullops version",
	Long:  ascii.LogoHelp(`Get the nullops version.`),
	Run: func(cmd *cobra.Command, args []string) {
		if releaseOnly {
			fmt.Printf("%s\n", version.Version)
			return
		}
		fmt.Println(ascii.Logo())
		fmt.Println("Author: Helvio Junior (m4v3r1ck)")
		fmt.Println("Source: https://github.com/helviojunior/nullops")
		fmt.Printf("Version: %s\nGit hash: %s\nBuild env: %s\nBuild time: %s\n\n",
			version.Version, version.GitHash, version.GoBuildEnv, version.GoBuildTime)
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
	versionCmd.Flags().BoolVarP(&releaseOnly, "release", "r", false, "Show release only")
}

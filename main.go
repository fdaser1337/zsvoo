package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"zsvo/cmd"
)

var rootCmd = &cobra.Command{
	Use:   "zsvo",
	Short: "A simple source-based package manager",
	Long: `A minimal package manager for custom Linux distributions based on LFS

Available commands:
  build     Build a package from recipe
  install   Install package(s) from package files
  upgrade   Upgrade package(s) from package files
  remove    Remove installed package(s)
  list      List installed packages
  info      Show package information
  help      Show help for a command
`,
}

func init() {
	// Register all commands
	rootCmd.AddCommand(cmd.BuildCmd)
	rootCmd.AddCommand(cmd.InstallCmd)
	rootCmd.AddCommand(cmd.UpgradeCmd)
	rootCmd.AddCommand(cmd.RemoveCmd)
	rootCmd.AddCommand(cmd.ListCmd)
	rootCmd.AddCommand(cmd.InfoCmd)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

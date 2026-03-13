package main

import (
	"log"
	"os"

	"github.com/spf13/cobra"
	"zsvo/cmd"
	"zsvo/pkg/i18n"
)

var rootCmd = &cobra.Command{
	Use:           "zsvo",
	Short:         "A simple source-based package manager",
	SilenceErrors: true,
	SilenceUsage:  true,
	Long: `A minimal package manager for custom Linux distributions based on LFS

Available commands:
  build     Build a package from recipe
  install   Install package(s) from local files or auto-build by name
  upgrade   Upgrade package(s) from package files
  remove    Remove installed package(s)
  list      List installed packages
  info      Show package information
  doctor    Check system for potential issues
  cache     Manage build cache
  search    Search for packages in Debian repositories
  lang      Set or display interface language
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
	rootCmd.AddCommand(cmd.DoctorCmd)
	rootCmd.AddCommand(cmd.CacheCmd)
	rootCmd.AddCommand(cmd.SearchCmd)
	rootCmd.AddCommand(cmd.LangCmd)
}

func main() {
	// Detect language from environment
	i18n.DetectLanguage()
	
	// Set up logging
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	
	if err := rootCmd.Execute(); err != nil {
		log.Printf("Error: %v", err)
		os.Exit(1)
	}
}

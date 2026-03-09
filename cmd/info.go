package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"zsvo/pkg/installer"
)

var InfoCmd = &cobra.Command{
	Use:   "info <package>",
	Short: "Show package information",
	Long:  `Show information about an installed package`,
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		packageName := args[0]

		// Create installer
		rootDir, _ := cmd.Flags().GetString("root")
		if rootDir == "" {
			rootDir = "/"
		}

		i := installer.NewInstaller(rootDir)

		// Get package info
		pkgInfo, err := i.GetPackageInfo(packageName)
		if err != nil {
			return fmt.Errorf("failed to get package info: %w", err)
		}

		fmt.Printf("Package: %s\n", pkgInfo.Name)
		fmt.Printf("Version: %s\n", pkgInfo.Version)
		if pkgInfo.Description != "" {
			fmt.Printf("Description: %s\n", pkgInfo.Description)
		}
		if len(pkgInfo.Dependencies) > 0 {
			fmt.Printf("Dependencies: %v\n", pkgInfo.Dependencies)
		}
		fmt.Printf("Install date: %s\n", pkgInfo.InstallDate)
		fmt.Printf("Files (%d):\n", len(pkgInfo.Files))
		for _, file := range pkgInfo.Files {
			fmt.Printf("  %s\n", file)
		}
		return nil
	},
}

func init() {
	InfoCmd.Flags().StringP("root", "r", "/", "Root directory for installation")
}

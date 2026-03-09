package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"zsvo/pkg/installer"
)

var ListCmd = &cobra.Command{
	Use:   "list",
	Short: "List installed packages",
	Long:  `List all installed packages`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Create installer
		rootDir, _ := cmd.Flags().GetString("root")
		if rootDir == "" {
			rootDir = "/"
		}

		i := installer.NewInstaller(rootDir)

		// List installed packages
		packages, err := i.ListInstalled()
		if err != nil {
			return fmt.Errorf("failed to list packages: %w", err)
		}

		if len(packages) == 0 {
			fmt.Println("No packages installed")
			return nil
		}

		fmt.Println("Installed packages:")
		for _, pkg := range packages {
			fmt.Printf("  %s\n", pkg)
		}
		return nil
	},
}

func init() {
	ListCmd.Flags().StringP("root", "r", "/", "Root directory for installation")
}

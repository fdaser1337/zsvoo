package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"zsvo/pkg/installer"
)

var RemoveCmd = &cobra.Command{
	Use:   "remove <package>",
	Short: "Remove a package",
	Long:  `Remove an installed package`,
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		packageName := args[0]

		// Create installer
		rootDir, _ := cmd.Flags().GetString("root")
		if rootDir == "" {
			rootDir = "/"
		}

		i := installer.NewInstaller(rootDir)

		// Remove package
		fmt.Printf("Removing package %s...\n", packageName)
		if err := i.Remove(packageName); err != nil {
			return fmt.Errorf("failed to remove package: %w", err)
		}

		fmt.Printf("Package %s removed successfully\n", packageName)
		return nil
	},
}

func init() {
	RemoveCmd.Flags().StringP("root", "r", "/", "Root directory for installation")
}

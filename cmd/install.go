package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"zsvo/pkg/installer"
)

var InstallCmd = &cobra.Command{
	Use:   "install <package> [package...]",
	Short: "Install package(s)",
	Long:  `Install one or more packages from package files`,
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		// Create installer
		rootDir, _ := cmd.Flags().GetString("root")
		if rootDir == "" {
			rootDir = "/"
		}

		i := installer.NewInstaller(rootDir)

		// Install packages
		if len(args) == 1 {
			fmt.Printf("Installing package from %s...\n", args[0])
		} else {
			fmt.Printf("Installing %d packages...\n", len(args))
		}
		if err := i.InstallMany(args); err != nil {
			return fmt.Errorf("failed to install packages: %w", err)
		}

		fmt.Printf("Package installation completed successfully\n")
		return nil
	},
}

func init() {
	InstallCmd.Flags().StringP("root", "r", "/", "Root directory for installation")
}

package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"zsvo/pkg/installer"
)

var InstallCmd = &cobra.Command{
	Use:   "install <package>",
	Short: "Install a package",
	Long:  `Install a package from a package file`,
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		packagePath := args[0]

		// Create installer
		rootDir, _ := cmd.Flags().GetString("root")
		if rootDir == "" {
			rootDir = "/"
		}

		i := installer.NewInstaller(rootDir)

		// Install package
		fmt.Printf("Installing package from %s...\n", packagePath)
		if err := i.Install(packagePath); err != nil {
			return fmt.Errorf("failed to install package: %w", err)
		}

		fmt.Printf("Package installed successfully\n")
		return nil
	},
}

func init() {
	InstallCmd.Flags().StringP("root", "r", "/", "Root directory for installation")
}

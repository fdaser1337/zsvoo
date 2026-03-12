package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"zsvo/pkg/installer"
)

var UpgradeCmd = &cobra.Command{
	Use:   "upgrade <package> [package...]",
	Short: "Upgrade package(s)",
	Long:  `Upgrade one or more packages from local package files`,
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		rootDir, _ := cmd.Flags().GetString("root")
		if rootDir == "" {
			rootDir = "/"
		}

		i := installer.NewInstaller(rootDir)

		fmt.Printf("Upgrading %d package(s)...\n", len(args))
		if err := i.Upgrade(args); err != nil {
			return fmt.Errorf("failed to upgrade packages: %w", err)
		}

		fmt.Printf("Package upgrade completed successfully\n")
		return nil
	},
}

func init() {
	UpgradeCmd.Flags().StringP("root", "r", "/", "Root directory for installation")
}

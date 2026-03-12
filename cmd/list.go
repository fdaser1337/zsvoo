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
		orphansOnly, _ := cmd.Flags().GetBool("orphans")

		var (
			packages []string
			err      error
		)
		if orphansOnly {
			packages, err = i.ListOrphans()
		} else {
			packages, err = i.ListInstalled()
		}
		if err != nil {
			return fmt.Errorf("failed to list packages: %w", err)
		}

		if len(packages) == 0 {
			if orphansOnly {
				fmt.Println("No orphan packages")
			} else {
				fmt.Println("No packages installed")
			}
			return nil
		}

		if orphansOnly {
			fmt.Println("Orphan packages:")
		} else {
			fmt.Println("Installed packages:")
		}
		for _, pkg := range packages {
			fmt.Printf("  %s\n", pkg)
		}
		return nil
	},
}

func init() {
	ListCmd.Flags().StringP("root", "r", "/", "Root directory for installation")
	ListCmd.Flags().BoolP("orphans", "o", false, "List only orphan packages")
}

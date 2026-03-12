package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"zsvo/pkg/installer"
)

var RemoveCmd = &cobra.Command{
	Use:   "remove <package> [package...]",
	Short: "Remove package(s)",
	Long:  `Remove one or more installed packages`,
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		// Create installer
		rootDir, _ := cmd.Flags().GetString("root")
		if rootDir == "" {
			rootDir = "/"
		}

		i := installer.NewInstaller(rootDir)
		cascade, _ := cmd.Flags().GetBool("cascade")
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		options := installer.RemoveOptions{
			Cascade: cascade,
		}

		if dryRun {
			plan, err := i.PlanRemove(args, options)
			if err != nil {
				return fmt.Errorf("failed to calculate removal plan: %w", err)
			}

			if len(plan) == 0 {
				fmt.Println("Nothing to remove")
				return nil
			}

			fmt.Printf("Planned removal (%d package(s)):\n", len(plan))
			for _, pkgName := range plan {
				fmt.Printf("  %s\n", pkgName)
			}
			return nil
		}

		removed, err := i.RemoveMany(args, options)
		if err != nil {
			return fmt.Errorf("failed to remove packages: %w", err)
		}

		fmt.Printf("Removed %d package(s):\n", len(removed))
		for _, pkgName := range removed {
			fmt.Printf("  %s\n", pkgName)
		}
		return nil
	},
}

func init() {
	RemoveCmd.Flags().StringP("root", "r", "/", "Root directory for installation")
	RemoveCmd.Flags().BoolP("cascade", "c", false, "Remove dependent packages that become broken")
	RemoveCmd.Flags().BoolP("dry-run", "n", false, "Show packages that would be removed without applying changes")
}

package cmd

import (
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"
	"zsvo/pkg/builder"
	"zsvo/pkg/recipe"
)

var BuildCmd = &cobra.Command{
	Use:   "build <recipe>",
	Short: "Build a package from recipe",
	Long:  `Build a package from a recipe file`,
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		recipePath := args[0]

		// Parse recipe
		rcp, err := recipe.ParseRecipe(recipePath)
		if err != nil {
			return fmt.Errorf("failed to parse recipe: %w", err)
		}

		// Create builder
		workDir, _ := cmd.Flags().GetString("work-dir")
		if workDir == "" {
			workDir = "/tmp/pkg-work"
		}

		b := builder.NewBuilder(workDir)

		// Build package
		fmt.Printf("Building package %s...\n", rcp.GetPackageName())
		if err := b.Build(rcp); err != nil {
			return fmt.Errorf("failed to build package: %w", err)
		}

		fmt.Printf("Package %s built successfully\n", rcp.GetPackageName())
		fmt.Printf("Package file: %s\n", filepath.Join(rcp.GetPackageDir(workDir), rcp.GetPackageFileName()))
		return nil
	},
}

func init() {
	BuildCmd.Flags().StringP("work-dir", "w", "", "Working directory for build")
}

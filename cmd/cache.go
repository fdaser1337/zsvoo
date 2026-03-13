package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"zsvo/pkg/builder"
)

var CacheCmd = &cobra.Command{
	Use:   "cache",
	Short: "Manage build cache",
	Long:  `Clean or show information about build cache`,
}

var CleanCacheCmd = &cobra.Command{
	Use:   "clean",
	Short: "Clean build cache",
	Long:  `Remove all cached packages, sources, and build artifacts`,
	RunE: func(cmd *cobra.Command, args []string) error {
		workDir, _ := cmd.Flags().GetString("work-dir")
		if workDir == "" {
			workDir = "/tmp/pkg-work"
		}

		b := builder.NewBuilder(workDir)
		
		// Show cache size before cleaning
		if size, err := b.GetCacheSize(); err == nil {
			fmt.Printf("Cache size: %s\n", formatBytes(size))
		}

		if err := b.CleanCache(); err != nil {
			return fmt.Errorf("failed to clean cache: %w", err)
		}

		fmt.Printf("Cache cleaned successfully\n")
		return nil
	},
}

var InfoCacheCmd = &cobra.Command{
	Use:   "info",
	Short: "Show cache information",
	Long:  `Display detailed information about build cache usage`,
	RunE: func(cmd *cobra.Command, args []string) error {
		workDir, _ := cmd.Flags().GetString("work-dir")
		if workDir == "" {
			workDir = "/tmp/pkg-work"
		}

		b := builder.NewBuilder(workDir)
		
		fmt.Printf("=== Build Cache Information ===\n\n")
		fmt.Printf("Work directory: %s\n", workDir)
		
		// Calculate total size
		totalSize, err := b.GetCacheSize()
		if err != nil {
			fmt.Printf("Error calculating cache size: %v\n", err)
		} else {
			fmt.Printf("Total cache size: %s\n", formatBytes(totalSize))
		}

		// Show individual directories
		dirs := map[string]string{
			"Download cache": filepath.Join(workDir, "cache"),
			"Built packages": filepath.Join(workDir, "packages"),
			"Source files":   filepath.Join(workDir, "sources"),
			"Staging files":  filepath.Join(workDir, "staging"),
		}

		fmt.Printf("\nCache breakdown:\n")
		for name, dir := range dirs {
			if size, err := getDirSize(dir); err == nil && size > 0 {
				fmt.Printf("  %s: %s\n", name, formatBytes(size))
			} else {
				fmt.Printf("  %s: empty or missing\n", name)
			}
		}

		// Count cached packages
		pkgDir := filepath.Join(workDir, "packages")
		if entries, err := os.ReadDir(pkgDir); err == nil {
			pkgCount := 0
			for _, entry := range entries {
				if entry.IsDir() {
					pkgCount++
				}
			}
			fmt.Printf("\nCached packages: %d\n", pkgCount)
		}

		return nil
	},
}

func init() {
	CacheCmd.AddCommand(CleanCacheCmd)
	CacheCmd.AddCommand(InfoCacheCmd)

	// Add work-dir flag to subcommands
	CleanCacheCmd.Flags().StringP("work-dir", "w", "/tmp/pkg-work", "Working directory for cache")
	InfoCacheCmd.Flags().StringP("work-dir", "w", "/tmp/pkg-work", "Working directory for cache")
}

func formatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

// getDirSize calculates total size of directory recursively
func getDirSize(path string) (int64, error) {
	var size int64
	err := filepath.Walk(path, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			size += info.Size()
		}
		return nil
	})
	return size, err
}

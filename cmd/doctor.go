package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/spf13/cobra"
)

var DoctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Check system for potential issues",
	Long:  `Diagnose common problems with build environment and system setup`,
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Printf("=== ZSVO System Diagnosis ===\n\n")

		// Check basic system info
		checkSystemInfo()
		
		// Check required tools
		checkBuildTools()
		
		// Check directories and permissions
		checkDirectories()
		
		// Check network connectivity
		checkNetwork()
		
		// Check package database
		checkPackageDB()

		fmt.Printf("\n=== Diagnosis Complete ===\n")
		fmt.Printf("If you see any ❌ items above, fix them before using zsvo.\n")
		return nil
	},
}

func checkSystemInfo() {
	fmt.Printf("📋 System Information:\n")
	
	fmt.Printf("  OS: %s\n", runtime.GOOS)
	fmt.Printf("  Arch: %s\n", runtime.GOARCH)
	fmt.Printf("  Go version: %s\n", runtime.Version())
	fmt.Printf("  CPU cores: %d\n", runtime.NumCPU())
	
	// Check user
	if user, err := os.UserHomeDir(); err == nil {
		fmt.Printf("  Home directory: %s\n", user)
	}
	
	fmt.Println()
}

func checkBuildTools() {
	fmt.Printf("🔧 Build Tools:\n")
	
	tools := []struct {
		name string
		cmd  string
		args []string
	}{
		{"make", "make", []string{"--version"}},
		{"gcc", "gcc", []string{"--version"}},
		{"pkg-config", "pkg-config", []string{"--version"}},
		{"cmake", "cmake", []string{"--version"}},
		{"meson", "meson", []string{"--version"}},
		{"python3", "python3", []string{"--version"}},
		{"tar", "tar", []string{"--version"}},
		{"xz", "xz", []string{"--version"}},
	}
	
	for _, tool := range tools {
		if checkCommand(tool.cmd, tool.args) {
			fmt.Printf("  ✅ %s\n", tool.name)
		} else {
			fmt.Printf("  ❌ %s (missing)\n", tool.name)
		}
	}
	
	fmt.Println()
}

func checkDirectories() {
	fmt.Printf("📁 Directories & Permissions:\n")
	
	dirs := []string{
		"/tmp",
		"/var/tmp",
		"/usr/local",
		"/usr/bin",
	}
	
	// Check work directory
	workDir := "/tmp/pkg-work"
	if err := os.MkdirAll(workDir, 0755); err != nil {
		fmt.Printf("  ❌ Work directory (%s): %v\n", workDir, err)
	} else {
		fmt.Printf("  ✅ Work directory (%s): writable\n", workDir)
		os.RemoveAll(workDir) // cleanup
	}
	
	// Check cache directory
	cacheDir := filepath.Join(os.TempDir(), "zsvo-cache")
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		fmt.Printf("  ❌ Cache directory (%s): %v\n", cacheDir, err)
	} else {
		fmt.Printf("  ✅ Cache directory (%s): writable\n", cacheDir)
		os.RemoveAll(cacheDir) // cleanup
	}
	
	for _, dir := range dirs {
		if info, err := os.Stat(dir); err != nil {
			fmt.Printf("  ❌ %s: %v\n", dir, err)
		} else {
			if info.IsDir() {
				fmt.Printf("  ✅ %s: exists\n", dir)
			} else {
				fmt.Printf("  ⚠️  %s: not a directory\n", dir)
			}
		}
	}
	
	fmt.Println()
}

func checkNetwork() {
	fmt.Printf("🌐 Network Connectivity:\n")
	
	// Test Debian mirror
	if checkHTTP("https://deb.debian.org") {
		fmt.Printf("  ✅ Debian mirror: reachable\n")
	} else {
		fmt.Printf("  ❌ Debian mirror: not reachable\n")
	}
	
	// Test FTP mirror
	if checkHTTP("https://ftp.gnu.org") {
		fmt.Printf("  ✅ GNU FTP: reachable\n")
	} else {
		fmt.Printf("  ❌ GNU FTP: not reachable\n")
	}
	
	fmt.Println()
}

func checkPackageDB() {
	fmt.Printf("📦 Package Database:\n")
	
	rootDir := "/"
	pkgDB := filepath.Join(rootDir, "var", "lib", "pkgdb")
	
	if info, err := os.Stat(pkgDB); err != nil {
		if os.IsNotExist(err) {
			fmt.Printf("  ℹ️  Package database (%s): not created yet\n", pkgDB)
		} else {
			fmt.Printf("  ❌ Package database (%s): %v\n", pkgDB, err)
		}
	} else {
		if info.IsDir() {
			fmt.Printf("  ✅ Package database (%s): exists\n", pkgDB)
			
			// Count packages
			if entries, err := os.ReadDir(pkgDB); err == nil {
				pkgCount := 0
				for _, entry := range entries {
					if entry.IsDir() {
						pkgCount++
					}
				}
				fmt.Printf("  📊 Installed packages: %d\n", pkgCount)
			}
		} else {
			fmt.Printf("  ❌ Package database (%s): not a directory\n", pkgDB)
		}
	}
	
	fmt.Println()
}

func checkCommand(name string, args []string) bool {
	cmd := exec.Command(name, args...)
	cmd.Stdout = nil
	cmd.Stderr = nil
	return cmd.Run() == nil
}

func checkHTTP(url string) bool {
	cmd := exec.Command("curl", "-s", "--connect-timeout", "5", url)
	cmd.Stdout = nil
	cmd.Stderr = nil
	return cmd.Run() == nil
}

func init() {
	// Will be added to root command in main.go
}

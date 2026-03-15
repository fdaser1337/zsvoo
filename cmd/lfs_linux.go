//go:build linux
// +build linux

package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"zsvo/pkg/builder"
	"zsvo/pkg/cache"
	"zsvo/pkg/installer"
	"zsvo/pkg/sandbox"
)

// LFSBuildSession optimized for Linux From Scratch
type LFSBuildSession struct {
	*autoBuildSession
	store   *cache.Store
	sandbox *sandbox.Sandbox
	thermal *builder.ThermalMonitor
}

// NewLFSBuildSession creates optimized LFS build session
func NewLFSBuildSession(workDir string, autoBuildDeps bool, jobs int, cooldownSeconds int) *LFSBuildSession {
	// Create content-addressable store
	storePath := filepath.Join(workDir, ".store")
	store, _ := cache.NewStore(storePath)

	// Use thermal monitoring
	targetTemp := 75.0
	if runtime.GOOS == "linux" {
		targetTemp = 80.0 // Linux handles heat better
	}

	thermal := builder.NewThermalMonitor(targetTemp, jobs)

	// Create base session
	base := newAutoBuildSession(workDir, autoBuildDeps, jobs, 0)

	return &LFSBuildSession{
		autoBuildSession: base,
		store:            store,
		thermal:          thermal,
	}
}

// BuildOptimized builds with LFS optimizations
func (s *LFSBuildSession) BuildOptimized(pkgName string) (string, error) {
	// Check store first (content-addressable)
	if s.store != nil {
		// Compute expected hash from package definition
		// For now, use name-based lookup
		cachePaths := []string{
			filepath.Join(s.workDir, ".store", pkgName[:2], pkgName),
		}

		for _, path := range cachePaths {
			if _, err := os.Stat(path); err == nil {
				fmt.Printf("📦 %s found in content-addressable store\n", pkgName)
				return path, nil
			}
		}
	}

	// Update thermal limits before build
	if err := s.thermal.Update(); err == nil {
		s.jobs = s.thermal.GetJobs()
		if s.thermal.ShouldCooldown() {
			fmt.Printf("🌡️  Thermal limit reached, using %d jobs with cooldown\n", s.jobs)
		}
	}

	// Build with fallback
	return s.buildPackageWithFallback(pkgName, false, false, []string{})
}

// InstallToFakeroot installs built package to fakeroot
func (s *LFSBuildSession) InstallToFakeroot(pkgPath, fakeroot string) error {
	// Use our installer
	i := s.toolInstaller
	i = installer.NewInstaller(fakeroot)

	return i.Install(pkgPath)
}

// VerifyFakeroot verifies fakeroot installation
func VerifyFakeroot(fakeroot string) error {
	// Check essential directories exist
	essentialDirs := []string{
		filepath.Join(fakeroot, "usr", "bin"),
		filepath.Join(fakeroot, "usr", "lib"),
		filepath.Join(fakeroot, "var", "lib", "pkgdb"),
	}

	for _, dir := range essentialDirs {
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			// Create if missing
			if err := os.MkdirAll(dir, 0755); err != nil {
				return fmt.Errorf("failed to create %s: %w", dir, err)
			}
		}
	}

	return nil
}

// QuickInstall performs optimized install for LFS
func QuickInstall(pkgName, workDir, fakeroot string) error {
	// Verify fakeroot
	if err := VerifyFakeroot(fakeroot); err != nil {
		return fmt.Errorf("fakeroot verification failed: %w", err)
	}

	// Create optimized session
	session := NewLFSBuildSession(workDir, true, 2, 0)

	// Build
	pkgPath, err := session.BuildOptimized(pkgName)
	if err != nil {
		return fmt.Errorf("build failed: %w", err)
	}

	// Install to fakeroot
	if err := session.InstallToFakeroot(pkgPath, fakeroot); err != nil {
		return fmt.Errorf("install failed: %w", err)
	}

	fmt.Printf("✅ %s installed to %s\n", pkgName, fakeroot)
	return nil
}

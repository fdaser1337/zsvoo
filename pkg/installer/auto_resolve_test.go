package installer

import (
	"os"
	"path/filepath"
	"testing"

	"zsvo/pkg/deps"
	"zsvo/pkg/types"
)

func TestInstaller_BuildPackageIndex(t *testing.T) {
	t.Parallel()

	// Create temporary directory structure
	tempDir, err := os.MkdirTemp("", "zsvo-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create package directory
	pkgDir := filepath.Join(tempDir, "packages")
	if err := os.MkdirAll(pkgDir, 0755); err != nil {
		t.Fatalf("Failed to create package dir: %v", err)
	}

	// Create root directory
	rootDir := filepath.Join(tempDir, "root")
	if err := os.MkdirAll(rootDir, 0755); err != nil {
		t.Fatalf("Failed to create root dir: %v", err)
	}

	installer := NewInstaller(rootDir)
	
	// Test with empty directory
	index, err := installer.buildPackageIndex([]string{pkgDir})
	if err != nil {
		t.Fatalf("Failed to build package index: %v", err)
	}

	if len(index) != 0 {
		t.Errorf("Expected 0 packages in index, got %d", len(index))
	}

	// Test with non-existent directory
	index, err = installer.buildPackageIndex([]string{"/non/existent/path"})
	if err != nil {
		t.Fatalf("Failed to build package index with non-existent path: %v", err)
	}

	if len(index) != 0 {
		t.Errorf("Expected 0 packages in index for non-existent path, got %d", len(index))
	}
}

func TestInstaller_FindMissingDependencies(t *testing.T) {
	t.Parallel()

	// Create temporary directory structure
	tempDir, err := os.MkdirTemp("", "zsvo-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create root directory
	rootDir := filepath.Join(tempDir, "root")
	if err := os.MkdirAll(rootDir, 0755); err != nil {
		t.Fatalf("Failed to create root dir: %v", err)
	}

	installer := NewInstaller(rootDir)

	// Test with package that has no dependencies
	pkgInfo := &types.PkgInfo{
		Name:         "testpkg",
		Version:      "1.0",
		Dependencies: []string{},
	}

	installed := make(map[string]*deps.PackageInfo)
	candidates := make(map[string]installCandidate)

	missing, err := installer.findMissingDependencies(pkgInfo, installed, candidates)
	if err != nil {
		t.Fatalf("Failed to find missing dependencies: %v", err)
	}

	if len(missing) != 0 {
		t.Errorf("Expected 0 missing dependencies, got %d", len(missing))
	}

	// Test with package that has missing dependency
	pkgInfoWithDeps := &types.PkgInfo{
		Name:         "testpkg",
		Version:      "1.0",
		Dependencies: []string{"missingdep"},
	}

	missing, err = installer.findMissingDependencies(pkgInfoWithDeps, installed, candidates)
	if err != nil {
		t.Fatalf("Failed to find missing dependencies: %v", err)
	}

	if len(missing) != 1 {
		t.Errorf("Expected 1 missing dependency, got %d", len(missing))
	}

	if missing[0] != "missingdep" {
		t.Errorf("Expected missing dependency 'missingdep', got %s", missing[0])
	}
}

func TestInstaller_ResolveDependenciesFromSearch(t *testing.T) {
	t.Parallel()

	// Create temporary directory structure
	tempDir, err := os.MkdirTemp("", "zsvo-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create root directory
	rootDir := filepath.Join(tempDir, "root")
	if err := os.MkdirAll(rootDir, 0755); err != nil {
		t.Fatalf("Failed to create root dir: %v", err)
	}

	installer := NewInstaller(rootDir)

	// Test with no available packages
	pkgInfo := &types.PkgInfo{
		Name:         "testpkg",
		Version:      "1.0",
		Dependencies: []string{},
	}

	candidate := installCandidate{
		path: "/fake/path/testpkg.pkg.tar.zst",
		info: pkgInfo,
	}

	candidates := []installCandidate{candidate}
	searchPaths := []string{"/non/existent/path"}

	resolved, err := installer.resolveDependenciesFromSearch(candidates, searchPaths)
	if err != nil {
		t.Fatalf("Failed to resolve dependencies: %v", err)
	}

	// Should resolve just the single package since it has no deps
	if len(resolved) != 1 {
		t.Errorf("Expected 1 package resolved, got %d", len(resolved))
	}

	if resolved[0].info.Name != "testpkg" {
		t.Errorf("Expected resolved package 'testpkg', got %s", resolved[0].info.Name)
	}
}

func TestInstaller_InstallWithAutoResolve_MissingDeps(t *testing.T) {
	t.Parallel()

	// Create temporary directory structure
	tempDir, err := os.MkdirTemp("", "zsvo-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create root directory
	rootDir := filepath.Join(tempDir, "root")
	if err := os.MkdirAll(rootDir, 0755); err != nil {
		t.Fatalf("Failed to create root dir: %v", err)
	}

	installer := NewInstaller(rootDir)

	// Test with non-existent package file
	nonExistentFile := filepath.Join(tempDir, "nonexistent.pkg.tar.zst")
	
	err = installer.InstallWithAutoResolve([]string{nonExistentFile}, []string{})
	
	if err == nil {
		t.Errorf("Expected error for non-existent package file")
	}
}

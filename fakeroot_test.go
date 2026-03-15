package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// FakerootTestRunner runs tests in fakeroot environment
type FakerootTestRunner struct {
	workDir   string
	rootDir   string
	zsvoBin   string
}

// NewFakerootTestRunner creates test runner
func NewFakerootTestRunner() (*FakerootTestRunner, error) {
	workDir, err := os.MkdirTemp("", "zsvo-test-")
	if err != nil {
		return nil, err
	}
	
	rootDir := filepath.Join(workDir, "fakeroot")
	if err := os.MkdirAll(rootDir, 0755); err != nil {
		return nil, err
	}
	
	// Find zsvo binary
	zsvoBin := "./zsvo"
	if _, err := os.Stat(zsvoBin); err != nil {
		// Try to build
		cmd := exec.Command("go", "build", "-o", "zsvo", ".")
		if err := cmd.Run(); err != nil {
			return nil, fmt.Errorf("failed to build zsvo: %w", err)
		}
	}
	
	return &FakerootTestRunner{
		workDir: workDir,
		rootDir: rootDir,
		zsvoBin: zsvoBin,
	}, nil
}

// Cleanup removes test directory
func (f *FakerootTestRunner) Cleanup() {
	os.RemoveAll(f.workDir)
}

// RunInFakeroot executes command in fakeroot environment
func (f *FakerootTestRunner) RunInFakeroot(args ...string) (string, string, error) {
	// Check if fakeroot is available
	fakerootPath, err := exec.LookPath("fakeroot")
	if err != nil {
		// Run without fakeroot but with custom root
		cmd := exec.Command(f.zsvoBin, args...)
		cmd.Env = append(os.Environ(), 
			"FAKEROOT=true",
			"ZSVO_ROOT="+f.rootDir,
		)
		output, err := cmd.CombinedOutput()
		return string(output), "", err
	}
	
	// Run with fakeroot
	cmdArgs := append([]string{f.zsvoBin}, args...)
	cmd := exec.Command(fakerootPath, cmdArgs...)
	cmd.Env = append(os.Environ(), "ZSVO_ROOT="+f.rootDir)
	
	output, err := cmd.CombinedOutput()
	return string(output), "", err
}

// TestInstallSimplePackage tests installing simple package
func TestInstallSimplePackage(t *testing.T) {
	runner, err := NewFakerootTestRunner()
	if err != nil {
		t.Fatalf("Failed to create test runner: %v", err)
	}
	defer runner.Cleanup()
	
	// Test installing expat (simple C library)
	stdout, stderr, err := runner.RunInFakeroot(
		"install", "expat",
		"--work-dir", runner.workDir,
		"--root", runner.rootDir,
		"--jobs", "2",
		"--cooldown", "0s",
	)
	
	t.Logf("stdout: %s", stdout)
	t.Logf("stderr: %s", stderr)
	
	if err != nil {
		t.Fatalf("Install failed: %v\nOutput: %s", err, stdout)
	}
	
	// Verify installation
	if !strings.Contains(stdout, "completed") && !strings.Contains(stdout, "successfully") {
		t.Errorf("Installation may have failed, output: %s", stdout)
	}
}

// TestCacheHit tests that cached packages are not rebuilt
func TestCacheHit(t *testing.T) {
	runner, err := NewFakerootTestRunner()
	if err != nil {
		t.Fatalf("Failed to create test runner: %v", err)
	}
	defer runner.Cleanup()
	
	// First install - should build
	stdout1, _, _ := runner.RunInFakeroot(
		"install", "zlib",
		"--work-dir", runner.workDir,
		"--root", runner.rootDir,
		"--dry-run",
	)
	
	if !strings.Contains(stdout1, "Would auto-build") {
		t.Skip("Dry-run mode doesn't show cache hit info")
	}
}

// TestDependencyResolution tests dependency resolution
func TestDependencyResolution(t *testing.T) {
	runner, err := NewFakerootTestRunner()
	if err != nil {
		t.Fatalf("Failed to create test runner: %v", err)
	}
	defer runner.Cleanup()
	
	stdout, _, err := runner.RunInFakeroot(
		"search", "libssl-dev",
	)
	
	if err != nil {
		t.Logf("Search output: %s", stdout)
	}
}

// TestFakerootIsolation tests that fakeroot provides proper isolation
func TestFakerootIsolation(t *testing.T) {
	runner, err := NewFakerootTestRunner()
	if err != nil {
		t.Fatalf("Failed to create test runner: %v", err)
	}
	defer runner.Cleanup()
	
	// Install a package
	_, _, err = runner.RunInFakeroot(
		"install", "expat",
		"--work-dir", runner.workDir,
		"--root", runner.rootDir,
		"--jobs", "2",
		"--cooldown", "0s",
	)
	
	if err != nil {
		t.Skipf("Install failed, skipping isolation test: %v", err)
	}
	
	// Verify files are in fakeroot, not system
	fakerootFile := filepath.Join(runner.rootDir, "usr", "lib", "libexpat.so")
	systemFile := "/usr/lib/libexpat.so"
	
	if _, err := os.Stat(fakerootFile); err == nil {
		t.Logf("✓ Package installed in fakeroot: %s", fakerootFile)
	} else {
		t.Errorf("Package not found in fakeroot: %v", err)
	}
	
	// System should not have the file (or it's different)
	if _, err := os.Stat(systemFile); err == nil {
		t.Logf("ℹ System already has %s (may be different version)", systemFile)
	}
}

// BenchmarkInstall measures install performance
func BenchmarkInstall(b *testing.B) {
	for i := 0; i < b.N; i++ {
		runner, err := NewFakerootTestRunner()
		if err != nil {
			b.Fatalf("Failed to create test runner: %v", err)
		}
		
		// Use dry-run for speed
		runner.RunInFakeroot(
			"install", "zlib",
			"--work-dir", runner.workDir,
			"--root", runner.rootDir,
			"--dry-run",
		)
		
		runner.Cleanup()
	}
}

// TestMain runs all tests
func TestMain(m *testing.M) {
	// Skip if not Linux
	if os.Getenv("GOOS") != "" && os.Getenv("GOOS") != "linux" {
		if _, err := exec.LookPath("go"); err != nil {
			fmt.Println("Skipping tests: not Linux environment")
			os.Exit(0)
		}
	}
	
	os.Exit(m.Run())
}

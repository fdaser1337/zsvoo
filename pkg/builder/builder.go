package builder

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"zsvo/pkg/fetcher"
	"zsvo/pkg/packager"
	"zsvo/pkg/recipe"
)

// Builder handles building packages from recipes
type Builder struct {
	workDir string
}

// NewBuilder creates a new builder
func NewBuilder(workDir string) *Builder {
	return &Builder{
		workDir: workDir,
	}
}

// Build builds a package from a recipe
func (b *Builder) Build(recipe *recipe.Recipe) error {
	if recipe == nil {
		return fmt.Errorf("recipe cannot be nil")
	}

	// Create working directories
	sourceDir := recipe.GetSourceDir(b.workDir)
	stagingDir := recipe.GetStagingDir(b.workDir)
	packageDir := recipe.GetPackageDir(b.workDir)

	if err := b.prepareDirectories(sourceDir, stagingDir, packageDir); err != nil {
		return fmt.Errorf("failed to prepare directories: %w", err)
	}

	// Download and extract source
	if err := b.downloadAndExtract(recipe); err != nil {
		return fmt.Errorf("failed to download and extract source: %w", err)
	}

	// Apply patches
	if err := b.applyPatches(recipe, sourceDir); err != nil {
		return fmt.Errorf("failed to apply patches: %w", err)
	}

	// Build package
	if err := b.executeBuild(recipe, sourceDir, stagingDir); err != nil {
		return fmt.Errorf("failed to build package: %w", err)
	}

	// Package files
	if err := b.packageFiles(recipe, sourceDir, stagingDir); err != nil {
		return fmt.Errorf("failed to package files: %w", err)
	}

	// Create package archive
	p := packager.NewPackager(b.workDir)
	if err := p.Package(recipe); err != nil {
		return fmt.Errorf("failed to create package archive: %w", err)
	}

	return nil
}

// prepareDirectories creates necessary directories
func (b *Builder) prepareDirectories(sourceDir, stagingDir, packageDir string) error {
	dirs := []string{
		sourceDir,
		stagingDir,
		packageDir,
	}

	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}

	return nil
}

// downloadAndExtract downloads and extracts source
func (b *Builder) downloadAndExtract(recipe *recipe.Recipe) error {
	sourceDir := recipe.GetSourceDir(b.workDir)
	if err := os.RemoveAll(sourceDir); err != nil {
		return fmt.Errorf("failed to clean source directory: %w", err)
	}
	if err := os.MkdirAll(sourceDir, 0755); err != nil {
		return fmt.Errorf("failed to recreate source directory: %w", err)
	}

	f := fetcher.NewFetcher(filepath.Join(b.workDir, "cache"))

	sourceURL := recipe.Source.URL
	if recipe.Source.DebianDSC != "" {
		sourceURL = recipe.Source.DebianDSC
	}

	return f.DownloadAndExtract(
		sourceURL,
		recipe.Source.Sha256,
		sourceDir,
	)
}

// applyPatches applies patches to source
func (b *Builder) applyPatches(recipe *recipe.Recipe, sourceDir string) error {
	if len(recipe.Source.Patches) == 0 {
		return nil
	}

	patches := make([]string, 0, len(recipe.Source.Patches))
	for _, patchPath := range recipe.Source.Patches {
		if patchPath == "" {
			return fmt.Errorf("patch path cannot be empty")
		}

		if !filepath.IsAbs(patchPath) && recipe.Dir != "" {
			patchPath = filepath.Join(recipe.Dir, patchPath)
		}
		patches = append(patches, patchPath)
	}

	f := fetcher.NewFetcher(filepath.Join(b.workDir, "cache"))
	return f.ApplyPatches(sourceDir, patches)
}

// executeBuild executes build commands
func (b *Builder) executeBuild(recipe *recipe.Recipe, sourceDir, stagingDir string) error {
	// Find the source directory (usually the first subdirectory)
	srcPath, err := b.findSourceDirectory(sourceDir)
	if err != nil {
		return fmt.Errorf("failed to find source directory: %w", err)
	}

	// Set up environment
	env := b.buildEnvironment(stagingDir)

	// Execute build commands
	for i, cmd := range recipe.Build {
		// Substitute variables in command
		cmd = b.substituteVariables(cmd, sourceDir, stagingDir)

		if err := b.executeCommand(srcPath, cmd, env); err != nil {
			return fmt.Errorf("build command %d failed: %w", i+1, err)
		}
	}

	return nil
}

// findSourceDirectory finds the actual source directory
func (b *Builder) findSourceDirectory(sourceDir string) (string, error) {
	// Validate input path
	if sourceDir == "" {
		return "", fmt.Errorf("source directory cannot be empty")
	}

	// Clean and normalize path
	sourceDir = filepath.Clean(sourceDir)

	entries, err := os.ReadDir(sourceDir)
	if err != nil {
		return "", fmt.Errorf("failed to read source directory %s: %w", sourceDir, err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			return filepath.Join(sourceDir, entry.Name()), nil
		}
	}

	return sourceDir, nil
}

// buildEnvironment builds the environment for build commands
func (b *Builder) buildEnvironment(stagingDir string) []string {
	env := os.Environ()

	// Add standard build environment variables
	env = append(env, fmt.Sprintf("DESTDIR=%s", stagingDir))
	env = append(env, fmt.Sprintf("PREFIX=/usr"))
	env = append(env, fmt.Sprintf("PKGDIR=%s", stagingDir))

	// Add parallel build variable
	env = append(env, fmt.Sprintf("MAKEFLAGS=-j%d", runtime.NumCPU()))

	return env
}

// executeCommand executes a single command
func (b *Builder) executeCommand(workDir, command string, env []string) error {
	if strings.TrimSpace(command) == "" {
		return nil
	}

	cmd := exec.Command("sh", "-c", command)
	cmd.Dir = workDir
	cmd.Env = env
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}

// executeCommandWithOutput executes a command and returns output
func (b *Builder) executeCommandWithOutput(workDir, command string, env []string) (string, error) {
	if strings.TrimSpace(command) == "" {
		return "", nil
	}

	cmd := exec.Command("sh", "-c", command)
	cmd.Dir = workDir
	cmd.Env = env

	output, err := cmd.CombinedOutput()
	return string(output), err
}

// Clean removes build artifacts
func (b *Builder) Clean(recipe *recipe.Recipe) error {
	dirs := []string{
		recipe.GetSourceDir(b.workDir),
		recipe.GetStagingDir(b.workDir),
	}

	for _, dir := range dirs {
		if err := os.RemoveAll(dir); err != nil {
			return fmt.Errorf("failed to clean directory %s: %w", dir, err)
		}
	}

	return nil
}

// GetBuildInfo returns information about the build
func (b *Builder) GetBuildInfo(recipe *recipe.Recipe) (*BuildInfo, error) {
	sourceDir := recipe.GetSourceDir(b.workDir)
	stagingDir := recipe.GetStagingDir(b.workDir)

	info := &BuildInfo{
		Recipe:        recipe,
		SourceDir:     sourceDir,
		StagingDir:    stagingDir,
		SourceExists:  false,
		StagingExists: false,
	}

	// Check if source directory exists
	if _, err := os.Stat(sourceDir); err == nil {
		info.SourceExists = true
	}

	// Check if staging directory exists
	if _, err := os.Stat(stagingDir); err == nil {
		info.StagingExists = true
	}

	return info, nil
}

// BuildInfo contains information about a build
type BuildInfo struct {
	Recipe        *recipe.Recipe
	SourceDir     string
	StagingDir    string
	SourceExists  bool
	StagingExists bool
}

// ListFilesInStaging lists all files in the staging directory
func (b *Builder) ListFilesInStaging(recipe *recipe.Recipe) ([]string, error) {
	stagingDir := recipe.GetStagingDir(b.workDir)
	var files []string

	err := filepath.Walk(stagingDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			// Convert to relative path
			relPath, err := filepath.Rel(stagingDir, path)
			if err != nil {
				return err
			}
			files = append(files, relPath)
		}
		return nil
	})

	return files, err
}

// GetWorkDir returns the working directory
func (b *Builder) GetWorkDir() string {
	return b.workDir
}

// SetWorkDir sets the working directory
func (b *Builder) SetWorkDir(workDir string) {
	b.workDir = workDir
}

// substituteVariables substitutes variables in command strings
func (b *Builder) substituteVariables(cmdStr, sourceDir, stagingDir string) string {
	// Get number of CPU cores
	jobs := runtime.NumCPU()

	// Substitute variables
	cmdStr = strings.ReplaceAll(cmdStr, "${jobs}", fmt.Sprintf("%d", jobs))
	cmdStr = strings.ReplaceAll(cmdStr, "${pkgdir}", stagingDir)
	cmdStr = strings.ReplaceAll(cmdStr, "${srcdir}", sourceDir)
	cmdStr = strings.ReplaceAll(cmdStr, "{{jobs}}", fmt.Sprintf("%d", jobs))
	cmdStr = strings.ReplaceAll(cmdStr, "{{pkgdir}}", stagingDir)
	cmdStr = strings.ReplaceAll(cmdStr, "{{srcdir}}", sourceDir)

	return cmdStr
}

// packageFiles runs the package commands
func (b *Builder) packageFiles(recipe *recipe.Recipe, sourceDir, stagingDir string) error {
	// Find the source directory (usually the first subdirectory)
	srcPath, err := b.findSourceDirectory(sourceDir)
	if err != nil {
		return fmt.Errorf("failed to find source directory: %w", err)
	}

	// Set up environment
	env := b.buildEnvironment(stagingDir)

	// Execute package commands
	for i, cmd := range recipe.Install {
		// Substitute variables in command
		cmd = b.substituteVariables(cmd, sourceDir, stagingDir)

		if err := b.executeCommand(srcPath, cmd, env); err != nil {
			return fmt.Errorf("package command %d failed: %w", i+1, err)
		}
	}

	return nil
}

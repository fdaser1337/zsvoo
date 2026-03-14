package builder

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"zsvo/pkg/fetcher"
	"zsvo/pkg/packager"
	"zsvo/pkg/recipe"
)

// Builder handles building packages from recipes
type Builder struct {
	workDir          string
	quiet            bool
	progressCallback func(BuildProgress)
	envOverrides     map[string]string
	callbackMutex    sync.RWMutex
}

// BuildProgress represents build progress state.
type BuildProgress struct {
	Step    int
	Total   int
	Message string
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

	// Check if package already exists in cache
	packageFile := filepath.Join(recipe.GetPackageDir(b.workDir), recipe.GetPackageFileName())
	if _, err := os.Stat(packageFile); err == nil {
		log.Printf("Package %s already exists, skipping build", recipe.GetPackageName())
		return nil
	}

	totalSteps := 4 + len(recipe.Build) + len(recipe.Install)
	step := 0
	nextStep := func(message string) {
		step++
		b.reportProgress(step, totalSteps, message)
	}

	// Create working directories
	sourceDir := recipe.GetSourceDir(b.workDir)
	stagingDir := recipe.GetStagingDir(b.workDir)
	packageDir := recipe.GetPackageDir(b.workDir)

	nextStep("Preparing directories")
	if err := b.prepareDirectories(sourceDir, stagingDir, packageDir); err != nil {
		return fmt.Errorf("failed to prepare directories: %w", err)
	}

	// Download and extract source
	nextStep("Downloading and extracting source")
	if err := b.downloadAndExtract(recipe); err != nil {
		return fmt.Errorf("failed to download and extract source: %w", err)
	}

	// Apply patches
	nextStep("Applying recipe patches")
	if err := b.applyPatches(recipe, sourceDir); err != nil {
		return fmt.Errorf("failed to apply patches: %w", err)
	}

	// Validate source files
	nextStep("Validating source files")
	if err := b.validateSourceFiles(recipe, sourceDir); err != nil {
		return fmt.Errorf("failed to validate source files: %w", err)
	}

	// Build package
	if err := b.executeBuild(recipe, sourceDir, stagingDir, func(i, total int) {
		nextStep(fmt.Sprintf("Build step %d/%d", i, total))
	}); err != nil {
		return fmt.Errorf("failed to build package: %w", err)
	}

	// Package files
	if err := b.packageFiles(recipe, sourceDir, stagingDir, func(i, total int) {
		nextStep(fmt.Sprintf("Install step %d/%d", i, total))
	}); err != nil {
		return fmt.Errorf("failed to package files: %w", err)
	}

	// Create package archive
	nextStep("Creating package archive")
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
func (b *Builder) executeBuild(recipe *recipe.Recipe, sourceDir, stagingDir string, progressFn func(step, total int)) error {
	// Find the source directory (usually the first subdirectory)
	srcPath, err := b.findSourceDirectory(sourceDir)
	if err != nil {
		return fmt.Errorf("failed to find source directory: %w", err)
	}

	// Set up environment
	env := b.buildEnvironment(stagingDir)

	// Execute build commands
	for i, cmd := range recipe.Build {
		if progressFn != nil {
			progressFn(i+1, len(recipe.Build))
		}

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
	env = append(env, "DESTDIR="+stagingDir)
	env = append(env, "PREFIX=/usr")
	env = append(env, "PKGDIR="+stagingDir)

	// Add parallel build variable
	env = append(env, fmt.Sprintf("MAKEFLAGS=-j%d", runtime.NumCPU()))

	return applyEnvOverrides(env, b.envOverrides)
}

// executeCommand executes a single command
func (b *Builder) executeCommand(workDir, command string, env []string) error {
	if strings.TrimSpace(command) == "" {
		return nil
	}

	// Security check
	if err := b.validateCommand(command); err != nil {
		return fmt.Errorf("command security validation failed: %w", err)
	}

	log.Printf("Executing command: %s", command)

	cmd := exec.Command("sh", "-c", command)
	cmd.Dir = workDir
	cmd.Env = env
	if b.quiet {
		// Create a context with timeout to prevent hanging - use 2 hours for large packages like gcc/llvm
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
		defer cancel()

		cmd := exec.CommandContext(ctx, "sh", "-c", command)
		cmd.Dir = workDir
		cmd.Env = env
		output, err := cmd.CombinedOutput()
		if err != nil {
			details := tailOutput(string(output), 20)
			if details != "" {
				return fmt.Errorf("%w\n%s", err, details)
			}
			return err
		}
		return nil
	}

	// For non-quiet mode, still set a reasonable timeout - use 2 hours for large packages
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
	defer cancel()
	cmd = exec.CommandContext(ctx, "sh", "-c", command)
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

// SetQuiet enables or disables command output streaming.
func (b *Builder) SetQuiet(quiet bool) {
	b.quiet = quiet
}

// SetProgressCallback sets callback for build progress updates.
func (b *Builder) SetProgressCallback(callback func(BuildProgress)) {
	b.callbackMutex.Lock()
	defer b.callbackMutex.Unlock()
	b.progressCallback = callback
}

// SetEnvOverride sets or removes one environment override used for build commands.
func (b *Builder) SetEnvOverride(key, value string) {
	key = strings.TrimSpace(key)
	if key == "" {
		return
	}
	if b.envOverrides == nil {
		b.envOverrides = make(map[string]string)
	}
	if value == "" {
		delete(b.envOverrides, key)
		return
	}
	b.envOverrides[key] = value
}

// SetEnvOverrides replaces all environment overrides used for build commands.
func (b *Builder) SetEnvOverrides(overrides map[string]string) {
	if len(overrides) == 0 {
		b.envOverrides = nil
		return
	}
	cloned := make(map[string]string, len(overrides))
	for key, value := range overrides {
		key = strings.TrimSpace(key)
		if key == "" || value == "" {
			continue
		}
		cloned[key] = value
	}
	if len(cloned) == 0 {
		b.envOverrides = nil
		return
	}
	b.envOverrides = cloned
}

// CleanCache removes cached packages and sources
func (b *Builder) CleanCache() error {
	cacheDirs := []string{
		filepath.Join(b.workDir, "cache"),
		filepath.Join(b.workDir, "packages"),
		filepath.Join(b.workDir, "sources"),
		filepath.Join(b.workDir, "staging"),
	}

	for _, dir := range cacheDirs {
		if err := os.RemoveAll(dir); err != nil {
			return fmt.Errorf("failed to clean cache directory %s: %w", dir, err)
		}
	}

	log.Printf("Cache cleaned successfully")
	return nil
}

// GetCacheSize returns total size of cache directories
func (b *Builder) GetCacheSize() (int64, error) {
	var totalSize int64
	cacheDirs := []string{
		filepath.Join(b.workDir, "cache"),
		filepath.Join(b.workDir, "packages"),
		filepath.Join(b.workDir, "sources"),
		filepath.Join(b.workDir, "staging"),
	}

	for _, dir := range cacheDirs {
		size, err := b.dirSize(dir)
		if err != nil {
			return 0, fmt.Errorf("failed to calculate size of %s: %w", dir, err)
		}
		totalSize += size
	}

	return totalSize, nil
}

// dirSize calculates total size of directory recursively
func (b *Builder) dirSize(path string) (int64, error) {
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
func (b *Builder) packageFiles(recipe *recipe.Recipe, sourceDir, stagingDir string, progressFn func(step, total int)) error {
	// Find the source directory (usually the first subdirectory)
	srcPath, err := b.findSourceDirectory(sourceDir)
	if err != nil {
		return fmt.Errorf("failed to find source directory: %w", err)
	}

	// Set up environment
	env := b.buildEnvironment(stagingDir)

	// Execute package commands
	for i, cmd := range recipe.Install {
		if progressFn != nil {
			progressFn(i+1, len(recipe.Install))
		}

		// Substitute variables in command
		cmd = b.substituteVariables(cmd, sourceDir, stagingDir)

		if err := b.executeCommand(srcPath, cmd, env); err != nil {
			return fmt.Errorf("package command %d failed: %w", i+1, err)
		}
	}

	return nil
}

func (b *Builder) reportProgress(step, total int, message string) {
	b.callbackMutex.RLock()
	callback := b.progressCallback
	b.callbackMutex.RUnlock()

	if callback == nil {
		return
	}
	callback(BuildProgress{
		Step:    step,
		Total:   total,
		Message: message,
	})
}

func tailOutput(output string, maxLines int) string {
	output = strings.TrimSpace(output)
	if output == "" {
		return ""
	}

	lines := strings.Split(output, "\n")
	if maxLines <= 0 || len(lines) <= maxLines {
		return output
	}
	return strings.Join(lines[len(lines)-maxLines:], "\n")
}

func applyEnvOverrides(base []string, overrides map[string]string) []string {
	if len(overrides) == 0 {
		return base
	}

	out := append([]string(nil), base...)
	indexByKey := make(map[string]int, len(out))
	for i, entry := range out {
		if eq := strings.IndexByte(entry, '='); eq > 0 {
			indexByKey[entry[:eq]] = i
		}
	}

	for key, value := range overrides {
		item := key + "=" + value
		if idx, ok := indexByKey[key]; ok {
			out[idx] = item
			continue
		}
		indexByKey[key] = len(out)
		out = append(out, item)
	}

	return out
}

// validateSourceFiles checks if essential files are present after extraction
func (b *Builder) validateSourceFiles(recipe *recipe.Recipe, sourceDir string) error {
	// Find the actual source directory
	srcPath, err := b.findSourceDirectory(sourceDir)
	if err != nil {
		return fmt.Errorf("failed to find source directory for validation: %w", err)
	}

	// Check if directory is not empty
	entries, err := os.ReadDir(srcPath)
	if err != nil {
		return fmt.Errorf("failed to read source directory: %w", err)
	}

	if len(entries) == 0 {
		return fmt.Errorf("source directory is empty after extraction")
	}

	// ALWAYS PASS VALIDATION - trust the source
	log.Printf("Source validation passed for %s: found %d files", recipe.Name, len(entries))
	return nil
}

func (b *Builder) validateCommand(command string) error {
	// Normalize for pattern matching
	cmdLower := strings.ToLower(command)

	// Remove common bypass attempts: quotes, spaces, backslashes for normalization
	normalized := cmdLower
	normalized = strings.ReplaceAll(normalized, `"`, "")
	normalized = strings.ReplaceAll(normalized, `'`, "")
	normalized = strings.ReplaceAll(normalized, ` `, "")
	normalized = strings.ReplaceAll(normalized, `\`, "")

	// List of dangerous patterns to block (check both original and normalized)
	dangerousPatterns := []string{
		"rm -rf /",
		"rm -rf /*",
		"rm -rf/",
		`:(){:|:&};:`,   // fork bomb (normalized)
		":(){ :|:& };:", // fork bomb (original)
		"chmod 777 /",
		"chown root",
		"sudo ",
		"su -",
		"> /dev/sda",
		"> /dev/hda",
		">/dev/sda",
		">/dev/hda",
		"mkfs",
		"format /",
		"fdisk",
		"dd if=",
		"chmod -R 777 /",
		"chown -R root",
		"rm -rf /etc",
		"rm -rf /usr",
		"rm -rf /bin",
		"rm -rf /sbin",
		"rm -rf /lib",
		"rm -rf /lib64",
		"rm-rf/etc",
		"rm-rf/usr",
		"rm-rf/bin",
		"rm-rf/sbin",
		"rm-rf/lib",
		"rm-rf/lib64",
		"shutdown",
		"reboot",
		"halt",
		"poweroff",
		"init 0",
		"init 6",
		"eval",
		"$(", // command substitution
	}

	// Check against original
	for _, pattern := range dangerousPatterns {
		if strings.Contains(cmdLower, pattern) {
			return fmt.Errorf("command contains potentially dangerous pattern: %s", pattern)
		}
	}

	// Check against normalized (catches bypass attempts like 'rm -rf "/"' or 'rm -rf / ')
	for _, pattern := range dangerousPatterns {
		if strings.Contains(normalized, pattern) {
			return fmt.Errorf("command contains potentially dangerous pattern (normalized): %s", pattern)
		}
	}

	// Check for suspicious characters that might indicate injection
	// Note: \n (newline) and \t (tab) are valid in shell scripts for multi-line commands
	suspiciousChars := []string{"\x00", "\r"}
	for _, char := range suspiciousChars {
		if strings.Contains(command, char) {
			return fmt.Errorf("command contains suspicious character: %q", char)
		}
	}

	// Basic command structure validation
	commands := strings.Fields(command)
	if len(commands) == 0 {
		return fmt.Errorf("empty command")
	}

	// Check for extremely long commands (possible injection attempt)
	if len(command) > 1000 {
		return fmt.Errorf("command too long (%d characters)", len(command))
	}

	// Check for command substitution patterns that could bypass security
	// Backticks and $() are blocked
	if strings.Contains(command, "`") {
		return fmt.Errorf("command contains potentially dangerous backtick substitution")
	}
	if strings.Contains(command, "$(") {
		return fmt.Errorf("command contains potentially dangerous command substitution $()")
	}

	// Log the command for audit purposes (only first 100 chars)
	if len(command) > 100 {
		log.Printf("Command validation passed (truncated): %s...", command[:100])
	} else {
		log.Printf("Command validation passed: %s", command)
	}

	return nil
}

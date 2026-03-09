package packager

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mholt/archiver/v3"
	"zsvo/pkg/recipe"
	"zsvo/pkg/types"
)

// Packager handles creating and extracting package archives
type Packager struct {
	workDir string
}

// NewPackager creates a new packager
func NewPackager(workDir string) *Packager {
	return &Packager{
		workDir: workDir,
	}
}

// Package creates a package archive from staging directory
func (p *Packager) Package(recipe *recipe.Recipe) error {
	// Validate recipe
	if recipe == nil {
		return fmt.Errorf("recipe cannot be nil")
	}
	if recipe.Name == "" {
		return fmt.Errorf("recipe name cannot be empty")
	}
	if recipe.Version == "" {
		return fmt.Errorf("recipe version cannot be empty")
	}

	stagingDir := recipe.GetStagingDir(p.workDir)
	packageDir := recipe.GetPackageDir(p.workDir)
	packageFile := filepath.Join(packageDir, recipe.GetPackageFileName())

	// Validate paths
	if stagingDir == "" {
		return fmt.Errorf("staging directory cannot be empty")
	}
	if packageDir == "" {
		return fmt.Errorf("package directory cannot be empty")
	}
	if packageFile == "" {
		return fmt.Errorf("package file path cannot be empty")
	}

	// Clean and normalize paths
	stagingDir = filepath.Clean(stagingDir)
	packageDir = filepath.Clean(packageDir)
	packageFile = filepath.Clean(packageFile)

	// Create package directory
	if err := os.MkdirAll(packageDir, 0755); err != nil {
		return fmt.Errorf("failed to create package directory %s: %w", packageDir, err)
	}

	// List files in staging directory
	files, err := p.listFiles(stagingDir)
	if err != nil {
		return fmt.Errorf("failed to list files in %s: %w", stagingDir, err)
	}

	// Create package info
	pkgInfo := &types.PkgInfo{
		Name:         recipe.Name,
		Version:      recipe.Version,
		Description:  recipe.Description,
		Dependencies: recipe.Dependencies,
		Files:        files,
		InstallDate:  time.Now().Format(time.RFC3339),
	}

	// Create package info file
	pkgInfoFile := filepath.Join(packageDir, ".pkginfo")
	if err := p.writePkgInfo(pkgInfoFile, pkgInfo); err != nil {
		return fmt.Errorf("failed to write package info to %s: %w", pkgInfoFile, err)
	}

	// Create package archive
	if err := p.createArchive(stagingDir, packageFile, pkgInfoFile); err != nil {
		return fmt.Errorf("failed to create package archive %s: %w", packageFile, err)
	}

	return nil
}

// listFiles lists all files in a directory recursively
func (p *Packager) listFiles(dir string) ([]string, error) {
	var files []string

	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			// Convert to relative path
			relPath, err := filepath.Rel(dir, path)
			if err != nil {
				return err
			}
			files = append(files, relPath)
		}
		return nil
	})

	return files, err
}

// writePkgInfo writes package info to file
func (p *Packager) writePkgInfo(path string, pkgInfo *types.PkgInfo) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()

	// Write package info in simple format
	content := fmt.Sprintf(
		"name = %q\nversion = %q\ndescription = %q\ndependencies = %v\nfiles = %v\ninstall_date = %q\n",
		pkgInfo.Name, pkgInfo.Version, pkgInfo.Description, pkgInfo.Dependencies, pkgInfo.Files, pkgInfo.InstallDate,
	)
	_, err = file.WriteString(content)
	return err
}

// createArchive creates a compressed archive
func (p *Packager) createArchive(sourceDir, archivePath, pkgInfoFile string) error {
	// Create archive using archiver.Archive
	paths := []string{sourceDir, pkgInfoFile}
	return archiver.Archive(paths, archivePath)
}

// Extract extracts a package archive
func (p *Packager) Extract(archivePath, destDir string) error {
	// Create destination directory
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return fmt.Errorf("failed to create destination directory: %w", err)
	}

	// Extract archive using archiver.Unarchive
	return archiver.Unarchive(archivePath, destDir)
}

// ReadPkgInfo reads package info from archive
func (p *Packager) ReadPkgInfo(archivePath string) (*types.PkgInfo, error) {
	// Extract to temporary directory
	tmpDir, err := os.MkdirTemp("", "pkginfo-")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp directory: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	if err := p.Extract(archivePath, tmpDir); err != nil {
		return nil, fmt.Errorf("failed to extract archive: %w", err)
	}

	// Read package info
	pkgInfoFile := filepath.Join(tmpDir, ".pkginfo")

	// Check if .pkginfo file exists
	if _, err := os.Stat(pkgInfoFile); os.IsNotExist(err) {
		return nil, fmt.Errorf("package info file not found in archive")
	}

	return p.readPkgInfoFromFile(pkgInfoFile)
}

// readPkgInfoFromFile reads package info from file
func (p *Packager) readPkgInfoFromFile(path string) (*types.PkgInfo, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var pkgInfo types.PkgInfo
	// Simple parser for our package info format
	// In a real implementation, you'd want to use a proper TOML parser
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}

		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])

		// Remove quotes from value
		value = strings.Trim(value, "\"")

		switch key {
		case "name":
			pkgInfo.Name = value
		case "version":
			pkgInfo.Version = value
		case "description":
			pkgInfo.Description = value
		case "dependencies":
			// Parse dependencies array
			value = strings.Trim(value, "[]")
			if value != "" {
				pkgInfo.Dependencies = strings.Split(value, ",")
				for i, dep := range pkgInfo.Dependencies {
					pkgInfo.Dependencies[i] = strings.TrimSpace(strings.Trim(dep, "\""))
				}
			}
		case "files":
			// Parse files array
			value = strings.Trim(value, "[]")
			if value != "" {
				pkgInfo.Files = strings.Split(value, ",")
				for i, file := range pkgInfo.Files {
					pkgInfo.Files[i] = strings.TrimSpace(strings.Trim(file, "\""))
				}
			}
		case "install_date":
			pkgInfo.InstallDate = value
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	// Validate that we actually parsed some data
	if pkgInfo.Name == "" {
		return nil, fmt.Errorf("invalid package info: missing name")
	}

	return &pkgInfo, nil
}

// VerifyPackage verifies package integrity
func (p *Packager) VerifyPackage(archivePath string) error {
	// Check if file exists
	if _, err := os.Stat(archivePath); err != nil {
		return fmt.Errorf("package file does not exist: %w", err)
	}

	// Try to extract to verify integrity
	tmpDir, err := os.MkdirTemp("", "verify-")
	if err != nil {
		return fmt.Errorf("failed to create temp directory: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	return p.Extract(archivePath, tmpDir)
}

// ListPackageContents lists contents of a package
func (p *Packager) ListPackageContents(archivePath string) ([]string, error) {
	// Extract to temporary directory
	tmpDir, err := os.MkdirTemp("", "list-")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp directory: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	if err := p.Extract(archivePath, tmpDir); err != nil {
		return nil, fmt.Errorf("failed to extract archive: %w", err)
	}

	// List files
	return p.listFiles(tmpDir)
}

// GetPackageDir returns the package directory
func (p *Packager) GetPackageDir(recipe *recipe.Recipe) string {
	return recipe.GetPackageDir(p.workDir)
}

// GetPackageFile returns the package file path
func (p *Packager) GetPackageFile(recipe *recipe.Recipe) string {
	return filepath.Join(p.GetPackageDir(recipe), recipe.GetPackageFileName())
}

// Clean removes package files
func (p *Packager) Clean(recipe *recipe.Recipe) error {
	packageDir := p.GetPackageDir(recipe)
	return os.RemoveAll(packageDir)
}

// pkgInfoFileInfo implements os.FileInfo for package info
type pkgInfoFileInfo struct {
	name string
	size int64
}

func (f *pkgInfoFileInfo) Name() string       { return f.name }
func (f *pkgInfoFileInfo) Size() int64        { return f.size }
func (f *pkgInfoFileInfo) Mode() os.FileMode  { return 0644 }
func (f *pkgInfoFileInfo) ModTime() time.Time { return time.Now() }
func (f *pkgInfoFileInfo) IsDir() bool        { return false }
func (f *pkgInfoFileInfo) Sys() interface{}   { return nil }

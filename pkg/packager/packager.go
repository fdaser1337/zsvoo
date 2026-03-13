package packager

import (
	"archive/tar"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/klauspost/compress/zstd"
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
		Dependencies: recipe.Deps,
		Files:        files,
		InstallDate:  time.Now().Format(time.RFC3339),
	}

	// Create package info file
	pkgInfoFile := filepath.Join(stagingDir, types.PackageMetadataFile)
	if err := p.writePkgInfo(pkgInfoFile, pkgInfo); err != nil {
		return fmt.Errorf("failed to write package info to %s: %w", pkgInfoFile, err)
	}
	defer os.Remove(pkgInfoFile)

	// Create package archive
	if err := p.createArchive(stagingDir, packageFile); err != nil {
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

	return types.WritePkgInfo(file, pkgInfo)
}

// createArchive creates a compressed archive
func (p *Packager) createArchive(sourceDir, archivePath string) error {
	outFile, err := os.Create(archivePath)
	if err != nil {
		return fmt.Errorf("failed to create archive file: %w", err)
	}
	
	// Ensure file is closed on error
	var closeErr error
	defer func() {
		if cerr := outFile.Close(); cerr != nil {
			closeErr = cerr
		}
		// Remove partial file on error
		if err != nil || closeErr != nil {
			os.Remove(archivePath)
		}
	}()

	zstdWriter, err := zstd.NewWriter(outFile)
	if err != nil {
		return fmt.Errorf("failed to create zstd writer: %w", err)
	}
	defer func() {
		if cerr := zstdWriter.Close(); cerr != nil {
			closeErr = cerr
		}
	}()

	tarWriter := tar.NewWriter(zstdWriter)
	defer func() {
		if cerr := tarWriter.Close(); cerr != nil {
			closeErr = cerr
		}
	}()

	walkErr := filepath.Walk(sourceDir, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == sourceDir {
			return nil
		}

		relPath, err := filepath.Rel(sourceDir, path)
		if err != nil {
			return fmt.Errorf("failed to get relative path for %s: %w", path, err)
		}
		relPath = filepath.ToSlash(relPath)

		var linkTarget string
		if info.Mode()&os.ModeSymlink != 0 {
			linkTarget, err = os.Readlink(path)
			if err != nil {
				return fmt.Errorf("failed to read symlink %s: %w", path, err)
			}
		}

		header, err := tar.FileInfoHeader(info, linkTarget)
		if err != nil {
			return fmt.Errorf("failed to create tar header for %s: %w", path, err)
		}
		header.Name = relPath

		if err := tarWriter.WriteHeader(header); err != nil {
			return fmt.Errorf("failed to write tar header for %s: %w", path, err)
		}

		if !info.Mode().IsRegular() {
			return nil
		}

		file, err := os.Open(path)
		if err != nil {
			return fmt.Errorf("failed to open file %s: %w", path, err)
		}
		defer file.Close()

		if _, err := io.Copy(tarWriter, file); err != nil {
			return fmt.Errorf("failed to copy file %s to archive: %w", path, err)
		}
		return nil
	})

	if walkErr != nil {
		return walkErr
	}
	if closeErr != nil {
		return closeErr
	}

	return nil
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
	pkgInfoFile := filepath.Join(tmpDir, types.PackageMetadataFile)

	// Check if metadata file exists.
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

	return types.ReadPkgInfo(file)
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

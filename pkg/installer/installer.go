package installer

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"zsvo/pkg/packager"
	"zsvo/pkg/types"
)

// Installer handles installing and removing packages
type Installer struct {
	rootDir string
	pkgDB   string
}

// NewInstaller creates a new installer
func NewInstaller(rootDir string) *Installer {
	return &Installer{
		rootDir: rootDir,
		pkgDB:   filepath.Join(rootDir, "var", "lib", "pkgdb"),
	}
}

// Install installs a package
func (i *Installer) Install(packagePath string) error {
	// Extract package to temporary directory
	tmpDir, err := os.MkdirTemp("", "install-")
	if err != nil {
		return fmt.Errorf("failed to create temp directory: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	// Extract package
	pkg := packager.NewPackager(i.rootDir)
	if err := pkg.Extract(packagePath, tmpDir); err != nil {
		return fmt.Errorf("failed to extract package: %w", err)
	}

	// Read package info
	pkgInfo, err := pkg.ReadPkgInfo(packagePath)
	if err != nil {
		return fmt.Errorf("failed to read package info: %w", err)
	}

	// Check dependencies
	if err := i.checkDependencies(pkgInfo.Dependencies); err != nil {
		return fmt.Errorf("dependency check failed: %w", err)
	}

	// Install files
	if err := i.installFiles(tmpDir, pkgInfo); err != nil {
		return fmt.Errorf("failed to install files: %w", err)
	}

	// Register package
	if err := i.registerPackage(pkgInfo); err != nil {
		return fmt.Errorf("failed to register package: %w", err)
	}

	return nil
}

// Remove removes a package
func (i *Installer) Remove(packageName string) error {
	// Get package info
	pkgInfo, err := i.getPackageInfo(packageName)
	if err != nil {
		return fmt.Errorf("package not found: %w", err)
	}

	// Remove files
	if err := i.removeFiles(pkgInfo); err != nil {
		return fmt.Errorf("failed to remove files: %w", err)
	}

	// Unregister package
	if err := i.unregisterPackage(packageName); err != nil {
		return fmt.Errorf("failed to unregister package: %w", err)
	}

	return nil
}

// ListInstalled lists all installed packages
func (i *Installer) ListInstalled() ([]string, error) {
	var packages []string

	// Check if pkgdb directory exists
	if _, err := os.Stat(i.pkgDB); os.IsNotExist(err) {
		return packages, nil
	}

	// Read pkgdb directory
	entries, err := os.ReadDir(i.pkgDB)
	if err != nil {
		return nil, fmt.Errorf("failed to read package database: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			packages = append(packages, entry.Name())
		}
	}

	return packages, nil
}

// GetPackageInfo gets information about an installed package
func (i *Installer) GetPackageInfo(packageName string) (*types.PkgInfo, error) {
	return i.getPackageInfo(packageName)
}

// checkDependencies checks if all dependencies are installed
func (i *Installer) checkDependencies(deps []string) error {
	installed, err := i.ListInstalled()
	if err != nil {
		return err
	}

	for _, dep := range deps {
		found := false
		for _, installedPkg := range installed {
			if installedPkg == dep {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("dependency not installed: %s", dep)
		}
	}

	return nil
}

// installFiles installs files from package
func (i *Installer) installFiles(tmpDir string, pkgInfo *types.PkgInfo) error {
	// Validate input paths
	if tmpDir == "" {
		return fmt.Errorf("temporary directory cannot be empty")
	}
	if i.rootDir == "" {
		return fmt.Errorf("root directory cannot be empty")
	}

	// Clean and normalize paths
	tmpDir = filepath.Clean(tmpDir)
	i.rootDir = filepath.Clean(i.rootDir)

	// Create root directory if it doesn't exist
	if err := os.MkdirAll(i.rootDir, 0755); err != nil {
		return fmt.Errorf("failed to create root directory %s: %w", i.rootDir, err)
	}

	// Copy files to root directory
	for _, file := range pkgInfo.Files {
		// Validate file path
		if file == "" {
			return fmt.Errorf("file path cannot be empty")
		}

		// Clean file path to prevent directory traversal
		file = filepath.Clean(file)
		if strings.HasPrefix(file, "..") {
			return fmt.Errorf("invalid file path: %s", file)
		}

		src := filepath.Join(tmpDir, file)
		dst := filepath.Join(i.rootDir, file)

		// Create parent directory
		if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", filepath.Dir(dst), err)
		}

		// Copy file
		if err := i.copyFile(src, dst); err != nil {
			return fmt.Errorf("failed to copy file %s: %w", file, err)
		}
	}

	return nil
}

// removeFiles removes files of a package
func (i *Installer) removeFiles(pkgInfo *types.PkgInfo) error {
	for _, file := range pkgInfo.Files {
		path := filepath.Join(i.rootDir, file)
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("failed to remove file %s: %w", file, err)
		}
	}
	return nil
}

// registerPackage registers a package in the database
func (i *Installer) registerPackage(pkgInfo *types.PkgInfo) error {
	// Create package database directory
	pkgDir := filepath.Join(i.pkgDB, pkgInfo.Name)
	if err := os.MkdirAll(pkgDir, 0755); err != nil {
		return fmt.Errorf("failed to create package database directory: %w", err)
	}

	// Write package info
	pkgInfoFile := filepath.Join(pkgDir, ".pkginfo")
	file, err := os.Create(pkgInfoFile)
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

// unregisterPackage unregisters a package from the database
func (i *Installer) unregisterPackage(packageName string) error {
	pkgDir := filepath.Join(i.pkgDB, packageName)
	return os.RemoveAll(pkgDir)
}

// getPackageInfo gets package info from database
func (i *Installer) getPackageInfo(packageName string) (*types.PkgInfo, error) {
	pkgDir := filepath.Join(i.pkgDB, packageName)
	pkgInfoFile := filepath.Join(pkgDir, ".pkginfo")

	// Check if package directory exists
	if _, err := os.Stat(pkgDir); os.IsNotExist(err) {
		return nil, fmt.Errorf("package %s not found in database", packageName)
	}

	// Check if .pkginfo file exists
	if _, err := os.Stat(pkgInfoFile); os.IsNotExist(err) {
		return nil, fmt.Errorf("package info file not found for %s", packageName)
	}

	file, err := os.Open(pkgInfoFile)
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

// copyFile copies a file from src to dst
func (i *Installer) copyFile(src, dst string) error {
	sourceFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	destFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer destFile.Close()

	_, err = io.Copy(destFile, sourceFile)
	return err
}

// GetRootDir returns the root directory
func (i *Installer) GetRootDir() string {
	return i.rootDir
}

// GetPkgDB returns the package database directory
func (i *Installer) GetPkgDB() string {
	return i.pkgDB
}

// IsInstalled checks if a package is installed
func (i *Installer) IsInstalled(packageName string) bool {
	pkgDir := filepath.Join(i.pkgDB, packageName)
	_, err := os.Stat(pkgDir)
	return err == nil
}

// GetInstalledVersion gets the installed version of a package
func (i *Installer) GetInstalledVersion(packageName string) (string, error) {
	pkgInfo, err := i.getPackageInfo(packageName)
	if err != nil {
		return "", err
	}
	return pkgInfo.Version, nil
}

// GetInstalledFiles gets the list of installed files for a package
func (i *Installer) GetInstalledFiles(packageName string) ([]string, error) {
	pkgInfo, err := i.getPackageInfo(packageName)
	if err != nil {
		return nil, err
	}
	return pkgInfo.Files, nil
}

// VerifyPackage verifies that all files of a package exist
func (i *Installer) VerifyPackage(packageName string) error {
	pkgInfo, err := i.getPackageInfo(packageName)
	if err != nil {
		return err
	}

	for _, file := range pkgInfo.Files {
		path := filepath.Join(i.rootDir, file)
		if _, err := os.Stat(path); err != nil {
			return fmt.Errorf("missing file: %s", file)
		}
	}

	return nil
}

// GetPackageSize gets the total size of installed package files
func (i *Installer) GetPackageSize(packageName string) (int64, error) {
	pkgInfo, err := i.getPackageInfo(packageName)
	if err != nil {
		return 0, err
	}

	var totalSize int64
	for _, file := range pkgInfo.Files {
		path := filepath.Join(i.rootDir, file)
		info, err := os.Stat(path)
		if err != nil {
			return 0, err
		}
		totalSize += info.Size()
	}

	return totalSize, nil
}

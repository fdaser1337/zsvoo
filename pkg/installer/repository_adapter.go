package installer

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"zsvo/pkg/deps"
	"zsvo/pkg/packager"
)

// InstallerPackageRepository implements deps.PackageRepository for Installer
type InstallerPackageRepository struct {
	installer *Installer
}

func NewInstallerPackageRepository(installer *Installer) *InstallerPackageRepository {
	return &InstallerPackageRepository{installer: installer}
}

func (r *InstallerPackageRepository) GetInstalled() (map[string]*deps.PackageInfo, error) {
	installed, err := r.installer.installedPkgInfosNoLock()
	if err != nil {
		return nil, fmt.Errorf("failed to get installed packages: %w", err)
	}

	result := make(map[string]*deps.PackageInfo, len(installed))
	for name, info := range installed {
		result[name] = &deps.PackageInfo{
			Name:         info.Name,
			Version:      info.Version,
			Dependencies: info.Dependencies,
		}
	}

	return result, nil
}

func (r *InstallerPackageRepository) GetPackage(name string) (*deps.PackageInfo, error) {
	info, err := r.installer.getPackageInfoNoLock(name)
	if err != nil {
		return nil, err
	}

	return &deps.PackageInfo{
		Name:         info.Name,
		Version:      info.Version,
		Dependencies: info.Dependencies,
	}, nil
}

// SearchPackages searches for packages in available paths
func (r *InstallerPackageRepository) SearchPackages(query string) ([]*deps.PackageInfo, error) {
	searchPaths := []string{
		filepath.Join(r.installer.rootDir, "var", "cache", "packages"),
		filepath.Join("/var", "cache", "packages"),
		filepath.Join(r.installer.rootDir, "work", "packages"),
	}

	var results []*deps.PackageInfo

	for _, searchPath := range searchPaths {
		if _, err := os.Stat(searchPath); os.IsNotExist(err) {
			continue
		}

		pkgs, err := r.searchPackagesInPath(query, searchPath)
		if err != nil {
			continue // Skip paths that can't be read
		}

		results = append(results, pkgs...)
	}

	return results, nil
}

// searchPackagesInPath searches for packages in a specific directory
func (r *InstallerPackageRepository) searchPackagesInPath(query, searchPath string) ([]*deps.PackageInfo, error) {
	entries, err := os.ReadDir(searchPath)
	if err != nil {
		return nil, err
	}

	var results []*deps.PackageInfo
	queryLower := strings.ToLower(query)

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		if !strings.HasSuffix(name, ".pkg.tar.zst") {
			continue
		}

		// Extract package name from filename
		pkgName := strings.TrimSuffix(name, ".pkg.tar.zst")

		// Check if query matches package name
		if strings.Contains(strings.ToLower(pkgName), queryLower) {
			// Try to read package info using packager directly
			pkgPath := filepath.Join(searchPath, name)
			p := packager.NewPackager(r.installer.rootDir)
			pkgInfo, err := p.ReadPkgInfo(pkgPath)
			if err != nil {
				continue // Skip packages that can't be read
			}

			results = append(results, &deps.PackageInfo{
				Name:         pkgInfo.Name,
				Version:      pkgInfo.Version,
				Dependencies: pkgInfo.Dependencies,
				Description:  pkgInfo.Description, // If available
			})
		}
	}

	return results, nil
}

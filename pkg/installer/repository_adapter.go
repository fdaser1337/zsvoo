package installer

import (
	"fmt"

	"zsvo/pkg/deps"
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

package installer

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"

	"zsvo/pkg/packager"
	"zsvo/pkg/types"
)

// Installer handles installing and removing packages.
type Installer struct {
	rootDir string
	pkgDB   string
}

// RemoveOptions controls package removal behavior.
type RemoveOptions struct {
	Cascade bool
}

func NewInstaller(rootDir string) *Installer {
	return &Installer{
		rootDir: rootDir,
		pkgDB:   filepath.Join(rootDir, "var", "lib", "pkgdb"),
	}
}

// Install installs a single package archive produced by zsvo build.
func (i *Installer) Install(packagePath string) error {
	return i.InstallMany([]string{packagePath})
}

// InstallMany installs multiple package archives in one transaction.
func (i *Installer) InstallMany(packagePaths []string) error {
	if len(packagePaths) == 0 {
		return fmt.Errorf("no packages provided")
	}

	return i.withDBLock(true, func() error {
		return i.installManyNoLock(packagePaths)
	})
}

// Upgrade upgrades packages from local package files.
func (i *Installer) Upgrade(packagePaths []string) error {
	return i.InstallMany(packagePaths)
}

// PlanRemove returns removal plan without changing filesystem.
func (i *Installer) PlanRemove(packageNames []string, options RemoveOptions) ([]string, error) {
	var plan []string
	if err := i.withDBLock(false, func() error {
		var err error
		plan, err = i.planRemoveNoLock(packageNames, options)
		return err
	}); err != nil {
		return nil, err
	}
	return plan, nil
}

// Remove removes a single installed package.
func (i *Installer) Remove(packageName string) error {
	_, err := i.RemoveMany([]string{packageName}, RemoveOptions{})
	return err
}

// RemoveMany removes one or more installed packages in one transaction.
func (i *Installer) RemoveMany(packageNames []string, options RemoveOptions) ([]string, error) {
	var removed []string
	if err := i.withDBLock(true, func() error {
		plan, err := i.planRemoveNoLock(packageNames, options)
		if err != nil {
			return err
		}

		removed, err = i.removeManyWithPlanNoLock(plan)
		return err
	}); err != nil {
		return nil, err
	}

	return removed, nil
}

// ListInstalled lists all installed packages.
func (i *Installer) ListInstalled() ([]string, error) {
	var packages []string
	if err := i.withDBLock(false, func() error {
		var err error
		packages, err = i.listInstalledNoLock()
		return err
	}); err != nil {
		return nil, err
	}

	return packages, nil
}

// ListOrphans returns installed packages that are not required by others.
func (i *Installer) ListOrphans() ([]string, error) {
	var orphans []string
	if err := i.withDBLock(false, func() error {
		infos, err := i.installedPkgInfosNoLock()
		if err != nil {
			return err
		}

		orphans = listOrphansFromInfos(infos)
		return nil
	}); err != nil {
		return nil, err
	}

	return orphans, nil
}

// GetPackageInfo returns metadata for an installed package.
func (i *Installer) GetPackageInfo(packageName string) (*types.PkgInfo, error) {
	var pkgInfo *types.PkgInfo
	if err := i.withDBLock(false, func() error {
		var err error
		pkgInfo, err = i.getPackageInfoNoLock(packageName)
		return err
	}); err != nil {
		return nil, err
	}

	return pkgInfo, nil
}

func (i *Installer) installManyNoLock(packagePaths []string) error {
	candidates, err := i.readInstallCandidatesNoLock(packagePaths)
	if err != nil {
		return err
	}

	installedInfos, err := i.installedPkgInfosNoLock()
	if err != nil {
		return err
	}

	txDir, err := os.MkdirTemp("", "install-many-")
	if err != nil {
		return fmt.Errorf("failed to create transaction directory: %w", err)
	}
	defer os.RemoveAll(txDir)

	applied := make([]installTransactionState, 0, len(candidates))
	remaining := append([]installCandidate(nil), candidates...)

	for len(remaining) > 0 {
		progress := false
		for idx, candidate := range remaining {
			if !depsSatisfied(candidate.info.Dependencies, installedInfos, candidate.info.Name) {
				continue
			}

			state, err := i.installPackageTxNoLock(candidate.path, txDir, installedInfos)
			if err != nil {
				if rbErr := i.rollbackInstallTransactionNoLock(applied); rbErr != nil {
					return fmt.Errorf("install failed for %s: %w (rollback error: %v)", candidate.info.Name, err, rbErr)
				}
				return fmt.Errorf("install failed for %s: %w", candidate.info.Name, err)
			}

			applied = append(applied, *state)
			installedInfos[candidate.info.Name] = candidate.info
			remaining = append(remaining[:idx], remaining[idx+1:]...)
			progress = true
			break
		}

		if progress {
			continue
		}

		block := remaining[0]
		missing := missingDeps(block.info.Dependencies, installedInfos, block.info.Name)
		if len(missing) == 0 {
			missing = []string{"unknown ordering issue"}
		}
		if rbErr := i.rollbackInstallTransactionNoLock(applied); rbErr != nil {
			return fmt.Errorf(
				"cannot resolve install order for %s: missing deps %s (rollback error: %v)",
				block.info.Name,
				strings.Join(missing, ", "),
				rbErr,
			)
		}
		return fmt.Errorf("cannot resolve install order for %s: missing deps %s", block.info.Name, strings.Join(missing, ", "))
	}

	return nil
}

func (i *Installer) readInstallCandidatesNoLock(packagePaths []string) ([]installCandidate, error) {
	p := packager.NewPackager(i.rootDir)

	candidates := make([]installCandidate, 0, len(packagePaths))
	seenNames := make(map[string]string, len(packagePaths))
	for _, packagePath := range packagePaths {
		if strings.TrimSpace(packagePath) == "" {
			return nil, fmt.Errorf("package path cannot be empty")
		}

		pkgInfo, err := p.ReadPkgInfo(packagePath)
		if err != nil {
			return nil, fmt.Errorf("failed to read package metadata from %s: %w", packagePath, err)
		}
		if err := validateSimpleDependencies(pkgInfo.Dependencies); err != nil {
			return nil, fmt.Errorf("invalid dependencies in %s: %w", packagePath, err)
		}

		if prev, exists := seenNames[pkgInfo.Name]; exists {
			return nil, fmt.Errorf("duplicate package %s in transaction: %s and %s", pkgInfo.Name, prev, packagePath)
		}
		seenNames[pkgInfo.Name] = packagePath

		candidates = append(candidates, installCandidate{path: packagePath, info: pkgInfo})
	}

	return candidates, nil
}

func (i *Installer) installPackageTxNoLock(packagePath, txDir string, installedInfos map[string]*types.PkgInfo) (*installTransactionState, error) {
	pkgTxDir, err := os.MkdirTemp(txDir, "pkg-")
	if err != nil {
		return nil, fmt.Errorf("failed to create package transaction directory: %w", err)
	}

	extractDir := filepath.Join(pkgTxDir, "extract")
	p := packager.NewPackager(i.rootDir)
	if err := p.Extract(packagePath, extractDir); err != nil {
		return nil, fmt.Errorf("failed to extract package: %w", err)
	}

	pkgInfo, err := p.ReadPkgInfo(packagePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read package metadata: %w", err)
	}
	if err := validateSimpleDependencies(pkgInfo.Dependencies); err != nil {
		return nil, err
	}

	if err := checkDependenciesAgainstInstalled(pkgInfo.Dependencies, installedInfos, pkgInfo.Name); err != nil {
		return nil, err
	}

	if err := i.checkFileOwnershipNoLock(pkgInfo, installedInfos); err != nil {
		return nil, err
	}

	extractRoot, err := i.resolveExtractRoot(extractDir, pkgInfo)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve package root: %w", err)
	}

	state := &installTransactionState{pkgInfo: pkgInfo, txDir: pkgTxDir}

	// Same-name install is treated as upgrade/reinstall.
	if oldInfo, exists := installedInfos[pkgInfo.Name]; exists {
		replacedBackupRoot := filepath.Join(pkgTxDir, "replaced")
		backups, err := i.removeFiles(oldInfo, replacedBackupRoot)
		if err != nil {
			return nil, fmt.Errorf("failed to remove old package %s files: %w", pkgInfo.Name, err)
		}
		if err := i.unregisterPackage(pkgInfo.Name); err != nil {
			_ = i.rollbackRemove(backups)
			return nil, fmt.Errorf("failed to unregister old package %s: %w", pkgInfo.Name, err)
		}

		state.replaced = &removeTransactionState{
			pkgName: pkgInfo.Name,
			info:    oldInfo,
			backups: backups,
		}
	}

	backupRoot := filepath.Join(pkgTxDir, "new")
	installedPaths, backups, err := i.installFiles(extractRoot, pkgInfo, backupRoot)
	if err != nil {
		_ = i.rollbackInstall(installedPaths, backups)
		if state.replaced != nil {
			_ = i.rollbackRemove(state.replaced.backups)
			_ = i.registerPackage(state.replaced.info)
		}
		return nil, fmt.Errorf("failed to install files: %w", err)
	}
	state.installedPaths = installedPaths
	state.newBackups = backups

	if err := i.registerPackage(pkgInfo); err != nil {
		_ = i.rollbackInstall(installedPaths, backups)
		if state.replaced != nil {
			_ = i.rollbackRemove(state.replaced.backups)
			_ = i.registerPackage(state.replaced.info)
		}
		return nil, fmt.Errorf("failed to register package: %w", err)
	}

	return state, nil
}

func (i *Installer) rollbackInstallTransactionNoLock(applied []installTransactionState) error {
	if len(applied) == 0 {
		return nil
	}

	var errs []string
	for idx := len(applied) - 1; idx >= 0; idx-- {
		state := applied[idx]

		rollbackRoot := filepath.Join(state.txDir, "rollback-new")
		backups, err := i.removeFiles(state.pkgInfo, rollbackRoot)
		if err != nil {
			errs = append(errs, fmt.Sprintf("failed to remove %s during rollback: %v", state.pkgInfo.Name, err))
			continue
		}
		if err := i.unregisterPackage(state.pkgInfo.Name); err != nil {
			_ = i.rollbackRemove(backups)
			errs = append(errs, fmt.Sprintf("failed to unregister %s during rollback: %v", state.pkgInfo.Name, err))
			continue
		}

		if err := i.rollbackInstall(state.installedPaths, state.newBackups); err != nil {
			errs = append(errs, fmt.Sprintf("failed to rollback overwritten paths for %s: %v", state.pkgInfo.Name, err))
		}

		if state.replaced != nil {
			if err := i.rollbackRemove(state.replaced.backups); err != nil {
				errs = append(errs, fmt.Sprintf("failed to restore old files for %s: %v", state.replaced.pkgName, err))
			}
			if err := i.registerPackage(state.replaced.info); err != nil {
				errs = append(errs, fmt.Sprintf("failed to re-register %s: %v", state.replaced.pkgName, err))
			}
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("install transaction rollback failed: %s", strings.Join(errs, "; "))
	}
	return nil
}

func (i *Installer) planRemoveNoLock(packageNames []string, options RemoveOptions) ([]string, error) {
	names, err := normalizePackageNames(packageNames)
	if err != nil {
		return nil, err
	}

	infos, err := i.installedPkgInfosNoLock()
	if err != nil {
		return nil, err
	}

	removeSet := make(map[string]struct{}, len(names))
	for _, name := range names {
		if _, ok := infos[name]; !ok {
			return nil, fmt.Errorf("package %s not found in database", name)
		}
		removeSet[name] = struct{}{}
	}

	if options.Cascade {
		for {
			broken, err := brokenPackagesAfterRemoval(infos, removeSet)
			if err != nil {
				return nil, err
			}

			changed := false
			for _, name := range broken {
				if _, exists := removeSet[name]; exists {
					continue
				}
				removeSet[name] = struct{}{}
				changed = true
			}

			if !changed {
				break
			}
		}
	} else {
		broken, err := brokenPackagesAfterRemoval(infos, removeSet)
		if err != nil {
			return nil, err
		}
		if len(broken) > 0 {
			return nil, fmt.Errorf("cannot remove %s: required by %s", strings.Join(names, ", "), strings.Join(broken, ", "))
		}
	}

	plan := make([]string, 0, len(removeSet))
	for name := range removeSet {
		plan = append(plan, name)
	}
	sort.Strings(plan)
	return plan, nil
}

func (i *Installer) removeManyWithPlanNoLock(plan []string) ([]string, error) {
	if len(plan) == 0 {
		return nil, nil
	}

	txDir, err := os.MkdirTemp("", "remove-many-")
	if err != nil {
		return nil, fmt.Errorf("failed to create remove transaction directory: %w", err)
	}
	defer os.RemoveAll(txDir)

	states := make([]removeTransactionState, 0, len(plan))
	for _, packageName := range plan {
		state, err := i.removePackageTxNoLock(packageName, txDir)
		if err != nil {
			if rbErr := i.rollbackRemoveTransactionNoLock(states); rbErr != nil {
				return nil, fmt.Errorf("failed to remove package %s: %w (rollback error: %v)", packageName, err, rbErr)
			}
			return nil, fmt.Errorf("failed to remove package %s: %w", packageName, err)
		}
		states = append(states, *state)
	}

	return append([]string(nil), plan...), nil
}

func (i *Installer) removePackageTxNoLock(packageName, txDir string) (*removeTransactionState, error) {
	pkgInfo, err := i.getPackageInfoNoLock(packageName)
	if err != nil {
		return nil, fmt.Errorf("package not found: %w", err)
	}

	backupRoot := filepath.Join(txDir, packageName)
	backups, err := i.removeFiles(pkgInfo, backupRoot)
	if err != nil {
		return nil, fmt.Errorf("failed to remove files: %w", err)
	}
	if err := i.unregisterPackage(packageName); err != nil {
		_ = i.rollbackRemove(backups)
		return nil, fmt.Errorf("failed to unregister package: %w", err)
	}

	return &removeTransactionState{pkgName: packageName, info: pkgInfo, backups: backups}, nil
}

func (i *Installer) rollbackRemoveTransactionNoLock(states []removeTransactionState) error {
	if len(states) == 0 {
		return nil
	}

	var errs []string
	for idx := len(states) - 1; idx >= 0; idx-- {
		state := states[idx]
		if err := i.rollbackRemove(state.backups); err != nil {
			errs = append(errs, err.Error())
		}
		if err := i.registerPackage(state.info); err != nil {
			errs = append(errs, fmt.Sprintf("failed to re-register %s: %v", state.pkgName, err))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("remove transaction rollback failed: %s", strings.Join(errs, "; "))
	}
	return nil
}

func (i *Installer) checkFileOwnershipNoLock(pkgInfo *types.PkgInfo, installedInfos map[string]*types.PkgInfo) error {
	ownerByPath := make(map[string]string)
	for ownerName, info := range installedInfos {
		for _, file := range info.Files {
			relPath, err := sanitizePackagePath(file)
			if err != nil {
				return err
			}
			ownerByPath[relPath] = ownerName
		}
	}

	rootAbs, err := filepath.Abs(filepath.Clean(i.rootDir))
	if err != nil {
		return fmt.Errorf("failed to resolve root directory: %w", err)
	}

	for _, file := range pkgInfo.Files {
		relPath, err := sanitizePackagePath(file)
		if err != nil {
			return err
		}

		if owner, exists := ownerByPath[relPath]; exists && owner != pkgInfo.Name {
			return fmt.Errorf("file conflict: %s is owned by %s", relPath, owner)
		}

		dst, err := safeJoinRoot(rootAbs, relPath)
		if err != nil {
			return err
		}

		if _, err := os.Lstat(dst); err == nil {
			if _, owned := ownerByPath[relPath]; !owned {
				return fmt.Errorf("file conflict: %s exists in filesystem and is not owned by any package", relPath)
			}
		} else if !os.IsNotExist(err) {
			return err
		}
	}

	return nil
}

func (i *Installer) installFiles(extractRoot string, pkgInfo *types.PkgInfo, backupRoot string) ([]string, []pathBackup, error) {
	extractRoot = filepath.Clean(extractRoot)
	rootAbs, err := filepath.Abs(filepath.Clean(i.rootDir))
	if err != nil {
		return nil, nil, fmt.Errorf("failed to resolve root directory: %w", err)
	}

	if err := os.MkdirAll(rootAbs, 0o755); err != nil {
		return nil, nil, fmt.Errorf("failed to create root directory %s: %w", rootAbs, err)
	}

	installedPaths := make([]string, 0, len(pkgInfo.Files))
	backups := make([]pathBackup, 0, len(pkgInfo.Files))
	for _, file := range pkgInfo.Files {
		relPath, err := sanitizePackagePath(file)
		if err != nil {
			return installedPaths, backups, err
		}

		src := filepath.Join(extractRoot, relPath)
		dst, err := safeJoinRoot(rootAbs, relPath)
		if err != nil {
			return installedPaths, backups, err
		}

		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return installedPaths, backups, fmt.Errorf("failed to create directory %s: %w", filepath.Dir(dst), err)
		}

		if _, err := os.Lstat(dst); err == nil {
			backupPath := filepath.Join(backupRoot, relPath)
			if err := os.MkdirAll(filepath.Dir(backupPath), 0o755); err != nil {
				return installedPaths, backups, err
			}
			if err := i.clonePath(dst, backupPath); err != nil {
				return installedPaths, backups, fmt.Errorf("failed to backup %s: %w", relPath, err)
			}
			backups = append(backups, pathBackup{originalPath: dst, backupPath: backupPath})
		} else if !os.IsNotExist(err) {
			return installedPaths, backups, err
		}

		if err := i.clonePath(src, dst); err != nil {
			return installedPaths, backups, fmt.Errorf("failed to copy %s: %w", relPath, err)
		}

		installedPaths = append(installedPaths, dst)
	}

	return installedPaths, backups, nil
}

func (i *Installer) rollbackInstall(installedPaths []string, backups []pathBackup) error {
	var errs []string

	seen := make(map[string]struct{}, len(installedPaths))
	for idx := len(installedPaths) - 1; idx >= 0; idx-- {
		path := installedPaths[idx]
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}

		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			errs = append(errs, fmt.Sprintf("remove %s: %v", path, err))
		}
	}

	for idx := len(backups) - 1; idx >= 0; idx-- {
		backup := backups[idx]
		if err := os.MkdirAll(filepath.Dir(backup.originalPath), 0o755); err != nil {
			errs = append(errs, fmt.Sprintf("mkdir %s: %v", filepath.Dir(backup.originalPath), err))
			continue
		}
		if err := i.clonePath(backup.backupPath, backup.originalPath); err != nil {
			errs = append(errs, fmt.Sprintf("restore %s: %v", backup.originalPath, err))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("install rollback failed: %s", strings.Join(errs, "; "))
	}
	return nil
}

func (i *Installer) resolveExtractRoot(tmpDir string, pkgInfo *types.PkgInfo) (string, error) {
	if filesExistUnderRoot(tmpDir, pkgInfo.Files) {
		return tmpDir, nil
	}

	entries, err := os.ReadDir(tmpDir)
	if err != nil {
		return "", err
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		candidate := filepath.Join(tmpDir, entry.Name())
		if filesExistUnderRoot(candidate, pkgInfo.Files) {
			return candidate, nil
		}
	}

	return "", fmt.Errorf("package archive layout does not match file list")
}

func filesExistUnderRoot(root string, files []string) bool {
	for _, file := range files {
		relPath, err := sanitizePackagePath(file)
		if err != nil {
			return false
		}

		if _, err := os.Lstat(filepath.Join(root, relPath)); err != nil {
			return false
		}
	}
	return true
}

// removeFiles removes package files and stores backups for rollback.
func (i *Installer) removeFiles(pkgInfo *types.PkgInfo, backupRoot string) ([]pathBackup, error) {
	rootAbs, err := filepath.Abs(filepath.Clean(i.rootDir))
	if err != nil {
		return nil, fmt.Errorf("failed to resolve root directory: %w", err)
	}

	backups := make([]pathBackup, 0, len(pkgInfo.Files))
	for _, file := range pkgInfo.Files {
		relPath, err := sanitizePackagePath(file)
		if err != nil {
			return backups, err
		}

		path, err := safeJoinRoot(rootAbs, relPath)
		if err != nil {
			return backups, err
		}

		if _, err := os.Lstat(path); os.IsNotExist(err) {
			continue
		} else if err != nil {
			return backups, err
		}

		backupPath := filepath.Join(backupRoot, relPath)
		if err := os.MkdirAll(filepath.Dir(backupPath), 0o755); err != nil {
			return backups, err
		}
		if err := i.clonePath(path, backupPath); err != nil {
			return backups, fmt.Errorf("failed to backup %s: %w", relPath, err)
		}
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return backups, fmt.Errorf("failed to remove %s: %w", relPath, err)
		}

		backups = append(backups, pathBackup{originalPath: path, backupPath: backupPath})
	}

	return backups, nil
}

func (i *Installer) rollbackRemove(backups []pathBackup) error {
	var errs []string

	for idx := len(backups) - 1; idx >= 0; idx-- {
		backup := backups[idx]
		if err := os.MkdirAll(filepath.Dir(backup.originalPath), 0o755); err != nil {
			errs = append(errs, fmt.Sprintf("mkdir %s: %v", filepath.Dir(backup.originalPath), err))
			continue
		}
		if err := i.clonePath(backup.backupPath, backup.originalPath); err != nil {
			errs = append(errs, fmt.Sprintf("restore %s: %v", backup.originalPath, err))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("remove rollback failed: %s", strings.Join(errs, "; "))
	}
	return nil
}

func (i *Installer) registerPackage(pkgInfo *types.PkgInfo) error {
	if err := pkgInfo.Validate(); err != nil {
		return err
	}
	if err := validateSimpleDependencies(pkgInfo.Dependencies); err != nil {
		return err
	}

	if err := os.MkdirAll(i.pkgDB, 0o755); err != nil {
		return fmt.Errorf("failed to create package database: %w", err)
	}

	pkgDir := filepath.Join(i.pkgDB, pkgInfo.Name)
	if err := os.MkdirAll(pkgDir, 0o755); err != nil {
		return fmt.Errorf("failed to create package database directory: %w", err)
	}

	pkgInfoFile := filepath.Join(pkgDir, types.PackageMetadataFile)
	file, err := os.Create(pkgInfoFile)
	if err != nil {
		return err
	}
	defer file.Close()

	return types.WritePkgInfo(file, pkgInfo)
}

func (i *Installer) unregisterPackage(packageName string) error {
	return os.RemoveAll(filepath.Join(i.pkgDB, packageName))
}

func (i *Installer) getPackageInfoNoLock(packageName string) (*types.PkgInfo, error) {
	pkgDir := filepath.Join(i.pkgDB, packageName)
	pkgInfoFile := filepath.Join(pkgDir, types.PackageMetadataFile)

	if _, err := os.Stat(pkgDir); os.IsNotExist(err) {
		return nil, fmt.Errorf("package %s not found in database", packageName)
	}
	if _, err := os.Stat(pkgInfoFile); os.IsNotExist(err) {
		return nil, fmt.Errorf("package metadata file not found for %s", packageName)
	}

	file, err := os.Open(pkgInfoFile)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	pkgInfo, err := types.ReadPkgInfo(file)
	if err != nil {
		return nil, fmt.Errorf("invalid package metadata for %s: %w", packageName, err)
	}

	return pkgInfo, nil
}

func (i *Installer) listInstalledNoLock() ([]string, error) {
	var packages []string

	if _, err := os.Stat(i.pkgDB); os.IsNotExist(err) {
		return packages, nil
	}

	entries, err := os.ReadDir(i.pkgDB)
	if err != nil {
		return nil, fmt.Errorf("failed to read package database: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			packages = append(packages, entry.Name())
		}
	}
	sort.Strings(packages)
	return packages, nil
}

func (i *Installer) installedPkgInfosNoLock() (map[string]*types.PkgInfo, error) {
	installed, err := i.listInstalledNoLock()
	if err != nil {
		return nil, err
	}

	infos := make(map[string]*types.PkgInfo, len(installed))
	for _, pkgName := range installed {
		info, err := i.getPackageInfoNoLock(pkgName)
		if err != nil {
			return nil, err
		}
		infos[pkgName] = info
	}

	return infos, nil
}

func (i *Installer) clonePath(src, dst string) error {
	info, err := os.Lstat(src)
	if err != nil {
		return err
	}

	switch {
	case info.Mode()&os.ModeSymlink != 0:
		target, err := os.Readlink(src)
		if err != nil {
			return err
		}
		if err := removePathIfExists(dst); err != nil {
			return err
		}
		return os.Symlink(target, dst)
	case info.IsDir():
		return os.MkdirAll(dst, info.Mode().Perm())
	case info.Mode().IsRegular():
		sourceFile, err := os.Open(src)
		if err != nil {
			return err
		}
		defer sourceFile.Close()

		destFile, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode().Perm())
		if err != nil {
			return err
		}
		defer destFile.Close()

		if _, err := io.Copy(destFile, sourceFile); err != nil {
			return err
		}
		return nil
	default:
		return fmt.Errorf("unsupported file type: %s", src)
	}
}

func removePathIfExists(path string) error {
	if err := os.RemoveAll(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func validateSimpleDependencies(deps []string) error {
	for _, dep := range deps {
		if depNameFromConstraint(dep) == "" {
			return fmt.Errorf("invalid dependency: %q", dep)
		}
	}
	return nil
}

func depsSatisfied(deps []string, installedInfos map[string]*types.PkgInfo, selfName string) bool {
	for _, dep := range deps {
		depName := depNameFromConstraint(dep)
		if depName == "" {
			return false
		}
		if depName == selfName {
			continue
		}
		if _, ok := installedInfos[depName]; !ok {
			return false
		}
	}
	return true
}

func missingDeps(deps []string, installedInfos map[string]*types.PkgInfo, selfName string) []string {
	missing := make([]string, 0)
	seen := make(map[string]struct{})
	for _, dep := range deps {
		depName := depNameFromConstraint(dep)
		if depName == "" || depName == selfName {
			continue
		}
		if _, ok := installedInfos[depName]; ok {
			continue
		}
		if _, exists := seen[depName]; exists {
			continue
		}
		seen[depName] = struct{}{}
		missing = append(missing, depName)
	}
	sort.Strings(missing)
	return missing
}

func checkDependenciesAgainstInstalled(deps []string, installedInfos map[string]*types.PkgInfo, selfName string) error {
	if depsSatisfied(deps, installedInfos, selfName) {
		return nil
	}
	missing := missingDeps(deps, installedInfos, selfName)
	if len(missing) == 0 {
		return fmt.Errorf("dependency check failed")
	}
	return fmt.Errorf("dependency not installed: %s", strings.Join(missing, ", "))
}

func depNameFromConstraint(dep string) string {
	dep = strings.TrimSpace(dep)
	if dep == "" {
		return ""
	}

	if idx := strings.Index(dep, "|"); idx >= 0 {
		dep = strings.TrimSpace(dep[:idx])
	}

	for idx, r := range dep {
		switch r {
		case '<', '>', '=', ' ', '\t', '\n', '\r':
			dep = strings.TrimSpace(dep[:idx])
			return dep
		}
	}

	return dep
}

func normalizePackageNames(packageNames []string) ([]string, error) {
	if len(packageNames) == 0 {
		return nil, fmt.Errorf("no packages provided")
	}

	normalized := make([]string, 0, len(packageNames))
	seen := make(map[string]struct{}, len(packageNames))
	for _, packageName := range packageNames {
		name := strings.TrimSpace(packageName)
		if name == "" {
			return nil, fmt.Errorf("package name cannot be empty")
		}
		if _, exists := seen[name]; exists {
			return nil, fmt.Errorf("duplicate package in request: %s", name)
		}
		seen[name] = struct{}{}
		normalized = append(normalized, name)
	}

	sort.Strings(normalized)
	return normalized, nil
}

func brokenPackagesAfterRemoval(infos map[string]*types.PkgInfo, removeSet map[string]struct{}) ([]string, error) {
	remaining := make(map[string]*types.PkgInfo, len(infos))
	for name, info := range infos {
		if _, removed := removeSet[name]; removed {
			continue
		}
		remaining[name] = info
	}

	names := make([]string, 0, len(remaining))
	for name := range remaining {
		names = append(names, name)
	}
	sort.Strings(names)

	broken := make([]string, 0)
	for _, pkgName := range names {
		pkgInfo := remaining[pkgName]
		for _, dep := range pkgInfo.Dependencies {
			depName := depNameFromConstraint(dep)
			if depName == "" {
				return nil, fmt.Errorf("invalid dependency in package %s: %q", pkgName, dep)
			}
			if depName == pkgName {
				continue
			}
			if _, ok := remaining[depName]; !ok {
				broken = append(broken, pkgName)
				break
			}
		}
	}

	return broken, nil
}

func listOrphansFromInfos(infos map[string]*types.PkgInfo) []string {
	required := make(map[string]struct{})
	for owner, info := range infos {
		for _, dep := range info.Dependencies {
			depName := depNameFromConstraint(dep)
			if depName == "" || depName == owner {
				continue
			}
			if _, ok := infos[depName]; ok {
				required[depName] = struct{}{}
			}
		}
	}

	orphans := make([]string, 0)
	for name := range infos {
		if _, ok := required[name]; !ok {
			orphans = append(orphans, name)
		}
	}
	sort.Strings(orphans)
	return orphans
}

func sanitizePackagePath(path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("file path cannot be empty")
	}
	if filepath.IsAbs(path) {
		return "", fmt.Errorf("absolute paths are not allowed in package: %s", path)
	}

	clean := filepath.Clean(path)
	if clean == "." {
		return "", fmt.Errorf("invalid file path: %s", path)
	}

	upPrefix := ".." + string(filepath.Separator)
	if clean == ".." || strings.HasPrefix(clean, upPrefix) {
		return "", fmt.Errorf("invalid file path: %s", path)
	}

	return clean, nil
}

func safeJoinRoot(rootAbs, relPath string) (string, error) {
	if filepath.IsAbs(relPath) {
		return "", fmt.Errorf("absolute path not allowed: %s", relPath)
	}

	joined := filepath.Clean(filepath.Join(rootAbs, relPath))
	if rootAbs == string(filepath.Separator) {
		return joined, nil
	}

	rootPrefix := rootAbs + string(filepath.Separator)
	if joined != rootAbs && !strings.HasPrefix(joined, rootPrefix) {
		return "", fmt.Errorf("path escapes root: %s", relPath)
	}

	return joined, nil
}

func (i *Installer) withDBLock(exclusive bool, fn func() error) error {
	if _, err := os.Stat(i.pkgDB); os.IsNotExist(err) {
		if !exclusive {
			return fn()
		}
		if err := os.MkdirAll(i.pkgDB, 0o755); err != nil {
			return fmt.Errorf("failed to create package database: %w", err)
		}
	} else if err != nil {
		return err
	}

	lockPath := filepath.Join(i.pkgDB, ".lock")
	lockFile, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return fmt.Errorf("failed to open lock file: %w", err)
	}
	defer lockFile.Close()

	lockType := syscall.LOCK_SH
	if exclusive {
		lockType = syscall.LOCK_EX
	}
	if err := syscall.Flock(int(lockFile.Fd()), lockType); err != nil {
		return fmt.Errorf("failed to lock package database: %w", err)
	}
	defer syscall.Flock(int(lockFile.Fd()), syscall.LOCK_UN)

	return fn()
}

// GetRootDir returns install root.
func (i *Installer) GetRootDir() string { return i.rootDir }

// GetPkgDB returns package database directory.
func (i *Installer) GetPkgDB() string { return i.pkgDB }

// IsInstalled checks whether package is installed.
func (i *Installer) IsInstalled(packageName string) bool {
	_, err := os.Stat(filepath.Join(i.pkgDB, packageName))
	return err == nil
}

// GetInstalledVersion returns installed version.
func (i *Installer) GetInstalledVersion(packageName string) (string, error) {
	pkgInfo, err := i.GetPackageInfo(packageName)
	if err != nil {
		return "", err
	}
	return pkgInfo.Version, nil
}

// GetInstalledFiles returns installed file list.
func (i *Installer) GetInstalledFiles(packageName string) ([]string, error) {
	pkgInfo, err := i.GetPackageInfo(packageName)
	if err != nil {
		return nil, err
	}
	return pkgInfo.Files, nil
}

// VerifyPackage checks that all package files exist.
func (i *Installer) VerifyPackage(packageName string) error {
	pkgInfo, err := i.GetPackageInfo(packageName)
	if err != nil {
		return err
	}

	rootAbs, err := filepath.Abs(filepath.Clean(i.rootDir))
	if err != nil {
		return fmt.Errorf("failed to resolve root directory: %w", err)
	}

	for _, file := range pkgInfo.Files {
		relPath, err := sanitizePackagePath(file)
		if err != nil {
			return err
		}
		path, err := safeJoinRoot(rootAbs, relPath)
		if err != nil {
			return err
		}
		if _, err := os.Stat(path); err != nil {
			return fmt.Errorf("missing file: %s", relPath)
		}
	}
	return nil
}

// GetPackageSize returns total size of installed files.
func (i *Installer) GetPackageSize(packageName string) (int64, error) {
	pkgInfo, err := i.GetPackageInfo(packageName)
	if err != nil {
		return 0, err
	}

	rootAbs, err := filepath.Abs(filepath.Clean(i.rootDir))
	if err != nil {
		return 0, fmt.Errorf("failed to resolve root directory: %w", err)
	}

	var totalSize int64
	for _, file := range pkgInfo.Files {
		relPath, err := sanitizePackagePath(file)
		if err != nil {
			return 0, err
		}
		path, err := safeJoinRoot(rootAbs, relPath)
		if err != nil {
			return 0, err
		}
		info, err := os.Lstat(path)
		if err != nil {
			return 0, err
		}
		totalSize += info.Size()
	}

	return totalSize, nil
}

type installCandidate struct {
	path string
	info *types.PkgInfo
}

type installTransactionState struct {
	pkgInfo        *types.PkgInfo
	replaced       *removeTransactionState
	installedPaths []string
	newBackups     []pathBackup
	txDir          string
}

type removeTransactionState struct {
	pkgName string
	info    *types.PkgInfo
	backups []pathBackup
}

type pathBackup struct {
	originalPath string
	backupPath   string
}

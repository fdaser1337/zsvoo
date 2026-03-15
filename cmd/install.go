package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"zsvo/pkg/builder"
	"zsvo/pkg/debian"
	"zsvo/pkg/i18n"
	"zsvo/pkg/installer"
	"zsvo/pkg/recipe"
	"zsvo/pkg/ui"

	"github.com/spf13/cobra"
)

var InstallCmd = &cobra.Command{
	Use:   "install <package> [package...]",
	Short: i18n.T("install_cmd"),
	Long:  `Install one or more packages from local files or auto-build from Debian source by package name`,
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		rootDir, _ := cmd.Flags().GetString("root")
		if rootDir == "" {
			rootDir = "/"
		}
		workDir, _ := cmd.Flags().GetString("work-dir")
		if strings.TrimSpace(workDir) == "" {
			workDir = "/tmp/pkg-work"
		}
		autoSource, _ := cmd.Flags().GetBool("auto-source")
		autoBuildDeps, _ := cmd.Flags().GetBool("auto-build-deps")
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		jobs, _ := cmd.Flags().GetInt("jobs")
		if jobs < 1 {
			jobs = 1 // Default to single job to prevent overheating
		}
		cooldown, _ := cmd.Flags().GetDuration("cooldown")

		pui := ui.NewPacmanUI(false)

		if dryRun {
			pui.PrintInfo("DRY RUN MODE")
			pui.PrintInfo(fmt.Sprintf("Root directory: %s", rootDir))
			pui.PrintInfo(fmt.Sprintf("Work directory: %s", workDir))
			pui.PrintInfo(fmt.Sprintf("Auto-source: %t", autoSource))
			pui.PrintInfo(fmt.Sprintf("Auto-build-deps: %t", autoBuildDeps))
			pui.PrintInfo(fmt.Sprintf("Parallel jobs: %d", jobs))
			pui.PrintInfo(fmt.Sprintf("Cooldown: %v", cooldown))
		}

		installTargets := make([]string, 0, len(args))
		i := installer.NewInstaller(rootDir)

		var session *autoBuildSession
		if autoSource {
			session = newAutoBuildSession(workDir, autoBuildDeps, jobs, cooldown)
		}

		for _, target := range args {
			isFile, err := isInstallFileTarget(target)
			if err != nil {
				return err
			}

			if isFile {
				if dryRun {
					pui := ui.NewPacmanUI(false)
					pui.PrintInfo(fmt.Sprintf(i18n.T("would_install_file"), target))
				}
				installTargets = append(installTargets, target)
				continue
			}

			if !autoSource {
				return fmt.Errorf(
					"%s is not a package file; pass a file path or enable --auto-source",
					target,
				)
			}

			if dryRun {
				pui.PrintInfo(fmt.Sprintf(i18n.T("would_auto_build"), target))
				installTargets = append(installTargets, target)
				continue
			}

			builtPackage, err := session.buildPackageWithFallback(target, false, false, []string{})
			if err != nil {
				return err
			}
			installTargets = append(installTargets, builtPackage)
		}

		if len(installTargets) == 1 {
			if dryRun {
				pui.PrintInfo(i18n.T("would_install_one"))
			} else {
				pui.PrintOperation("installing", installTargets[0])
			}
		} else {
			if dryRun {
				pui.PrintInfo(fmt.Sprintf(i18n.T("would_install_many"), len(installTargets)))
			} else {
				pui.PrintOperation("installing", fmt.Sprintf("%d packages", len(installTargets)))
			}
		}

		if dryRun {
			pui.PrintSuccess("DRY RUN COMPLETE")
			pui.PrintInfo(i18n.T("No actual changes were made."))
			return nil
		}

		// Choose installation method - always use auto-resolve
		searchPaths := []string{
			filepath.Join(rootDir, "var", "cache", "packages"),
			filepath.Join(workDir, "packages"),
			"/tmp/pkg-work/packages",
		}

		err := i.InstallWithAutoResolve(installTargets, searchPaths)
		if err != nil {
			return fmt.Errorf("failed to install packages: %w", err)
		}

		pui.PrintSuccess(i18n.T("Package installation completed successfully"))
		return nil
	},
}

func init() {
	InstallCmd.Flags().StringP("root", "r", "/", "Root directory for installation")
	InstallCmd.Flags().StringP("work-dir", "w", "/tmp/pkg-work", "Working directory for source builds")
	InstallCmd.Flags().Bool("auto-source", true, "Auto-build package names from Debian source")
	InstallCmd.Flags().Bool("auto-build-deps", true, "Auto-build missing source build dependencies through zsvo")
	InstallCmd.Flags().Bool("dry-run", false, "Show what would be done without making changes")
	InstallCmd.Flags().IntP("jobs", "j", 1, "Number of parallel build jobs (default: 1 to prevent overheating)")
	InstallCmd.Flags().Duration("cooldown", 5*time.Second, "Cooldown period between package builds (default: 5s)")
}

func isInstallFileTarget(target string) (bool, error) {
	target = strings.TrimSpace(target)
	if target == "" {
		return false, fmt.Errorf("install target cannot be empty")
	}

	// Explicit file-like targets are validated as paths.
	if looksLikeFilePath(target) {
		info, err := os.Stat(target)
		if err != nil {
			return false, fmt.Errorf("package file %s not found: %w", target, err)
		}
		if info.IsDir() {
			return false, fmt.Errorf("package file %s is a directory", target)
		}
		return true, nil
	}

	// If a same-name local file exists, use it as package archive.
	if info, err := os.Stat(target); err == nil {
		if info.IsDir() {
			return false, fmt.Errorf("package file %s is a directory", target)
		}
		return true, nil
	}

	return false, nil
}

func looksLikeFilePath(target string) bool {
	return strings.Contains(target, string(os.PathSeparator)) ||
		strings.HasPrefix(target, ".") ||
		strings.HasSuffix(target, ".pkg.tar.zst") ||
		strings.HasSuffix(target, ".zov")
}

const maxAutoBuildDepth = 15 // Увеличим для сложных цепочек зависимостей
const parallelWorkers = 10   // Уменьшили с 200 до 10 - слишком много потоков
const buildWorkers = 2       // Уменьшили с 4 до 2 - достаточно для CPU intensive задач

type autoBuildSession struct {
	workDir          string
	toolRoot         string
	autoBuildDeps    bool
	resolver         *debian.Resolver
	depResolver      *debian.DependencyResolver
	builder          *builder.Builder
	toolInstaller    *installer.Installer
	builtPackages    map[string]string
	toolDepsReady    map[string]struct{}
	buildingPackages map[string]struct{}
	processing       map[string]struct{} // Track packages being processed to avoid duplicates
	processingMu     sync.RWMutex        // Protect processing map
	jobs             int                 // Number of parallel jobs
	cooldown         time.Duration       // Cooldown between builds
}

// depNode represents a node in the dependency graph
type depNode struct {
	name         string
	deps         []string // dependencies this package needs
	dependents   []string // packages that depend on this one
	buildDepends []string // original Build-Depends from Debian
	level        int      // dependency level (0 = no deps, 1 = depends on level 0, etc.)
	srcInfo      *debian.SourceInfo
	recipe       *recipe.Recipe
	built        bool
	err          error
}

// depGraph manages the dependency tree for parallel building
type depGraph struct {
	nodes map[string]*depNode
	mu    sync.RWMutex
}

// newDepGraph creates a new dependency graph
func newDepGraph() *depGraph {
	return &depGraph{
		nodes: make(map[string]*depNode),
	}
}

// getOrCreateNode gets or creates a node in the graph
func (g *depGraph) getOrCreateNode(name string) *depNode {
	g.mu.Lock()
	defer g.mu.Unlock()

	if node, exists := g.nodes[name]; exists {
		return node
	}

	node := &depNode{
		name:         name,
		deps:         []string{},
		dependents:   []string{},
		buildDepends: []string{},
		level:        -1, // -1 means not calculated yet
	}
	g.nodes[name] = node
	return node
}

// addDependency adds a dependency relationship: pkg depends on dep
func (g *depGraph) addDependency(pkg, dep string) {
	pkgNode := g.getOrCreateNode(pkg)
	depNode := g.getOrCreateNode(dep)

	// Add to deps list if not already there
	found := false
	for _, d := range pkgNode.deps {
		if d == dep {
			found = true
			break
		}
	}
	if !found {
		pkgNode.deps = append(pkgNode.deps, dep)
	}

	// Add to dependents list if not already there
	found = false
	for _, d := range depNode.dependents {
		if d == pkg {
			found = true
			break
		}
	}
	if !found {
		depNode.dependents = append(depNode.dependents, pkg)
	}
}

// calculateLevels calculates dependency levels (topological sort layers)
func (g *depGraph) calculateLevels() {
	g.mu.Lock()
	defer g.mu.Unlock()

	// Reset all levels
	for _, node := range g.nodes {
		node.level = -1
	}

	// Find all nodes with no dependencies (level 0)
	queue := make([]*depNode, 0)
	for _, node := range g.nodes {
		// Count only unbuilt dependencies
		unbuiltDeps := 0
		for _, dep := range node.deps {
			if depNode, exists := g.nodes[dep]; exists && !depNode.built {
				unbuiltDeps++
			}
		}
		if unbuiltDeps == 0 {
			node.level = 0
			queue = append(queue, node)
		}
	}

	// BFS to calculate levels
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		for _, depName := range current.dependents {
			depNode := g.nodes[depName]
			if depNode.level == -1 || depNode.level < current.level+1 {
				depNode.level = current.level + 1
				queue = append(queue, depNode)
			}
		}
	}
}

// getNodesByLevel returns all nodes at a specific level, sorted by name
func (g *depGraph) getNodesByLevel(level int) []*depNode {
	g.mu.RLock()
	defer g.mu.RUnlock()

	result := make([]*depNode, 0)
	for _, node := range g.nodes {
		if node.level == level {
			result = append(result, node)
		}
	}

	// Sort by name for deterministic order
	sort.Slice(result, func(i, j int) bool {
		return result[i].name < result[j].name
	})

	return result
}

// getMaxLevel returns the maximum dependency level
func (g *depGraph) getMaxLevel() int {
	g.mu.RLock()
	defer g.mu.RUnlock()

	max := 0
	for _, node := range g.nodes {
		if node.level > max {
			max = node.level
		}
	}
	return max
}

func newAutoBuildSession(workDir string, autoBuildDeps bool, jobs int, cooldown time.Duration) *autoBuildSession {
	b := builder.NewBuilder(workDir)
	b.SetQuiet(true)

	s := &autoBuildSession{
		workDir:          workDir,
		toolRoot:         filepath.Join(workDir, "bootstrap-root"),
		autoBuildDeps:    autoBuildDeps,
		resolver:         debian.NewResolver(),
		depResolver:      debian.NewDependencyResolver(),
		builder:          b,
		toolInstaller:    installer.NewInstaller(filepath.Join(workDir, "bootstrap-root")),
		builtPackages:    make(map[string]string),
		toolDepsReady:    make(map[string]struct{}),
		buildingPackages: make(map[string]struct{}),
		processing:       make(map[string]struct{}),
		jobs:             jobs,
		cooldown:         cooldown,
	}
	s.refreshBuildEnv()
	return s
}

func (s *autoBuildSession) refreshBuildEnv() {
	basePath := splitPathList(os.Getenv("PATH"))
	binPrefixes := []string{
		filepath.Join(s.toolRoot, "usr", "bin"),
		filepath.Join(s.toolRoot, "bin"),
		filepath.Join(s.toolRoot, "usr", "sbin"),
		filepath.Join(s.toolRoot, "sbin"),
	}
	mergedPath := joinPathListUnique(append(binPrefixes, basePath...))
	s.builder.SetEnvOverride("PATH", mergedPath)

	pkgConfigPath := splitPathList(os.Getenv("PKG_CONFIG_PATH"))
	pkgConfigPrefixes := []string{
		filepath.Join(s.toolRoot, "usr", "lib", "pkgconfig"),
		filepath.Join(s.toolRoot, "usr", "lib64", "pkgconfig"),
		filepath.Join(s.toolRoot, "usr", "share", "pkgconfig"),
		filepath.Join(s.toolRoot, "lib", "pkgconfig"),
		filepath.Join(s.toolRoot, "lib64", "pkgconfig"),
	}
	s.builder.SetEnvOverride("PKG_CONFIG_PATH", joinPathListUnique(append(pkgConfigPrefixes, pkgConfigPath...)))
	cmakePrefixes := []string{
		filepath.Join(s.toolRoot, "usr"),
		filepath.Join(s.toolRoot),
	}
	cmakePrefixes = append(cmakePrefixes, splitPathList(os.Getenv("CMAKE_PREFIX_PATH"))...)
	s.builder.SetEnvOverride("CMAKE_PREFIX_PATH", joinPathListUnique(cmakePrefixes))
}

func (s *autoBuildSession) buildPackageWithFallback(requestName string, asBuildDep bool, allowFailure bool, stack []string) (string, error) {
	requestName = normalizePackageName(requestName)
	if requestName == "" {
		return "", fmt.Errorf("invalid package name")
	}

	if len(stack) >= maxAutoBuildDepth {
		return "", fmt.Errorf("dependency chain is too deep while building %s: %s", requestName, strings.Join(append(stack, requestName), " -> "))
	}
	if _, exists := s.buildingPackages[requestName]; exists {
		return "", fmt.Errorf("dependency cycle detected: %s", strings.Join(append(stack, requestName), " -> "))
	}

	// FAST CACHE CHECK: Check if already built this session
	if builtPath, ok := s.builtPackages[requestName]; ok {
		if asBuildDep {
			if err := s.installBuildDependency(requestName, builtPath); err != nil {
				return "", err
			}
		}
		return builtPath, nil
	}

	// FAST CACHE CHECK: Check local cache directory for existing package
	cachePaths := []string{
		filepath.Join(s.workDir, "packages", requestName, requestName+".pkg.tar.zst"),
		filepath.Join(s.workDir, "packages", requestName+".pkg.tar.zst"),
		filepath.Join(s.workDir, requestName+".pkg.tar.zst"),
	}

	for _, cachePath := range cachePaths {
		if info, err := os.Stat(cachePath); err == nil && !info.IsDir() {
			// Found in cache!
			s.builtPackages[requestName] = cachePath
			if asBuildDep {
				if err := s.installBuildDependency(requestName, cachePath); err != nil {
					return "", err
				}
			}
			fmt.Printf("📦 %s found in cache: %s\n", requestName, cachePath)
			return cachePath, nil
		}
	}

	s.buildingPackages[requestName] = struct{}{}
	defer delete(s.buildingPackages, requestName)

	// Resolve source
	srcInfo, err := s.resolver.ResolveSource(requestName)
	if err != nil {
		if !allowFailure {
			return "", fmt.Errorf("failed to resolve source for %s: %w", requestName, err)
		}
		return "", fmt.Errorf("build failed - install missing dependencies manually")
	}

	rcp := autoRecipeFromDebian(srcInfo)
	normalizedRecipeName := normalizePackageName(rcp.Name)
	if normalizedRecipeName != "" && normalizedRecipeName != requestName {
		// Alias resolved source package name to requested name.
		if _, exists := s.buildingPackages[normalizedRecipeName]; exists {
			return "", fmt.Errorf("dependency cycle detected: %s", strings.Join(append(stack, requestName, normalizedRecipeName), " -> "))
		}
	}

	fmt.Printf("Building %s from %s...\n", rcp.GetPackageName(), srcInfo.DSCURL)

	// Авто-разрешение зависимостей: собираем все Build-Depends параллельно
	if len(srcInfo.BuildDepends) > 0 && s.autoBuildDeps {
		fmt.Printf("🔍 Resolving %d build dependencies...\n", len(srcInfo.BuildDepends))

		// Create dependency graph and collect all dependencies
		graph := newDepGraph()
		for _, dep := range srcInfo.BuildDepends {
			depName := extractPackageNameFromConstraint(dep)
			if depName == "" {
				continue
			}

			// Check if this is a Debian-specific package that should be skipped
			sourcePkg := mapDebianPackageToSource(depName)
			if sourcePkg == "" {
				// Try comprehensive resolver
				sourcePkg, _ = s.depResolver.BinaryToSource(depName)
				if sourcePkg == "" {
					continue
				}
			}

			// Check if already built or available
			if _, built := s.builtPackages[sourcePkg]; built {
				continue
			}
			if toolAlreadyAvailable(sourcePkg) {
				s.toolDepsReady[sourcePkg] = struct{}{}
				continue
			}
			if _, ready := s.toolDepsReady[sourcePkg]; ready {
				continue
			}

			// Collect all dependencies recursively
			if err := s.collectAllDependencies(sourcePkg, graph, []string{requestName}); err != nil {
				// Silent error handling for dependencies
			}
		}

		// Build all collected dependencies in parallel
		if len(graph.nodes) > 0 {
			if err := s.buildDependenciesParallel(graph); err != nil {
				// Silent error handling
			}
		}

		// Refresh environment after building dependencies
		s.refreshBuildEnv()
	}

	// Now build the main package
	var buildErr error
	for attempt := 0; attempt < 2; attempt++ {
		// Clean progress bar - no verbose output
		buildErr = s.builder.Build(rcp)
		if buildErr == nil {
			break
		}
	}

	builtPackage := filepath.Join(rcp.GetPackageDir(s.workDir), rcp.GetPackageFileName())

	if buildErr != nil {
		if !allowFailure {
			return "", fmt.Errorf("failed to auto-build %s: %w", requestName, buildErr)
		}
		return "", fmt.Errorf("build failed for %s (allowFailure set): %w", requestName, buildErr)
	}

	s.builtPackages[rcp.Name] = builtPackage
	return builtPackage, nil
}

func (s *autoBuildSession) installSystemPackage(pkg string, currentBuildingPackage string) error {
	// In LFS environment, we need to build dependencies through zsvo
	// Try to resolve and build the package as a dependency
	fmt.Printf("Building system dependency %s through zsvo...\n", pkg)

	// Use comprehensive Debian resolver to map binary package to source
	sourcePkg, err := s.depResolver.BinaryToSource(pkg)
	if err != nil {
		// Fall back to built-in mapping for common packages
		sourcePkg = mapDebianPackageToSource(pkg)
	}
	if sourcePkg == "" {
		fmt.Printf("Skipping Debian-specific dependency: %s\n", pkg)
		return nil // Skip Debian-specific packages
	}

	// Skip if this is the same package we're currently building (self-dependency)
	if sourcePkg == currentBuildingPackage {
		fmt.Printf("Skipping self-dependency: %s\n", pkg)
		return nil
	}

	// Try to build the dependency
	_, err = s.buildPackageWithFallback(sourcePkg, true, false, []string{})
	if err != nil {
		return fmt.Errorf("failed to build system dependency %s (mapped from %s): %w", sourcePkg, pkg, err)
	}

	// Install it into the toolchain
	packagePath := filepath.Join(s.workDir, "packages", sourcePkg+".pkg.tar.zst")
	if _, err := os.Stat(packagePath); os.IsNotExist(err) {
		// Try other possible package paths
		packagePath = filepath.Join(s.workDir, sourcePkg+".pkg.tar.zst")
	}

	if err := s.installBuildDependency(sourcePkg, packagePath); err != nil {
		return fmt.Errorf("failed to install system dependency %s: %w", sourcePkg, err)
	}

	return nil
}

func (s *autoBuildSession) ensureBuildDependency(dep string, stack []string) error {
	dep = normalizePackageName(dep)
	if dep == "" {
		return fmt.Errorf("invalid build dependency name")
	}

	if _, ready := s.toolDepsReady[dep]; ready {
		return nil
	}
	if toolAlreadyAvailable(dep) {
		s.toolDepsReady[dep] = struct{}{}
		return nil
	}

	fmt.Printf("Installing build dependency %s into %s...\n", dep, s.toolRoot)
	packagePath, err := s.buildPackageWithFallback(dep, true, false, append(stack, dep))
	if err != nil {
		fmt.Printf("Warning: failed to build dependency %s: %v (skipping installation)\n", dep, err)
		return nil // Don't try to install a package that failed to build
	}
	if packagePath == "" {
		fmt.Printf("Warning: build succeeded but no package path returned for %s (skipping installation)\n", dep)
		return nil
	}
	return s.installBuildDependency(dep, packagePath)
}

func (s *autoBuildSession) installBuildDependency(dep, packagePath string) error {
	dep = normalizePackageName(dep)
	if dep == "" {
		return fmt.Errorf("invalid build dependency name")
	}
	if _, ready := s.toolDepsReady[dep]; ready {
		return nil
	}

	fmt.Printf("Installing build dependency %s into %s...\n", dep, s.toolRoot)
	if err := s.toolInstaller.Install(packagePath); err != nil {
		return fmt.Errorf("failed to install build dependency %s: %w", dep, err)
	}
	s.toolDepsReady[dep] = struct{}{}
	s.refreshBuildEnv()
	return nil
}

// collectAllDependencies recursively collects all dependencies into a graph without building
// Now uses parallel workers (5 concurrent) for faster dependency resolution
func (s *autoBuildSession) collectAllDependencies(rootPkg string, graph *depGraph, stack []string) error {
	rootPkg = normalizePackageName(rootPkg)
	if rootPkg == "" {
		return fmt.Errorf("invalid package name")
	}

	if len(stack) >= maxAutoBuildDepth {
		return fmt.Errorf("dependency chain is too deep while building %s", rootPkg)
	}

	// Check for cycles
	for _, s := range stack {
		if s == rootPkg {
			return fmt.Errorf("dependency cycle detected: %s", strings.Join(append(stack, rootPkg), " -> "))
		}
	}

	// Check if already being processed globally at session level
	s.processingMu.Lock()
	if _, exists := s.processing[rootPkg]; exists {
		s.processingMu.Unlock()
		return nil // Already being processed by another goroutine
	}
	// Mark as processing
	s.processing[rootPkg] = struct{}{}
	s.processingMu.Unlock()

	// Check if already in graph with srcInfo
	graph.mu.RLock()
	node, exists := graph.nodes[rootPkg]
	if exists && node.srcInfo != nil {
		graph.mu.RUnlock()
		return nil
	}
	graph.mu.RUnlock()

	// Resolve source - this is the expensive operation
	srcInfo, err := s.resolver.ResolveSource(rootPkg)
	if err != nil {
		return fmt.Errorf("failed to resolve source for %s: %w", rootPkg, err)
	}

	// Create recipe to get proper package name
	rcp := autoRecipeFromDebian(srcInfo)
	pkgName := rcp.Name

	// Update graph with proper locking
	graph.mu.Lock()
	node, exists = graph.nodes[pkgName]
	if !exists {
		node = &depNode{
			name:         pkgName,
			deps:         []string{},
			dependents:   []string{},
			buildDepends: []string{},
			level:        -1,
		}
		graph.nodes[pkgName] = node
	}
	// Only update if not already set (first one wins)
	if node.srcInfo == nil {
		node.srcInfo = srcInfo
		node.recipe = rcp
		node.buildDepends = srcInfo.BuildDepends
	}
	graph.mu.Unlock()

	// If this node was already processed by another goroutine, return early
	if exists && node.srcInfo != nil {
		return nil
	}

	// Collect all dependency names first
	var depsToProcess []string
	for _, dep := range srcInfo.BuildDepends {
		depName := extractPackageNameFromConstraint(dep)
		if depName == "" {
			continue
		}

		// Map to source package
		sourcePkg := mapDebianPackageToSource(depName)
		if sourcePkg == "" {
			sourcePkg, _ = s.depResolver.BinaryToSource(depName)
		}
		if sourcePkg == "" || toolAlreadyAvailable(sourcePkg) {
			continue
		}

		// Add dependency relationship
		graph.addDependency(pkgName, sourcePkg)

		// Check if already in graph with lock
		graph.mu.RLock()
		depNode, depExists := graph.nodes[sourcePkg]
		alreadyProcessing := depExists && depNode.srcInfo != nil
		graph.mu.RUnlock()

		if !alreadyProcessing {
			depsToProcess = append(depsToProcess, sourcePkg)
		}
	}

	// Process dependencies sequentially to avoid race conditions
	for _, dep := range depsToProcess {
		s.collectAllDependencies(dep, graph, append(stack, pkgName))
	}

	return nil
}

// buildDependenciesParallel builds all dependencies in parallel using worker pools
func (s *autoBuildSession) buildDependenciesParallel(graph *depGraph) error {
	// Calculate dependency levels for topological sort
	graph.calculateLevels()

	maxLevel := graph.getMaxLevel()
	if maxLevel == 0 && len(graph.nodes) == 0 {
		return nil
	}

	fmt.Printf("\n📊 Dependency graph: %d packages, %d levels\n", len(graph.nodes), maxLevel+1)

	// Build level by level (respecting dependencies)
	for level := 0; level <= maxLevel; level++ {
		nodes := graph.getNodesByLevel(level)
		if len(nodes) == 0 {
			continue
		}

		// Filter out already built packages
		nodesToBuild := make([]*depNode, 0)
		for _, node := range nodes {
			if _, built := s.builtPackages[node.name]; !built && !toolAlreadyAvailable(node.name) {
				nodesToBuild = append(nodesToBuild, node)
			}
		}

		if len(nodesToBuild) == 0 {
			continue
		}

		fmt.Printf("\n🔨 Level %d: Building %d packages (jobs=%d)...\n", level, len(nodesToBuild), s.jobs)
		for _, n := range nodesToBuild {
			fmt.Printf("   - %s\n", n.name)
		}

		// Build this level - use single worker if jobs=1 for thermal safety
		if s.jobs == 1 {
			// Sequential build with cooldown
			for _, node := range nodesToBuild {
				_, err := s.buildPackageFromNode(node)
				if err != nil {
					node.err = err
					fmt.Printf("   ⚠️ %s failed: %v\n", node.name, err)
				} else {
					node.built = true
					fmt.Printf("   ✓ %s complete\n", node.name)
				}
				// Cooldown to prevent overheating
				if s.cooldown > 0 {
					fmt.Printf("   ⏱️  Cooling down for %v...\n", s.cooldown)
					time.Sleep(s.cooldown)
				}
			}
		} else {
			// Parallel build with limited workers
			var wg sync.WaitGroup
			errChan := make(chan error, len(nodesToBuild))

			// Limit concurrent builds based on jobs setting
			semaphore := make(chan struct{}, s.jobs)

			for _, node := range nodesToBuild {
				wg.Add(1)
				go func(n *depNode) {
					defer wg.Done()

					semaphore <- struct{}{}
					defer func() { <-semaphore }()

					// Build the package
					_, err := s.buildPackageFromNode(n)
					if err != nil {
						n.err = err
						errChan <- fmt.Errorf("%s: %w", n.name, err)
					} else {
						n.built = true
					}
				}(node)
			}

			wg.Wait()
			close(errChan)

			// Check for errors
			errors := make([]error, 0)
			for err := range errChan {
				errors = append(errors, err)
			}

			if len(errors) > 0 {
				fmt.Printf("⚠️  %d packages failed at level %d\n", len(errors), level)
				for _, err := range errors {
					fmt.Printf("   %v\n", err)
				}
			}
		}

		// Refresh environment after each level
		s.refreshBuildEnv()

		// Level cooldown for thermal safety
		if s.cooldown > 0 && level < maxLevel {
			fmt.Printf("⏱️  Level cooldown for %v...\n", s.cooldown)
			time.Sleep(s.cooldown)
		}
	}

	return nil
}

// buildPackageFromNode builds a single package from a dependency node
func (s *autoBuildSession) buildPackageFromNode(node *depNode) (string, error) {
	if node.recipe == nil || node.srcInfo == nil {
		return "", fmt.Errorf("node %s has no recipe or source info", node.name)
	}

	// Check if already built
	if builtPath, ok := s.builtPackages[node.name]; ok {
		return builtPath, nil
	}

	fmt.Printf("Building %s from %s...\n", node.recipe.GetPackageName(), node.srcInfo.DSCURL)

	bar := newProgressUI(node.name)
	s.builder.SetProgressCallback(func(p builder.BuildProgress) {
		bar.update(p.Step, p.Total, fmt.Sprintf("%s: %s", node.name, p.Message))
	})

	buildErr := s.builder.Build(node.recipe)
	s.builder.SetProgressCallback(nil)

	if buildErr != nil {
		bar.finish(false, fmt.Sprintf("%s: failed", node.name))
		return "", fmt.Errorf("failed to build %s: %w", node.name, buildErr)
	}

	bar.finish(true, fmt.Sprintf("%s: complete", node.name))

	builtPackage := filepath.Join(node.recipe.GetPackageDir(s.workDir), node.recipe.GetPackageFileName())
	s.builtPackages[node.name] = builtPackage
	return builtPackage, nil
}

func splitPathList(value string) []string {
	parts := strings.Split(value, string(os.PathListSeparator))
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		out = append(out, part)
	}
	return out
}

func joinPathListUnique(parts []string) string {
	seen := make(map[string]struct{}, len(parts))
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if _, exists := seen[part]; exists {
			continue
		}
		seen[part] = struct{}{}
		out = append(out, part)
	}
	return strings.Join(out, string(os.PathListSeparator))
}

var simplePkgNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9+.-]*[a-z0-9]$`)
var versionNumberPattern = regexp.MustCompile(`[0-9]+\.[0-9]+`)
var missingCommandPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?m)(?:^|[\s:])(?:/bin/)?sh:\s*(?:\d+:\s*)?([a-zA-Z0-9+_.-]+):\s*(?:command not found|not found)\b`),
	regexp.MustCompile(`(?m)\b([a-zA-Z0-9+_.-]+):\s*command not found\b`),
	regexp.MustCompile(`(?m)\b([a-zA-Z0-9+_.-]+):\s*not found\b`),
}

func inferMissingBuildDeps(err error) []string {
	if err == nil {
		return nil
	}

	text := err.Error()
	lowerText := strings.ToLower(text)
	found := make(map[string]struct{})

	for _, pattern := range missingCommandPatterns {
		matches := pattern.FindAllStringSubmatch(text, -1)
		for _, match := range matches {
			if len(match) < 2 {
				continue
			}
			pkg := mapToolToSourcePackage(match[1])
			if pkg == "" {
				continue
			}
			found[pkg] = struct{}{}
		}
	}

	if strings.Contains(lowerText, "failed to find a lua 5.1-compatible interpreter") {
		found["lua5.1"] = struct{}{}
	}
	if strings.Contains(lowerText, "pkg-config") &&
		(strings.Contains(lowerText, "not found") || strings.Contains(lowerText, "could not find")) {
		found["pkgconf"] = struct{}{}
	}
	if strings.Contains(lowerText, "no acceptable c compiler found in $path") ||
		strings.Contains(lowerText, "c compiler cannot create executables") {
		found["gcc"] = struct{}{}
	}

	// Detect missing CMake
	if strings.Contains(lowerText, "cmake") &&
		!strings.Contains(lowerText, "lua 5.1") &&
		(strings.Contains(lowerText, "not found") ||
			strings.Contains(lowerText, "command not found") ||
			strings.Contains(lowerText, "cmake: command not found") ||
			strings.Contains(lowerText, "cmake command not found") ||
			strings.Contains(lowerText, "no cmake") ||
			strings.Contains(lowerText, "could not find cmake")) {
		found["cmake"] = struct{}{}
	}

	// Detect missing Git
	if strings.Contains(lowerText, "git") &&
		(strings.Contains(lowerText, "not found") ||
			strings.Contains(lowerText, "command not found") ||
			strings.Contains(lowerText, "git: command not found") ||
			strings.Contains(lowerText, "git command not found") ||
			strings.Contains(lowerText, "no git") ||
			strings.Contains(lowerText, "could not find git")) {
		found["git"] = struct{}{}
	}

	// Detect missing make - but don't auto-add it as it's usually a build system issue
	// if strings.Contains(lowerText, "make") &&
	// 	(strings.Contains(lowerText, "not found") ||
	// 		strings.Contains(lowerText, "command not found") ||
	// 		strings.Contains(lowerText, "make: command not found") ||
	// 		strings.Contains(lowerText, "make command not found") ||
	// 		strings.Contains(lowerText, "no make") ||
	// 		strings.Contains(lowerText, "could not find make")) {
	// 	found["make"] = struct{}{}
	// }

	// Detect missing source files and CMake errors
	if !strings.Contains(lowerText, "lua 5.1") && (strings.Contains(lowerText, "cannot find source file") ||
		strings.Contains(lowerText, "no sources given to target") ||
		strings.Contains(lowerText, "cmake generate step failed") ||
		strings.Contains(lowerText, "cmake error") ||
		strings.Contains(lowerText, "could not load cache")) {
		found["cmake"] = struct{}{}
	}

	// Detect missing submodules/sources (git issues)
	if strings.Contains(lowerText, "yyjson.c") ||
		strings.Contains(lowerText, "3rdparty") ||
		strings.Contains(lowerText, "submodule") ||
		strings.Contains(lowerText, "git submodule") ||
		strings.Contains(lowerText, "fatal: not a git repository") ||
		strings.Contains(lowerText, "not a git repository") {
		found["git"] = struct{}{}
	}

	if len(found) == 0 {
		return nil
	}

	deps := make([]string, 0, len(found))
	for dep := range found {
		deps = append(deps, dep)
	}
	sort.Strings(deps)
	return deps
}

func extractPackageNameFromConstraint(pkg string) string {
	// Remove version constraints like "pkg (>= 1.0)", "pkg (= 13)", "pkg <!nocheck>"
	re := regexp.MustCompile(`^([a-zA-Z0-9+.-]+)`)
	matches := re.FindStringSubmatch(pkg)
	if len(matches) > 1 {
		return matches[1]
	}
	return pkg
}

func mapDebianPackageToSource(pkg string) string {
	// Extract package name from version constraints like "pkg (>= 1.0)", "pkg (= 13)"
	pkg = extractPackageNameFromConstraint(pkg)

	// Map common Debian build dependencies to source package names
	switch pkg {
	case "cmake":
		return "cmake"
	case "pkg-config":
		return "pkgconf"
	case "build-essential":
		return "" // Skip for LFS - assume basic build tools are available
	case "dpkg-dev":
		return "" // Skip - Debian specific
	case "libc6-dev":
		return "" // Skip - basic C library should be available
	case "debhelper-compat":
		return "" // This is Debian-specific, skip for LFS
	case "debhelper":
		return "" // This is Debian-specific, skip for LFS
	case "gcc-multilib":
		return "" // Skip - Debian specific multilib
	case "directx-headers-dev":
		return "" // Skip - Windows specific
	case "libchafa-dev":
		return "" // Skip - optional dependency
	case "libddcutil-dev":
		return "" // Skip - optional dependency
	case "libdrm-dev":
		return "" // Skip - DRM specific
	case "libegl-dev":
		return "" // Skip - graphics specific
	case "libglx-dev":
		return "" // Skip - X11 specific
	case "libmagickcore-dev":
		return "" // Skip - ImageMagick specific
	case "libnm-dev":
		return "" // Skip - NetworkManager specific
	case "libosmesa6-dev":
		return "" // Skip - Mesa specific
	case "libpulse-dev":
		return "" // Skip - PulseAudio specific
	case "librpm-dev":
		return "" // Skip - RPM specific
	case "libvulkan-dev":
		return "" // Skip - Vulkan specific
	case "libwayland-dev":
		return "" // Skip - Wayland specific
	case "libxcb-randr0-dev":
		return "" // Skip - X11 specific
	case "libxfconf-0-dev":
		return "" // Skip - XFCE specific
	case "libxrandr-dev":
		return "" // Skip - X11 specific
	case "libyyjson-dev":
		return "" // Skip - already handled in build script
	case "ocl-icd-opencl-dev":
		return "" // Skip - OpenCL specific
	case "po4a":
		return "" // Skip - Debian specific
	case "help2man":
		return "" // Skip - optional
	case "dist":
		return "" // Skip - Debian specific
	case "fakeroot":
		return "" // Skip - Debian specific
	case "kyua":
		return "" // Skip - test framework
	case "atf-sh":
		return "" // Skip - test framework
	case "liblua5.1-0-dev":
		return "" // Skip - optional
	case "liblutok-dev":
		return "" // Skip - optional
	case "libsqlite3-dev":
		return "" // Skip - optional
	case "libatf-dev":
		return "" // Skip - test framework
	case "autotools-dev":
		return "" // Skip - Debian specific
	case "libmodule-build-perl":
		return "" // Skip - Perl specific
	case "sq":
		return "" // Skip - optional
	case "sqv":
		return "" // Skip - optional
	case "sqop":
		return "" // Skip - optional
	case "sqopv":
		return "" // Skip - optional
	case "rsop":
		return "" // Skip - optional
	case "rsopv":
		return "" // Skip - optional
	case "gosop":
		return "" // Skip - optional
	case "gpg-sq":
		return "" // Skip - optional
	case "gpgv-sq":
		return "" // Skip - optional
	case "gnupg":
		return "" // Skip - optional
	case "cppcheck":
		return "" // Skip - optional
	case "shellcheck":
		return "" // Skip - optional
	case "aspell":
		return "" // Skip - optional
	case "aspell-en":
		return "" // Skip - optional
	case "codespell":
		return "" // Skip - optional
	case "i18nspector":
		return "" // Skip - optional
	case "libtest-minimumversion-perl":
		return "" // Skip - test specific
	case "libtest-perl-critic-perl":
		return "" // Skip - test specific
	case "libtest-pod-coverage-perl":
		return "" // Skip - test specific
	case "libtest-pod-perl":
		return "" // Skip - test specific
	case "libtest-spelling-perl":
		return "" // Skip - test specific
	case "libtest-strict-perl":
		return "" // Skip - test specific
	case "libtest-synopsis-perl":
		return "" // Skip - test specific
	case "lcov":
		return "" // Skip - test specific
	case "libdevel-cover-perl":
		return "" // Skip - test specific
	case "procps":
		return "procps-ng"
	case "libssl-dev":
		return "openssl"
	case "libjson-c-dev":
		return "json-c"
	case "libdconf-dev":
		return "dconf"
	case "liblua5.1-dev":
		return "lua5.1"
	case "libuv1-dev":
		return "libuv"
	case "libncurses-dev":
		return "ncurses"
	case "libreadline-dev":
		return "readline"
	case "zlib1g-dev":
		return "zlib"
	case "libz-dev":
		return "zlib"
	case "libbz2-dev":
		return "bzip2"
	case "libffi-dev":
		return "libffi"
	case "libxml2-dev":
		return "libxml2"
	case "libcurl4-openssl-dev":
		return "curl"
	case "git":
		return "git"
	case "flex":
		return "flex"
	case "bison":
		return "bison"
	case "autoconf":
		return "autoconf"
	case "automake":
		return "automake"
	case "libtool":
		return "libtool"
	case "m4":
		return "m4"
	case "gettext":
		return "gettext"
	case "pkgconf":
		return "pkgconf"
	case "ninja-build":
		return "ninja"
	case "meson":
		return "meson"
	case "python3-dev":
		return "python3"
	case "libexpat1-dev":
		return "expat"
	case "libpcre3-dev":
		return "pcre3"
	case "libpcre2-dev":
		return "pcre2"
	default:
		// Try to remove -dev suffix and other common patterns
		if strings.HasSuffix(pkg, "-dev") {
			base := strings.TrimSuffix(pkg, "-dev")
			if strings.HasPrefix(base, "lib") {
				// Remove lib prefix for source packages
				base = strings.TrimPrefix(base, "lib")
				// Convert numbers like 5.1, 2.0 etc
				base = versionNumberPattern.ReplaceAllString(base, "")
				if base != "" {
					return base
				}
			}
		}
		return pkg
	}
}

func mapToolToSourcePackage(tool string) string {
	tool = normalizePackageName(tool)
	if tool == "" || allDigits(tool) || len(tool) < 2 {
		return ""
	}

	// Filter out common words that are not package names
	commonWords := map[string]bool{
		"was": true, "the": true, "and": true, "for": true, "are": true,
		"but": true, "not": true, "you": true, "all": true, "can": true,
		"had": true, "her": true, "one": true, "our": true,
		"out": true, "day": true, "get": true, "has": true, "him": true,
		"his": true, "how": true, "man": true, "new": true, "now": true,
		"old": true, "see": true, "two": true, "way": true, "who": true,
		"its": true, "did": true, "yes": true, "she": true, "may": true,
		"why": true, "try": true, "use": true,
	}

	if commonWords[tool] {
		return ""
	}

	switch tool {
	case "sh", "bash", "dash", "zsh":
		return ""
	case "pkg-config":
		return "pkgconf"
	case "ninja":
		return "ninja-build"
	case "python":
		return "python3"
	case "lua":
		return "lua5.1"
	case "autoconf":
		return "autoconf"
	case "automake":
		return "automake"
	case "libtool":
		return "libtool"
	case "xzcat":
		return "xz-utils"
	case "ld":
		return "binutils"
	case "git":
		return "git"
	case "cmake":
		return "cmake"
	case "pkgconf":
		return "pkgconf"
	case "flex":
		return "flex"
	case "bison":
		return "bison"
	case "m4":
		return "m4"
	case "gettext":
		return "gettext"
	case "libssl-dev":
		return "libssl-dev"
	case "libcrypto-dev":
		return "libssl-dev"
	case "zlib1g-dev":
		return "zlib1g-dev"
	case "libpng-dev":
		return "libpng-dev"
	case "libjpeg-dev":
		return "libjpeg-dev"
	case "libxml2-dev":
		return "libxml2-dev"
	case "libcurl4-openssl-dev":
		return "libcurl4-openssl-dev"
	case "libffi-dev":
		return "libffi-dev"
	case "libreadline-dev":
		return "libreadline-dev"
	case "libncurses-dev":
		return "libncurses-dev"
	case "libsqlite3-dev":
		return "libsqlite3-dev"
	case "libbz2-dev":
		return "libbz2-dev"
	case "liblzma-dev":
		return "liblzma-dev"
	case "libiconv":
		return "libiconv"
	case "libintl":
		return "gettext"
	case "libuuid-dev":
		return "libuuid-dev"
	case "libexpat1-dev":
		return "libexpat1-dev"
	case "libpcre3-dev":
		return "libpcre3-dev"
	case "libpcre2-dev":
		return "libpcre2-dev"
	case "libgcrypt20-dev":
		return "libgcrypt20-dev"
	case "libgpg-error-dev":
		return "libgpg-error-dev"
	case "libgnutls28-dev":
		return "libgnutls28-dev"
	}

	if !simplePkgNamePattern.MatchString(tool) {
		return ""
	}
	return tool
}

func allDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, ch := range s {
		if ch < '0' || ch > '9' {
			return false
		}
	}
	return true
}

func normalizePackageName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

var commandHintsByDep = map[string][]string{
	"pkgconf":     {"pkg-config"},
	"ninja-build": {"ninja"},
	"python3":     {"python3", "python"},
	"lua5.1":      {"lua", "lua5.1"},
}

func toolAlreadyAvailable(dep string) bool {
	dep = normalizePackageName(dep)
	if dep == "" {
		return false
	}

	// Check if command exists in PATH
	commands := commandHintsByDep[dep]
	if len(commands) == 0 {
		commands = []string{dep}
	}

	for _, name := range commands {
		if _, err := exec.LookPath(name); err == nil {
			return true
		}
	}
	return false
}

func autoRecipeFromDebian(src *debian.SourceInfo) *recipe.Recipe {
	name := src.SourcePackage
	if name == "" {
		name = src.RequestedPackage
	}
	version := src.UpstreamVersion
	if version == "" {
		version = "0"
	}

	// Use inline install commands instead of external script to avoid hardcoded paths
	// For legacy Makefiles that don't respect DESTDIR, we need to use PREFIX={{pkgdir}}/usr
	installCommands := []string{
		"if [ -f build/cmake_install.cmake ]; then DESTDIR={{pkgdir}} cmake --install build; " +
			"elif [ -f build/meson-private/coredata.dat ]; then DESTDIR={{pkgdir}} meson install -C build; " +
			"elif [ -f Makefile ] || [ -f makefile ] || [ -f GNUmakefile ]; then " +
			"  # Try DESTDIR first, fall back to PREFIX for legacy Makefiles\n" +
			"  if make -n DESTDIR={{pkgdir}} PREFIX=/usr install 2>/dev/null | grep -q 'mkdir.*{{pkgdir}}'; then\n" +
			"    make DESTDIR={{pkgdir}} PREFIX=/usr install;\n" +
			"  else\n" +
			"    # Legacy Makefile - use PREFIX directly\n" +
			"    make PREFIX={{pkgdir}}/usr install;\n" +
			"  fi; " +
			"else mkdir -p {{pkgdir}}/usr/bin {{pkgdir}}/usr/lib {{pkgdir}}/usr/include; fi",
	}

	return &recipe.Recipe{
		Name:        name,
		Version:     version,
		Description: fmt.Sprintf("Auto-generated build recipe from Debian source %s", src.DSCURL),
		Source: recipe.Source{
			DebianDSC: src.DSCURL,
			Sha256:    src.DSCSHA256,
		},
		Build: []string{
			"if [ ! -f configure ] && [ -f autogen.sh ]; then sh ./autogen.sh --no-check ${ZSVO_AUTOGEN_ARGS}; fi; if [ -f configure ]; then ./configure --prefix=/usr ${ZSVO_CONFIGURE_FLAGS}; fi",
			"# Download missing dependencies automatically",
			"if [ ! -f \"src/3rdparty/yyjson/yyjson.c\" ]; then echo \"Downloading yyjson...\" && mkdir -p src/3rdparty/yyjson && curl -L https://raw.githubusercontent.com/ibireme/yyjson/master/src/yyjson.c -o src/3rdparty/yyjson/yyjson.c; fi",
			"if [ ! -f \"src/3rdparty/yyjson/yyjson.h\" ]; then echo \"Downloading yyjson header...\" && mkdir -p src/3rdparty/yyjson && curl -L https://raw.githubusercontent.com/ibireme/yyjson/master/src/yyjson.h -o src/3rdparty/yyjson/yyjson.h; fi",
			"if [ -f CMakeLists.txt ]; then cmake -S . -B build -DCMAKE_BUILD_TYPE=Release -DCMAKE_INSTALL_PREFIX=/usr -DUNIX=1 -DCMAKE_DISABLE_FIND_PACKAGE_Win32=1 ${ZSVO_CMAKE_FLAGS} && cmake --build build -j${jobs} -- ${ZSVO_CMAKE_BUILD_FLAGS}; " +
				"elif [ -f meson.build ]; then meson setup build --prefix=/usr ${ZSVO_MESON_SETUP_ARGS} && meson compile -C build -j${jobs} ${ZSVO_MESON_COMPILE_ARGS}; " +
				"elif [ -f Makefile ] || [ -f makefile ] || [ -f GNUmakefile ]; then make -j${jobs} ${ZSVO_MAKE_FLAGS}; fi",
		},
		Install: installCommands,
	}
}

// progressUI wrapper for compatibility
type progressUI struct {
	statusBar *ui.StatusBar
}

func newProgressUI(pkgName string) *progressUI {
	bar := ui.NewStatusBar(pkgName, 1)
	bar.SetTheme("neon")
	bar.SetSpinner("dots")
	return &progressUI{
		statusBar: bar,
	}
}

func (p *progressUI) update(step, total int, message string) {
	p.statusBar.Update(step, message)
}

func (p *progressUI) finish(ok bool, message string) {
	p.statusBar.Finish(ok, message)
}

var progressFrames = []string{"|", "/", "-", "\\"}

func renderColoredBar(width, filled int) string {
	if width <= 0 {
		return "[]"
	}

	if filled < 0 {
		filled = 0
	}
	if filled > width {
		filled = width
	}

	done := strings.Repeat("=", filled)
	todo := strings.Repeat(".", width-filled)
	return "[" + colorize("32", done) + colorize("2", todo) + "]"
}

func supportsANSIAndTTY() bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	term := strings.TrimSpace(strings.ToLower(os.Getenv("TERM")))
	if term == "" || term == "dumb" {
		return false
	}
	info, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return (info.Mode() & os.ModeCharDevice) != 0
}

func colorize(code, text string) string {
	if text == "" {
		return ""
	}
	return "\x1b[" + code + "m" + text + "\x1b[0m"
}

func formatElapsed(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	totalSeconds := int(d.Seconds())
	minutes := totalSeconds / 60
	seconds := totalSeconds % 60
	return fmt.Sprintf("%02d:%02d", minutes, seconds)
}

func truncateText(s string, max int) string {
	if max <= 3 || len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}

func visibleLen(s string) int {
	// This is enough here because we only inject ANSI codes ourselves.
	n := 0
	inEsc := false
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if inEsc {
			if ch == 'm' {
				inEsc = false
			}
			continue
		}
		if ch == 0x1b {
			inEsc = true
			continue
		}
		n++
	}
	return n
}

func buildFailureHint(pkgName string, err error) string {
	if err == nil {
		return ""
	}

	text := strings.ToLower(err.Error())
	pkgName = strings.TrimSpace(strings.ToLower(pkgName))
	hints := make([]string, 0, 4)

	missingDeps := inferMissingBuildDeps(err)
	if len(missingDeps) > 0 {
		hints = append(
			hints,
			fmt.Sprintf("Hint: missing build dependencies detected: %s.", strings.Join(missingDeps, ", ")),
			"Hint: добавь рецепты для этих пакетов (или алиасы к Debian source), затем повтори `zsvo install`.",
			"Hint: или используйте готовые пакеты: ./zsvo install --repo-url https://github.com/yourname/zsvo-prebuilt neovim",
		)
	}
	if strings.Contains(text, "on systems using dpkg and apt, try: \"apt-get install package\"") {
		hints = append(hints,
			"Hint: upstream configure-script ожидает системные build-зависимости; в zsvo это нужно решать рецептами/автосборкой зависимостей.",
		)
	}

	if pkgName == "neovim" && strings.Contains(text, "lua") {
		hints = append(hints,
			"Hint (neovim): после установки Lua можно зафиксировать интерпретатор: `export ZSVO_CMAKE_FLAGS=\"-DLUA_PRG=$(which lua) -DLUA_GEN_PRG=$(which lua)\"`.",
		)
	}

	if len(hints) == 0 {
		return ""
	}

	return strings.Join(hints, "\n")
}

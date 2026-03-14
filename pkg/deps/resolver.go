package deps

import (
	"fmt"
	"strings"
)

// PackageRepository provides access to installed packages
type PackageRepository interface {
	GetInstalled() (map[string]*PackageInfo, error)
	GetPackage(name string) (*PackageInfo, error)
	SearchPackages(query string) ([]*PackageInfo, error) // New method for heuristic search
}

// PackageFetcher provides ability to download packages
type PackageFetcher interface {
	FetchPackage(pkgName string, version string) error
}

// PackageInfo represents package metadata
type PackageInfo struct {
	Name         string
	Version      string
	Dependencies []string
	Description  string // Optional for better heuristics
}

// DependencyResolver handles dependency resolution
type DependencyResolver struct {
	repo    PackageRepository
	fetcher PackageFetcher
}

// NewDependencyResolver creates a new resolver
func NewDependencyResolver(repo PackageRepository, fetcher PackageFetcher) *DependencyResolver {
	return &DependencyResolver{
		repo:    repo,
		fetcher: fetcher,
	}
}

// ResolveOrder determines installation order using topological sort
func (r *DependencyResolver) ResolveOrder(packages []*PackageInfo) ([]*PackageInfo, error) {
	// Create name to package map
	pkgMap := make(map[string]*PackageInfo)
	for _, pkg := range packages {
		pkgMap[pkg.Name] = pkg
	}

	// Build dependency graph
	indegree := make(map[string]int)
	graph := make(map[string][]string)

	// Initialize
	for _, pkg := range packages {
		indegree[pkg.Name] = 0
		graph[pkg.Name] = []string{}
	}

	// Calculate indegrees
	for _, pkg := range packages {
		reqs, err := ParseRequirements(pkg.Dependencies)
		if err != nil {
			return nil, fmt.Errorf("invalid dependencies in %s: %w", pkg.Name, err)
		}

		for _, req := range reqs {
			for _, alt := range req.Alternatives {
				if depPkg, exists := pkgMap[alt.Name]; exists {
					// Check if version constraint is satisfied
					if alt.MatchesVersion(depPkg.Version) {
						graph[alt.Name] = append(graph[alt.Name], pkg.Name)
						indegree[pkg.Name]++
						break // Only one alternative needed
					}
				}
			}
		}
	}

	// Topological sort using Kahn's algorithm
	queue := make([]string, 0)
	for name, deg := range indegree {
		if deg == 0 {
			queue = append(queue, name)
		}
	}

	result := make([]*PackageInfo, 0)
	for len(queue) > 0 {
		// Get first element
		current := queue[0]
		queue = queue[1:]

		// Add to result
		result = append(result, pkgMap[current])

		// Remove edges
		for _, neighbor := range graph[current] {
			indegree[neighbor]--
			if indegree[neighbor] == 0 {
				queue = append(queue, neighbor)
			}
		}
	}

	if len(result) != len(packages) {
		return nil, fmt.Errorf("dependency cycle detected")
	}

	return result, nil
}

// ResolveDependencies recursively resolves and downloads dependencies
func (r *DependencyResolver) ResolveDependencies(rootPackages []*PackageInfo) ([]*PackageInfo, error) {
	processed := make(map[string]bool)
	result := make([]*PackageInfo, 0)

	for _, rootPkg := range rootPackages {
		pkgs, err := r.resolveRecursive(rootPkg, processed)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve dependencies for %s: %w", rootPkg.Name, err)
		}
		result = append(result, pkgs...)
	}

	// Remove duplicates and sort by dependency order
	return r.deduplicateAndSort(result)
}

// resolveRecursive recursively resolves dependencies with fetching
func (r *DependencyResolver) resolveRecursive(pkg *PackageInfo, processed map[string]bool) ([]*PackageInfo, error) {
	if processed[pkg.Name] {
		return nil, nil // Already processed
	}
	processed[pkg.Name] = true

	result := []*PackageInfo{pkg}

	// Parse dependencies
	reqs, err := ParseRequirements(pkg.Dependencies)
	if err != nil {
		return result, fmt.Errorf("invalid dependencies in %s: %w", pkg.Name, err)
	}

	for i := range reqs {
		depPkg, err := r.resolveRequirement(&reqs[i], processed)
		if err != nil {
			// Log warning but continue - don't fail installation
			fmt.Printf("Warning: failed to resolve dependency %s for %s: %v\n",
				reqs[i].Raw, pkg.Name, err)
			continue
		}

		if depPkg != nil {
			result = append(result, depPkg)
		}
	}

	return result, nil
}

// resolveRequirement resolves a single dependency requirement
func (r *DependencyResolver) resolveRequirement(req *Requirement, processed map[string]bool) (*PackageInfo, error) {
	// First check if already installed
	installed, err := r.repo.GetInstalled()
	if err != nil {
		return nil, fmt.Errorf("failed to get installed packages: %w", err)
	}

	// Check if any alternative is already installed and satisfies constraint
	for i := range req.Alternatives {
		if installedPkg, exists := installed[req.Alternatives[i].Name]; exists {
			if req.Alternatives[i].MatchesVersion(installedPkg.Version) {
				return nil, nil // Already satisfied
			}
		}
	}

	// Try to find and fetch the dependency
	for i := range req.Alternatives {
		depPkg, err := r.findAndFetchDependency(&req.Alternatives[i], processed)
		if err == nil && depPkg != nil {
			return depPkg, nil
		}
		// Try next alternative if this one fails
	}

	// If all alternatives fail, try heuristic search
	heuristicPkg, err := r.findHeuristicDependency(req.Raw)
	if err == nil && heuristicPkg != nil {
		return heuristicPkg, nil
	}

	return nil, fmt.Errorf("no suitable dependency found for %s", req.Raw)
}

// findAndFetchDependency finds and fetches a dependency
func (r *DependencyResolver) findAndFetchDependency(alt *Constraint, processed map[string]bool) (*PackageInfo, error) {
	// Try to get package from repository
	pkg, err := r.repo.GetPackage(alt.Name)
	if err != nil {
		// Package not found, try to fetch it
		if r.fetcher != nil {
			fetchErr := r.fetcher.FetchPackage(alt.Name, "")
			if fetchErr != nil {
				return nil, fmt.Errorf("failed to fetch %s: %w", alt.Name, fetchErr)
			}

			// Try again after fetching
			pkg, err = r.repo.GetPackage(alt.Name)
			if err != nil {
				return nil, fmt.Errorf("package %s not found after fetch", alt.Name)
			}
		} else {
			return nil, fmt.Errorf("package %s not found and no fetcher available", alt.Name)
		}
	}

	// Check version constraint
	if !alt.MatchesVersion(pkg.Version) {
		var opStr string
		switch alt.Op {
		case OpAny:
			opStr = ""
		case OpEqual:
			opStr = "="
		case OpGreater:
			opStr = ">"
		case OpGreaterOrEqual:
			opStr = ">="
		case OpLess:
			opStr = "<"
		case OpLessOrEqual:
			opStr = "<="
		default:
			opStr = ""
		}
		return nil, fmt.Errorf("version mismatch for %s: need %s%s, have %s",
			alt.Name, opStr, alt.Version, pkg.Version)
	}

	// Recursively resolve its dependencies
	if !processed[pkg.Name] {
		_, err = r.resolveRecursive(pkg, processed)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve dependencies of %s: %w", pkg.Name, err)
		}
	}

	return pkg, nil
}

// findHeuristicDependency tries to find dependency using heuristics
func (r *DependencyResolver) findHeuristicDependency(depName string) (*PackageInfo, error) {
	// Try different naming patterns
	patterns := []string{
		depName,                  // Exact match
		"lib" + depName,          // lib prefix
		depName + "-dev",         // dev suffix
		depName + "-devel",       // devel suffix
		depName + "-libs",        // libs suffix
		strings.ToLower(depName), // lowercase
		strings.Title(depName),   // title case
	}

	// Try common library name transformations
	libPatterns := r.generateLibraryPatterns(depName)
	patterns = append(patterns, libPatterns...)

	for _, pattern := range patterns {
		if r.repo == nil {
			continue
		}

		// Search for packages
		pkgs, err := r.repo.SearchPackages(pattern)
		if err != nil {
			continue
		}

		// Return the best match
		if len(pkgs) > 0 {
			return r.findBestMatch(depName, pkgs), nil
		}
	}

	return nil, fmt.Errorf("heuristic search failed for %s", depName)
}

// generateLibraryPatterns generates common library naming patterns
func (r *DependencyResolver) generateLibraryPatterns(name string) []string {
	patterns := []string{}

	// Remove common prefixes/suffixes and add lib prefix
	cleanName := strings.TrimPrefix(name, "lib")
	cleanName = strings.TrimSuffix(cleanName, "-dev")
	cleanName = strings.TrimSuffix(cleanName, "-devel")

	patterns = append(patterns,
		"lib"+cleanName,
		"lib"+cleanName+"-dev",
		"lib"+cleanName+"-devel",
		"lib"+cleanName+"-libs",
	)

	// Common library transformations
	libMappings := map[string]string{
		"ssl":      "openssl",
		"crypto":   "libcrypto",
		"z":        "zlib",
		"png":      "libpng",
		"jpeg":     "libjpeg",
		"tiff":     "libtiff",
		"xml":      "libxml2",
		"xslt":     "libxslt",
		"ffi":      "libffi",
		"readline": "readline",
		"ncurses":  "ncurses",
		"curl":     "libcurl",
		"sqlite":   "sqlite3",
		"db":       "db",
		"bz2":      "libbz2",
		"lzma":     "liblzma",
		"iconv":    "libiconv",
		"intl":     "libintl",
		"uuid":     "libuuid",
		"expat":    "libexpat",
		"pcre":     "libpcre",
		"pcre2":    "libpcre2",
		"gcrypt":   "libgcrypt",
		"gpg":      "libgpg",
		"tls":      "gnutls",
		"ssl2":     "libssl",
	}

	if mapped, exists := libMappings[cleanName]; exists {
		patterns = append(patterns, mapped)
	}

	return patterns
}

// findBestMatch finds the best matching package from search results
func (r *DependencyResolver) findBestMatch(originalName string, candidates []*PackageInfo) *PackageInfo {
	if len(candidates) == 1 {
		return candidates[0]
	}

	bestScore := -1
	var bestPkg *PackageInfo

	for _, pkg := range candidates {
		score := r.calculateMatchScore(originalName, pkg)
		if score > bestScore {
			bestScore = score
			bestPkg = pkg
		}
	}

	return bestPkg
}

// calculateMatchScore calculates how well a package matches the requested dependency
func (r *DependencyResolver) calculateMatchScore(originalName string, pkg *PackageInfo) int {
	score := 0
	pkgName := strings.ToLower(pkg.Name)
	originalLower := strings.ToLower(originalName)

	// Exact match gets highest score
	if pkgName == originalLower {
		score += 100
	}

	// Name contains original
	if strings.Contains(pkgName, originalLower) || strings.Contains(originalLower, pkgName) {
		score += 50
	}

	// Common prefix/suffix matches
	if strings.HasPrefix(pkgName, "lib") && strings.Contains(pkgName, originalLower) {
		score += 30
	}

	if strings.HasSuffix(pkgName, "dev") && strings.Contains(pkgName, originalLower) {
		score += 20
	}

	// Description match (if available)
	if pkg.Description != "" {
		descLower := strings.ToLower(pkg.Description)
		if strings.Contains(descLower, originalLower) {
			score += 10
		}
	}

	// Favor packages with shorter names (likely more specific)
	score += 10 / (len(pkgName) + 1)

	return score
}

// deduplicateAndSort removes duplicates and sorts by dependency order
func (r *DependencyResolver) deduplicateAndSort(packages []*PackageInfo) ([]*PackageInfo, error) {
	// Remove duplicates
	seen := make(map[string]bool)
	unique := make([]*PackageInfo, 0)

	for _, pkg := range packages {
		if !seen[pkg.Name] {
			seen[pkg.Name] = true
			unique = append(unique, pkg)
		}
	}

	// Sort by dependency order
	return r.ResolveOrder(unique)
}

// CheckDependencies verifies if dependencies are satisfied (non-fatal)
func (r *DependencyResolver) CheckDependencies(pkg *PackageInfo) []string {
	installed, err := r.repo.GetInstalled()
	if err != nil {
		return []string{fmt.Sprintf("failed to get installed packages: %v", err)}
	}

	missing := r.findMissingDependencies(pkg, installed)
	return missing
}

func (r *DependencyResolver) findMissingDependencies(pkg *PackageInfo, installed map[string]*PackageInfo) []string {
	var missing []string

	reqs, err := ParseRequirements(pkg.Dependencies)
	if err != nil {
		return []string{err.Error()}
	}

	for _, req := range reqs {
		satisfied := false
		for _, alt := range req.Alternatives {
			if installedPkg, exists := installed[alt.Name]; exists {
				if alt.MatchesVersion(installedPkg.Version) {
					satisfied = true
					break
				}
			}
		}
		if !satisfied {
			missing = append(missing, req.Raw)
		}
	}

	return missing
}

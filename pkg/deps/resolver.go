package deps

import (
	"fmt"
	"strings"
)

// PackageRepository provides access to installed packages
type PackageRepository interface {
	GetInstalled() (map[string]*PackageInfo, error)
	GetPackage(name string) (*PackageInfo, error)
}

// PackageInfo represents package metadata
type PackageInfo struct {
	Name         string
	Version      string
	Dependencies []string
}

// DependencyResolver handles dependency resolution
type DependencyResolver struct {
	repo PackageRepository
}

// NewDependencyResolver creates a new resolver
func NewDependencyResolver(repo PackageRepository) *DependencyResolver {
	return &DependencyResolver{repo: repo}
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

// CheckDependencies verifies if dependencies are satisfied
func (r *DependencyResolver) CheckDependencies(pkg *PackageInfo) error {
	installed, err := r.repo.GetInstalled()
	if err != nil {
		return fmt.Errorf("failed to get installed packages: %w", err)
	}

	missing := r.findMissingDependencies(pkg, installed)
	if len(missing) > 0 {
		return fmt.Errorf("missing dependencies: %s", strings.Join(missing, ", "))
	}

	return nil
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

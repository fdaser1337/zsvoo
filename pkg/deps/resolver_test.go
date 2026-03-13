package deps

import (
	"fmt"
	"testing"
)

// MockPackageRepository for testing
type MockPackageRepository struct {
	packages map[string]*PackageInfo
}

func NewMockPackageRepository(packages map[string]*PackageInfo) *MockPackageRepository {
	return &MockPackageRepository{packages: packages}
}

func (m *MockPackageRepository) GetInstalled() (map[string]*PackageInfo, error) {
	result := make(map[string]*PackageInfo)
	for k, v := range m.packages {
		result[k] = v
	}
	return result, nil
}

func (m *MockPackageRepository) GetPackage(name string) (*PackageInfo, error) {
	pkg, exists := m.packages[name]
	if !exists {
		return nil, fmt.Errorf("package %s not found", name)
	}
	return pkg, nil
}

func TestDependencyResolver_CheckDependencies(t *testing.T) {
	t.Parallel()

	installed := map[string]*PackageInfo{
		"glibc": {
			Name:         "glibc",
			Version:      "2.31",
			Dependencies: nil,
		},
		"zlib": {
			Name:         "zlib",
			Version:      "1.2.11",
			Dependencies: []string{"glibc"},
		},
	}

	repo := NewMockPackageRepository(installed)
	resolver := NewDependencyResolver(repo)

	cases := []struct {
		name string
		pkg  *PackageInfo
		want bool
	}{
		{
			name: "all dependencies satisfied",
			pkg: &PackageInfo{
				Name:         "testpkg",
				Version:      "1.0",
				Dependencies: []string{"glibc", "zlib"},
			},
			want: true,
		},
		{
			name: "missing dependency",
			pkg: &PackageInfo{
				Name:         "testpkg2",
				Version:      "1.0",
				Dependencies: []string{"nonexistent"},
			},
			want: false,
		},
		{
			name: "version constraint not satisfied",
			pkg: &PackageInfo{
				Name:         "testpkg3",
				Version:      "1.0",
				Dependencies: []string{"glibc>=2.40"},
			},
			want: false,
		},
		{
			name: "alternative dependencies",
			pkg: &PackageInfo{
				Name:         "testpkg4",
				Version:      "1.0",
				Dependencies: []string{"lua5.1 | luajit"},
			},
			want: false, // neither alternative is installed
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := resolver.CheckDependencies(tc.pkg)
			got := err == nil
			if got != tc.want {
				t.Errorf("CheckDependencies() = %v, want %v", got, tc.want)
				if err != nil {
					t.Logf("Error: %v", err)
				}
			}
		})
	}
}

func TestDependencyResolver_ResolveOrder(t *testing.T) {
	t.Parallel()

	packages := []*PackageInfo{
		{
			Name:         "a",
			Version:      "1.0",
			Dependencies: []string{"b"},
		},
		{
			Name:         "b",
			Version:      "1.0",
			Dependencies: []string{"c"},
		},
		{
			Name:         "c",
			Version:      "1.0",
			Dependencies: nil,
		},
	}

	installed := map[string]*PackageInfo{}
	repo := NewMockPackageRepository(installed)
	resolver := NewDependencyResolver(repo)

	order, err := resolver.ResolveOrder(packages)
	if err != nil {
		t.Fatalf("ResolveOrder() error = %v", err)
	}

	// Check that dependencies come before dependents
	pos := make(map[string]int)
	for i, pkg := range order {
		pos[pkg.Name] = i
	}

	if pos["c"] > pos["b"] || pos["b"] > pos["a"] {
		t.Errorf("Dependencies not in correct order: got %v", order)
	}
}

func TestDependencyResolver_FindMissingDependencies(t *testing.T) {
	t.Parallel()

	installed := map[string]*PackageInfo{
		"pkg1": {Name: "pkg1", Version: "1.0"},
		"pkg2": {Name: "pkg2", Version: "2.0"},
	}

	repo := NewMockPackageRepository(installed)
	resolver := NewDependencyResolver(repo)

	pkg := &PackageInfo{
		Name:         "testpkg",
		Version:      "1.0",
		Dependencies: []string{"pkg1", "pkg3", "pkg4>=1.5"},
	}

	missing := resolver.findMissingDependencies(pkg, installed)
	
	expected := []string{"pkg3", "pkg4>=1.5"}
	if len(missing) != len(expected) {
		t.Fatalf("Expected %d missing dependencies, got %d", len(expected), len(missing))
	}

	for i, dep := range expected {
		if i >= len(missing) || missing[i] != dep {
			t.Errorf("Expected missing dependency %s, got %s", dep, missing[i])
		}
	}
}

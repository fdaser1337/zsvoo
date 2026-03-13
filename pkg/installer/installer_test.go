package installer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"zsvo/pkg/packager"
	"zsvo/pkg/recipe"
	"zsvo/pkg/types"
)

func buildTestPackage(t *testing.T, workDir string, r *recipe.Recipe, files map[string][]byte) string {
	t.Helper()

	stagingDir := r.GetStagingDir(workDir)
	for relPath, content := range files {
		path := filepath.Join(stagingDir, relPath)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("MkdirAll(%s) error = %v", path, err)
		}
		if err := os.WriteFile(path, content, 0o755); err != nil {
			t.Fatalf("WriteFile(%s) error = %v", path, err)
		}
	}

	p := packager.NewPackager(workDir)
	if err := p.Package(r); err != nil {
		t.Fatalf("Package(%s) error = %v", r.GetPackageName(), err)
	}
	return p.GetPackageFile(r)
}

func TestInstallPackageCopiesFilesAndSymlinks(t *testing.T) {
	t.Parallel()

	workDir := t.TempDir()
	rootDir := t.TempDir()
	r := &recipe.Recipe{Name: "demo", Version: "1.0.0", Build: []string{"true"}, Install: []string{"true"}}

	stagingDir := r.GetStagingDir(workDir)
	if err := os.MkdirAll(filepath.Join(stagingDir, "usr", "bin"), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(stagingDir, "usr", "bin", "demo"), []byte("demo\n"), 0o755); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if err := os.Symlink("demo", filepath.Join(stagingDir, "usr", "bin", "demo-link")); err != nil {
		t.Fatalf("Symlink() error = %v", err)
	}

	p := packager.NewPackager(workDir)
	if err := p.Package(r); err != nil {
		t.Fatalf("Package() error = %v", err)
	}

	ins := NewInstaller(rootDir)
	if err := ins.Install(p.GetPackageFile(r)); err != nil {
		t.Fatalf("Install() error = %v", err)
	}

	if _, err := os.Stat(filepath.Join(rootDir, "usr", "bin", "demo")); err != nil {
		t.Fatalf("installed file missing: %v", err)
	}
	if target, err := os.Readlink(filepath.Join(rootDir, "usr", "bin", "demo-link")); err != nil {
		t.Fatalf("Readlink() error = %v", err)
	} else if target != "demo" {
		t.Fatalf("unexpected symlink target: %s", target)
	}
}

func TestInstallManyOrdersByDependencies(t *testing.T) {
	t.Parallel()

	workDir := t.TempDir()
	rootDir := t.TempDir()
	ins := NewInstaller(rootDir)

	libPkg := buildTestPackage(t, workDir, &recipe.Recipe{
		Name:    "libfoo",
		Version: "1.0.0",
		Build:   []string{"true"},
		Install: []string{"true"},
	}, map[string][]byte{"usr/lib/libfoo.so": []byte("lib\n")})

	appPkg := buildTestPackage(t, workDir, &recipe.Recipe{
		Name:    "app",
		Version: "1.0.0",
		Build:   []string{"true"},
		Install: []string{"true"},
		Deps:    []string{"libfoo"},
	}, map[string][]byte{"usr/bin/app": []byte("app\n")})

	// app передан первым, installer должен поставить libfoo раньше.
	if err := ins.InstallMany([]string{appPkg, libPkg}); err != nil {
		t.Fatalf("InstallMany() error = %v", err)
	}

	if !ins.IsInstalled("libfoo") || !ins.IsInstalled("app") {
		t.Fatalf("expected libfoo and app to be installed")
	}
}

func TestInstallManyRollsBackOnFailure(t *testing.T) {
	t.Parallel()

	workDir := t.TempDir()
	rootDir := t.TempDir()
	ins := NewInstaller(rootDir)

	libPkg := buildTestPackage(t, workDir, &recipe.Recipe{
		Name:    "libfoo",
		Version: "1.0.0",
		Build:   []string{"true"},
		Install: []string{"true"},
	}, map[string][]byte{"usr/lib/libfoo.so": []byte("lib\n")})

	appPkg := buildTestPackage(t, workDir, &recipe.Recipe{
		Name:    "app",
		Version: "1.0.0",
		Build:   []string{"true"},
		Install: []string{"true"},
		Deps:    []string{"libfoo"},
	}, map[string][]byte{"usr/bin/app": []byte("app\n")})

	brokenPkg := buildTestPackage(t, workDir, &recipe.Recipe{
		Name:    "broken",
		Version: "1.0.0",
		Build:   []string{"true"},
		Install: []string{"true"},
		Deps:    []string{"missing"},
	}, map[string][]byte{"usr/bin/broken": []byte("broken\n")})

	err := ins.InstallMany([]string{appPkg, libPkg, brokenPkg})
	if err == nil {
		t.Fatalf("expected InstallMany() to fail")
	}

	if ins.IsInstalled("libfoo") || ins.IsInstalled("app") || ins.IsInstalled("broken") {
		t.Fatalf("expected transaction rollback")
	}
}

func TestInstallManyRespectsVersionConstraints(t *testing.T) {
	t.Parallel()

	workDir := t.TempDir()
	rootDir := t.TempDir()
	ins := NewInstaller(rootDir)

	libPkg := buildTestPackage(t, workDir, &recipe.Recipe{
		Name:    "libfoo",
		Version: "2.1.0",
		Build:   []string{"true"},
		Install: []string{"true"},
	}, map[string][]byte{"usr/lib/libfoo.so": []byte("lib2\n")})

	appPkg := buildTestPackage(t, workDir, &recipe.Recipe{
		Name:    "app",
		Version: "1.0.0",
		Build:   []string{"true"},
		Install: []string{"true"},
		Deps:    []string{"libfoo>=2.0.0"},
	}, map[string][]byte{"usr/bin/app": []byte("app\n")})

	if err := ins.InstallMany([]string{appPkg, libPkg}); err != nil {
		t.Fatalf("InstallMany() error = %v", err)
	}
	if !ins.IsInstalled("libfoo") || !ins.IsInstalled("app") {
		t.Fatalf("expected libfoo and app to be installed")
	}
}

func TestInstallManyFailsOnUnsatisfiedVersionConstraint(t *testing.T) {
	t.Parallel()

	workDir := t.TempDir()
	rootDir := t.TempDir()
	ins := NewInstaller(rootDir)

	libOld := buildTestPackage(t, workDir, &recipe.Recipe{
		Name:    "libfoo",
		Version: "1.0.0",
		Build:   []string{"true"},
		Install: []string{"true"},
	}, map[string][]byte{"usr/lib/libfoo.so": []byte("lib1\n")})
	if err := ins.Install(libOld); err != nil {
		t.Fatalf("Install(libOld) error = %v", err)
	}

	appPkg := buildTestPackage(t, workDir, &recipe.Recipe{
		Name:    "app",
		Version: "1.0.0",
		Build:   []string{"true"},
		Install: []string{"true"},
		Deps:    []string{"libfoo>=2.0.0"},
	}, map[string][]byte{"usr/bin/app": []byte("app\n")})

	err := ins.Install(appPkg)
	if err == nil {
		t.Fatalf("expected install error for unsatisfied version constraint")
	}
	if !strings.Contains(err.Error(), "libfoo>=2.0.0") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestInstallManySupportsAlternativeDependencies(t *testing.T) {
	t.Parallel()

	workDir := t.TempDir()
	rootDir := t.TempDir()
	ins := NewInstaller(rootDir)

	libBarPkg := buildTestPackage(t, workDir, &recipe.Recipe{
		Name:    "libbar",
		Version: "1.5.0",
		Build:   []string{"true"},
		Install: []string{"true"},
	}, map[string][]byte{"usr/lib/libbar.so": []byte("bar\n")})

	appPkg := buildTestPackage(t, workDir, &recipe.Recipe{
		Name:    "app",
		Version: "1.0.0",
		Build:   []string{"true"},
		Install: []string{"true"},
		Deps:    []string{"libfoo | libbar>=1.2.0"},
	}, map[string][]byte{"usr/bin/app": []byte("app\n")})

	if err := ins.InstallMany([]string{appPkg, libBarPkg}); err != nil {
		t.Fatalf("InstallMany() error = %v", err)
	}
	if !ins.IsInstalled("libbar") || !ins.IsInstalled("app") {
		t.Fatalf("expected libbar and app to be installed")
	}
}

func TestInstallManyDetectsDependencyCycle(t *testing.T) {
	t.Parallel()

	workDir := t.TempDir()
	rootDir := t.TempDir()
	ins := NewInstaller(rootDir)

	pkgA := buildTestPackage(t, workDir, &recipe.Recipe{
		Name:    "a",
		Version: "1.0.0",
		Build:   []string{"true"},
		Install: []string{"true"},
		Deps:    []string{"b>=1.0"},
	}, map[string][]byte{"usr/bin/a": []byte("a\n")})

	pkgB := buildTestPackage(t, workDir, &recipe.Recipe{
		Name:    "b",
		Version: "1.0.0",
		Build:   []string{"true"},
		Install: []string{"true"},
		Deps:    []string{"a>=1.0"},
	}, map[string][]byte{"usr/bin/b": []byte("b\n")})

	err := ins.InstallMany([]string{pkgA, pkgB})
	if err == nil {
		t.Fatalf("expected cycle error")
	}
	if !strings.Contains(err.Error(), "dependency cycle") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestInstallRespectsRootFlag(t *testing.T) {
	t.Parallel()

	workDir := t.TempDir()
	rootDir := t.TempDir()
	ins := NewInstaller(rootDir)

	pkgFile := buildTestPackage(t, workDir, &recipe.Recipe{
		Name:    "rooted",
		Version: "1.0.0",
		Build:   []string{"true"},
		Install: []string{"true"},
	}, map[string][]byte{"usr/bin/rooted": []byte("ok\n")})

	if err := ins.Install(pkgFile); err != nil {
		t.Fatalf("Install() error = %v", err)
	}

	if _, err := os.Stat(filepath.Join(rootDir, "usr", "bin", "rooted")); err != nil {
		t.Fatalf("expected file in custom root: %v", err)
	}
}

func TestRemoveManyWithoutCascadeFails(t *testing.T) {
	t.Parallel()

	rootDir := t.TempDir()
	ins := NewInstaller(rootDir)

	lib := &types.PkgInfo{Name: "libfoo", Version: "1.0.0", Files: []string{"usr/lib/libfoo.so"}}
	app := &types.PkgInfo{Name: "app", Version: "1.0.0", Dependencies: []string{"libfoo"}, Files: []string{"usr/bin/app"}}

	if err := ins.registerPackage(lib); err != nil {
		t.Fatalf("registerPackage(lib) error = %v", err)
	}
	if err := ins.registerPackage(app); err != nil {
		t.Fatalf("registerPackage(app) error = %v", err)
	}

	_, err := ins.RemoveMany([]string{"libfoo"}, RemoveOptions{})
	if err == nil {
		t.Fatalf("expected RemoveMany() to fail")
	}
	if !strings.Contains(err.Error(), "required by") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRemoveManyCascade(t *testing.T) {
	t.Parallel()

	rootDir := t.TempDir()
	ins := NewInstaller(rootDir)

	lib := &types.PkgInfo{Name: "libfoo", Version: "1.0.0", Files: []string{"usr/lib/libfoo.so"}}
	app := &types.PkgInfo{Name: "app", Version: "1.0.0", Dependencies: []string{"libfoo"}, Files: []string{"usr/bin/app"}}

	if err := ins.registerPackage(lib); err != nil {
		t.Fatalf("registerPackage(lib) error = %v", err)
	}
	if err := ins.registerPackage(app); err != nil {
		t.Fatalf("registerPackage(app) error = %v", err)
	}

	removed, err := ins.RemoveMany([]string{"libfoo"}, RemoveOptions{Cascade: true})
	if err != nil {
		t.Fatalf("RemoveMany(cascade) error = %v", err)
	}

	if len(removed) != 2 {
		t.Fatalf("expected two removed packages, got %d", len(removed))
	}
	if ins.IsInstalled("libfoo") || ins.IsInstalled("app") {
		t.Fatalf("expected packages to be removed")
	}
}

func TestRemoveManyAllowsAlternativeDependencyProvider(t *testing.T) {
	t.Parallel()

	rootDir := t.TempDir()
	ins := NewInstaller(rootDir)

	libFoo := &types.PkgInfo{Name: "libfoo", Version: "1.0.0", Files: []string{"usr/lib/libfoo.so"}}
	libBar := &types.PkgInfo{Name: "libbar", Version: "1.0.0", Files: []string{"usr/lib/libbar.so"}}
	app := &types.PkgInfo{
		Name:         "app",
		Version:      "1.0.0",
		Dependencies: []string{"libfoo | libbar"},
		Files:        []string{"usr/bin/app"},
	}

	if err := ins.registerPackage(libFoo); err != nil {
		t.Fatalf("registerPackage(libFoo) error = %v", err)
	}
	if err := ins.registerPackage(libBar); err != nil {
		t.Fatalf("registerPackage(libBar) error = %v", err)
	}
	if err := ins.registerPackage(app); err != nil {
		t.Fatalf("registerPackage(app) error = %v", err)
	}

	removed, err := ins.RemoveMany([]string{"libfoo"}, RemoveOptions{})
	if err != nil {
		t.Fatalf("RemoveMany() error = %v", err)
	}
	if len(removed) != 1 || removed[0] != "libfoo" {
		t.Fatalf("unexpected removed list: %#v", removed)
	}
	if !ins.IsInstalled("app") {
		t.Fatalf("app should remain installed because libbar satisfies dependency")
	}
}

func TestListOrphans(t *testing.T) {
	t.Parallel()

	rootDir := t.TempDir()
	ins := NewInstaller(rootDir)

	lib := &types.PkgInfo{Name: "libfoo", Version: "1.0.0"}
	app := &types.PkgInfo{Name: "app", Version: "1.0.0", Dependencies: []string{"libfoo"}}
	tool := &types.PkgInfo{Name: "tool", Version: "1.0.0"}

	if err := ins.registerPackage(lib); err != nil {
		t.Fatalf("registerPackage(lib) error = %v", err)
	}
	if err := ins.registerPackage(app); err != nil {
		t.Fatalf("registerPackage(app) error = %v", err)
	}
	if err := ins.registerPackage(tool); err != nil {
		t.Fatalf("registerPackage(tool) error = %v", err)
	}

	orphans, err := ins.ListOrphans()
	if err != nil {
		t.Fatalf("ListOrphans() error = %v", err)
	}

	set := make(map[string]struct{}, len(orphans))
	for _, name := range orphans {
		set[name] = struct{}{}
	}

	if _, ok := set["app"]; !ok {
		t.Fatalf("expected app to be orphan")
	}
	if _, ok := set["tool"]; !ok {
		t.Fatalf("expected tool to be orphan")
	}
	if _, ok := set["libfoo"]; ok {
		t.Fatalf("did not expect libfoo to be orphan")
	}
}

func TestListOrphansAlternativeDependencyMarksChosenProvider(t *testing.T) {
	t.Parallel()

	rootDir := t.TempDir()
	ins := NewInstaller(rootDir)

	libFoo := &types.PkgInfo{Name: "libfoo", Version: "1.0.0"}
	libBar := &types.PkgInfo{Name: "libbar", Version: "1.0.0"}
	app := &types.PkgInfo{Name: "app", Version: "1.0.0", Dependencies: []string{"libfoo | libbar"}}

	if err := ins.registerPackage(libFoo); err != nil {
		t.Fatalf("registerPackage(libFoo) error = %v", err)
	}
	if err := ins.registerPackage(libBar); err != nil {
		t.Fatalf("registerPackage(libBar) error = %v", err)
	}
	if err := ins.registerPackage(app); err != nil {
		t.Fatalf("registerPackage(app) error = %v", err)
	}

	orphans, err := ins.ListOrphans()
	if err != nil {
		t.Fatalf("ListOrphans() error = %v", err)
	}

	set := make(map[string]struct{}, len(orphans))
	for _, name := range orphans {
		set[name] = struct{}{}
	}

	if _, ok := set["app"]; !ok {
		t.Fatalf("expected app to be orphan")
	}
	if _, ok := set["libbar"]; !ok {
		t.Fatalf("expected libbar to be orphan when libfoo is first satisfiable provider")
	}
	if _, ok := set["libfoo"]; ok {
		t.Fatalf("did not expect libfoo to be orphan")
	}
}

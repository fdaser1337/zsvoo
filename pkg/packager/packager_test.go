package packager

import (
	"os"
	"path/filepath"
	"testing"

	"zsvo/pkg/recipe"
)

func TestPackageAndReadPkgInfo(t *testing.T) {
	t.Parallel()

	workDir := t.TempDir()
	stagingDir := filepath.Join(workDir, "staging", "demo-1.0.0")
	if err := os.MkdirAll(filepath.Join(stagingDir, "usr", "bin"), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	if err := os.WriteFile(filepath.Join(stagingDir, "usr", "bin", "demo"), []byte("demo\n"), 0o755); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	r := &recipe.Recipe{
		Name:    "demo",
		Version: "1.0.0",
		Deps:    []string{"glibc", "zlib"},
	}

	p := NewPackager(workDir)
	if err := p.Package(r); err != nil {
		t.Fatalf("Package() error = %v", err)
	}

	archive := p.GetPackageFile(r)
	info, err := p.ReadPkgInfo(archive)
	if err != nil {
		t.Fatalf("ReadPkgInfo() error = %v", err)
	}

	if len(info.Dependencies) != 2 || info.Dependencies[0] != "glibc" || info.Dependencies[1] != "zlib" {
		t.Fatalf("dependencies mismatch: %#v", info.Dependencies)
	}
	if len(info.Files) != 1 || info.Files[0] != "usr/bin/demo" {
		t.Fatalf("files mismatch: %#v", info.Files)
	}

	contents, err := p.ListPackageContents(archive)
	if err != nil {
		t.Fatalf("ListPackageContents() error = %v", err)
	}

	hasPkgInfo := false
	hasBinary := false
	for _, entry := range contents {
		if entry == ".zsvo.yml" {
			hasPkgInfo = true
		}
		if entry == "usr/bin/demo" {
			hasBinary = true
		}
	}

	if !hasPkgInfo || !hasBinary {
		t.Fatalf("archive contents missing expected files: %#v", contents)
	}
}

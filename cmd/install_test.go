package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"zsvo/pkg/debian"
)

func TestIsInstallFileTarget_File(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	pkgPath := filepath.Join(tmpDir, "demo.pkg.tar.zst")
	if err := os.WriteFile(pkgPath, []byte("x"), 0o644); err != nil {
		t.Fatalf("failed to write temp package: %v", err)
	}

	isFile, err := isInstallFileTarget(pkgPath)
	if err != nil {
		t.Fatalf("isInstallFileTarget() error = %v", err)
	}
	if !isFile {
		t.Fatalf("expected file target")
	}
}

func TestIsInstallFileTarget_Name(t *testing.T) {
	t.Parallel()

	isFile, err := isInstallFileTarget("neofetch")
	if err != nil {
		t.Fatalf("isInstallFileTarget() error = %v", err)
	}
	if isFile {
		t.Fatalf("expected package-name target")
	}
}

func TestIsInstallFileTarget_MissingPath(t *testing.T) {
	t.Parallel()

	_, err := isInstallFileTarget("./missing.pkg.tar.zst")
	if err == nil {
		t.Fatalf("expected error for missing file path")
	}
}

func TestAutoRecipeFromDebian(t *testing.T) {
	t.Parallel()

	r := autoRecipeFromDebian(&debian.SourceInfo{
		RequestedPackage: "neofetch",
		SourcePackage:    "neofetch",
		DSCURL:           "https://deb.debian.org/debian/pool/main/n/neofetch/neofetch_7.1.0-4.dsc",
		DSCSHA256:        "deadbeef",
		DebianVersion:    "7.1.0-4",
		UpstreamVersion:  "7.1.0",
	})

	if r.Name != "neofetch" || r.Version != "7.1.0" {
		t.Fatalf("unexpected recipe identity: %s %s", r.Name, r.Version)
	}
	if r.Source.DebianDSC == "" {
		t.Fatalf("expected debian dsc source")
	}
	if len(r.Build) == 0 || len(r.Install) == 0 {
		t.Fatalf("expected auto recipe build/install commands")
	}
}

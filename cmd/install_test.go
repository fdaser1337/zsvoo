package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
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
	if !strings.Contains(r.Build[0], "autogen.sh --no-check") {
		t.Fatalf("expected auto recipe to support autogen fallback, got: %s", r.Build[0])
	}
}

func TestBuildFailureHintForLua(t *testing.T) {
	t.Parallel()

	msg := buildFailureHint("neovim", errors.New("Failed to find a Lua 5.1-compatible interpreter"))
	if !strings.Contains(strings.ToLower(msg), "lua") {
		t.Fatalf("expected lua hint, got: %q", msg)
	}
}

func TestInferMissingBuildDeps_FromCommandNotFound(t *testing.T) {
	t.Parallel()

	err := errors.New("sh: cmake: command not found\n/bin/sh: 1: pkg-config: not found")
	got := inferMissingBuildDeps(err)
	want := []string{"cmake", "pkgconf"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("inferMissingBuildDeps() mismatch:\nwant: %#v\ngot:  %#v", want, got)
	}
}

func TestInferMissingBuildDeps_FromLuaError(t *testing.T) {
	t.Parallel()

	err := errors.New("CMake Error: Failed to find a Lua 5.1-compatible interpreter")
	got := inferMissingBuildDeps(err)
	want := []string{"lua5.1"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("inferMissingBuildDeps() mismatch:\nwant: %#v\ngot:  %#v", want, got)
	}
}

func TestMapToolToSourcePackage(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"pkg-config": "pkgconf",
		"ninja":      "ninja-build",
		"python":     "python3",
		"lua":        "lua5.1",
		"cmake":      "cmake",
		"bash":       "",
		"1":          "",
		"":           "",
	}

	for input, want := range cases {
		if got := mapToolToSourcePackage(input); got != want {
			t.Fatalf("mapToolToSourcePackage(%q): want %q, got %q", input, want, got)
		}
	}
}

package recipe

import (
	"strings"
	"testing"
)

func TestParseRecipeFromReaderYAML(t *testing.T) {
	t.Parallel()

	input := `
name: bash
version: "5.2"
description: GNU shell

source:
  url: https://ftp.gnu.org/gnu/bash/bash-5.2.tar.gz
  sha256: deadbeef

build:
  - ./configure --prefix=/usr
  - make -j$(nproc)

install:
  - make DESTDIR={{pkgdir}} install

deps:
  - glibc
  - readline
`

	r, err := ParseRecipeFromReader(strings.NewReader(input))
	if err != nil {
		t.Fatalf("ParseRecipeFromReader() error = %v", err)
	}

	if r.Name != "bash" || r.Version != "5.2" {
		t.Fatalf("unexpected recipe identity: %s %s", r.Name, r.Version)
	}
	if len(r.Build) != 2 || len(r.Install) != 1 || len(r.Deps) != 2 {
		t.Fatalf("unexpected parsed sections: build=%d install=%d deps=%d", len(r.Build), len(r.Install), len(r.Deps))
	}
}

func TestParseRecipeAcceptsComplexDependencySyntax(t *testing.T) {
	t.Parallel()

	input := `
name: app
version: "1.0.0"
source:
  url: https://example.org/app.tar.gz
  sha256: deadbeef
build:
  - make
install:
  - make DESTDIR={{pkgdir}} install
deps:
  - glibc>=2.39
  - liblua5.1-0 | libluajit-5.1-2
`

	r, err := ParseRecipeFromReader(strings.NewReader(input))
	if err != nil {
		t.Fatalf("expected complex dependencies to parse, got error: %v", err)
	}
	if len(r.Deps) != 2 {
		t.Fatalf("expected 2 deps, got %d", len(r.Deps))
	}
}

func TestParseRecipeFromReaderDebianDSC(t *testing.T) {
	t.Parallel()

	input := `
name: coreutils
version: "9.5"
source:
  debian_dsc: https://deb.debian.org/debian/pool/main/c/coreutils/coreutils_9.5-1.dsc
build:
  - ./configure --prefix=/usr
  - make -j$(nproc)
install:
  - make DESTDIR={{pkgdir}} install
deps:
  - glibc
`

	r, err := ParseRecipeFromReader(strings.NewReader(input))
	if err != nil {
		t.Fatalf("ParseRecipeFromReader() error = %v", err)
	}

	if r.Source.DebianDSC == "" {
		t.Fatalf("expected source.debian_dsc to be parsed")
	}
	if r.Source.Sha256 != "" {
		t.Fatalf("expected empty source.sha256 for debian dsc flow")
	}
}

func TestParseRecipeRejectsURLAndDebianDSCTogether(t *testing.T) {
	t.Parallel()

	input := `
name: demo
version: "1.0"
source:
  url: https://example.org/demo.tar.gz
  sha256: deadbeef
  debian_dsc: https://deb.debian.org/debian/pool/main/d/demo/demo_1.0-1.dsc
build:
  - make
install:
  - make DESTDIR={{pkgdir}} install
`

	_, err := ParseRecipeFromReader(strings.NewReader(input))
	if err == nil {
		t.Fatalf("expected parse error for mutually exclusive source fields")
	}
}

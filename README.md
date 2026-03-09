# Package Manager

A minimal source-based package manager for custom Linux distributions, inspired by Arch Linux and Gentoo.

## Project Structure

```
zsvo/
├── go.mod                    # Go module definition
├── go.sum                    # Go dependencies
├── main.go                   # CLI entry point
├── cmd/                      # CLI commands
│   ├── build.go             # pkg build command
│   ├── install.go           # pkg install command
│   ├── remove.go            # pkg remove command
│   ├── list.go              # pkg list command
│   └── info.go              # pkg info command
├── pkg/                      # Core packages
│   ├── recipe/              # Recipe parsing and validation
│   │   └── recipe.go
│   ├── fetcher/             # Source downloading and extraction
│   │   └── fetcher.go
│   ├── builder/             # Package building
│   │   └── builder.go
│   ├── packager/            # Package creation
│   │   └── packager.go
│   └── installer/           # Package installation/removal
│       └── installer.go
└── recipes/                 # Example recipes
    └── zlib.toml
```

## Modules

### Recipe Module (`pkg/recipe/`)
- Parses TOML recipe files
- Validates recipe structure
- Provides package naming utilities
- Defines `Recipe`, `Source`, `Build`, and `PkgInfo` structs

### Fetcher Module (`pkg/fetcher/`)
- Downloads sources with checksum verification
- Extracts various archive formats
- Handles patch application
- Implements caching for downloaded sources
- Supports concurrent downloads

### Builder Module (`pkg/builder/`)
- Manages build process from recipe
- Sets up build environment
- Executes build commands
- Handles source directory management
- Provides build information and file listing

### Packager Module (`pkg/packager/`)
- Creates compressed package archives
- Generates package metadata (.pkginfo)
- Extracts packages for installation
- Verifies package integrity
- Lists package contents

### Installer Module (`pkg/installer/`)
- Installs packages to root filesystem
- Manages package database
- Handles dependency checking
- Removes packages and cleans up
- Provides package information queries

## Usage

### Building a Package

```bash
# Build from recipe
pkg build recipes/zlib.toml

# Build with custom work directory
pkg build -w /tmp/build zlib.toml
```

### Installing a Package

```bash
# Install package
pkg install /path/to/package.pkg.tar.zst

# Install to custom root
pkg install -r /mnt/root package.pkg.tar.zst
```

### Managing Packages

```bash
# List installed packages
pkg list

# Show package information
pkg info zlib

# Remove package
pkg remove zlib
```

## Recipe Format

Recipes are written in TOML format:

```toml
name = "package-name"
version = "1.0.0"
description = "Package description"

[source]
url = "https://example.com/package-1.0.0.tar.gz"
sha256 = "sha256-hash-of-source"
patches = ["patch1.diff", "patch2.diff"]

[build]
commands = [
    "./configure --prefix=/usr",
    "make -j$(nproc)",
    "make DESTDIR={{pkgdir}} install"
]
env = [
    "CFLAGS=-O2"
]

[dependencies]
# List of package dependencies
```

## Package Format

Packages are created as `name-version.pkg.tar.zst` archives containing:
- Installed files
- `.pkginfo` metadata file

## Package Database

Installed packages are tracked in `/var/lib/pkgdb/<pkgname>/` with:
- `.pkginfo` file containing package metadata
- File lists and installation information

## Dependencies

- `github.com/BurntSushi/toml` - TOML parsing
- `github.com/mholt/archiver/v4` - Archive handling
- `github.com/spf13/cobra` - CLI framework
- `github.com/spf13/viper` - Configuration management
- `golang.org/x/sync` - Concurrent operations

## Philosophy

This package manager follows the simplicity philosophy of Arch Linux tools:
- Minimal dependencies
- Clear separation of concerns
- Production-ready code
- Source-based package management
- Simple, understandable implementation
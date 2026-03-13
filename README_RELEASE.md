# ZSVO Package Manager

**Source-based package management for custom Linux distributions with automatic dependency resolution**

## 🚀 Quick Start

### Installation

#### Linux/macOS/BSD
```bash
# Download latest release
curl -L https://github.com/yourusername/zsvo/releases/latest/download/zsvo-latest-linux-amd64.tar.gz | tar xz

# Install
cd zsvo-*-linux-amd64
sudo ./install.sh
```

#### Docker
```bash
docker run --rm zsvo/zsvo:latest --help
```

#### Build from source
```bash
git clone https://github.com/yourusername/zsvo
cd zsvo
make build-all
sudo cp dist/linux/amd64/zsvo /usr/local/bin/
```

## 📦 Features

### 🔥 Automatic Dependency Resolution
```bash
# Install package with all dependencies automatically
zsvo install --auto-resolve-deps myapp.pkg.tar.zst
```

- **No external tools needed** - everything built into ZSVO
- **Recursive resolution** - finds dependencies of dependencies
- **Multiple sources** - searches in package cache directories
- **Version constraints** - respects version requirements
- **Alternatives support** - handles `pkgA | pkgB` style dependencies

### 🏗️ Source-based Building
```bash
# Build from Debian source
zsvo install --auto-source --auto-build-deps nginx

# Build from custom recipe
zsvo build myapp.recipe.yaml
zsvo install myapp.pkg.tar.zst
```

### 🔄 Transactional Operations
```bash
# Install multiple packages atomically
zsvo install pkg1.pkg.tar.zst pkg2.pkg.tar.zst pkg3.pkg.tar.zst

# Automatic rollback on failure
zsvo install --auto-resolve-deps complex-app.pkg.tar.zst
```

## 🎯 Supported Platforms

| Platform | Architectures | Status |
|----------|---------------|--------|
| Linux | x86_64, ARM64, i386, ARM | ✅ Full Support |
| macOS | Intel, Apple Silicon | ✅ Full Support |
| Windows | x86_64, i386 | ✅ Full Support |
| FreeBSD | x86_64, ARM64 | ✅ Full Support |
| OpenBSD | x86_64, ARM64 | ✅ Full Support |

## 📋 Commands

### Package Management
```bash
# Install packages
zsvo install package.pkg.tar.zst
zsvo install --auto-resolve-deps app.pkg.tar.zst

# Remove packages
zsvo remove package_name
zsvo remove --cascade package_name  # Remove with dependents

# Upgrade packages
zsvo upgrade package.pkg.tar.zst

# List installed packages
zsvo list
zsvo list --orphans  # Show unused packages
```

### Building
```bash
# Build from recipe
zsvo build recipe.yaml

# Build from Debian source
zsvo install --auto-source package_name

# Build with custom options
zsvo build --work-dir /tmp/build recipe.yaml
```

### Information
```bash
# Package information
zsvo info package_name

# System doctor
zsvo doctor

# Search packages
zsvo search package_name
```

## 🔧 Configuration

### Environment Variables
```bash
export ZSVO_ROOT="/opt/zsvo"          # Installation root
export ZSVO_WORK="/tmp/zsvo-work"      # Build directory
export ZSVO_CACHE="/var/cache/zsvo"    # Package cache
```

### Configuration File
```yaml
# ~/.config/zsvo/config.yaml
root_dir: "/"
work_dir: "/tmp/pkg-work"
cache_dirs:
  - "/var/cache/packages"
  - "~/.local/share/zsvo/packages"

auto_source: true
auto_build_deps: true
auto_resolve_deps: true
```

## 📁 Project Structure

```
zsvo/
├── cmd/                    # CLI commands
├── pkg/                    # Core packages
│   ├── deps/              # Dependency resolution
│   ├── installer/         # Package installation
│   ├── builder/           # Package building
│   ├── fetcher/           # Source fetching
│   ├── packager/          # Package creation
│   ├── security/          # Security utilities
│   └── errors/            # Error handling
├── scripts/               # Build and utility scripts
├── examples/              # Example recipes
├── docs/                  # Documentation
├── dist/                  # Built binaries (gitignored)
└── release/               # Release packages (gitignored)
```

## 🛠️ Development

### Building from Source
```bash
# Install dependencies
make dev-setup

# Run tests
make test
make test-coverage

# Build for current platform
make build

# Build for all platforms
make build-all

# Create release
make release
```

### Using Build Scripts
```bash
# Cross-platform build
./scripts/build.sh all

# Specific platform
./scripts/build.sh build linux amd64

# Create release packages
./scripts/release.sh create
```

### Docker Development
```bash
# Build Docker image
./scripts/docker.sh build linux/amd64

# Multi-architecture build
./scripts/docker.sh multi

# Run container
./scripts/docker.sh run zsvo:latest --help
```

## 🔐 Security

- **Path traversal protection** - Validates all file paths
- **Checksum verification** - Ensures package integrity
- **Type-safe dependency resolution** - Prevents injection attacks
- **Sandboxed builds** - Isolated build environment
- **Cryptographic verification** - Optional GPG signature support

## 📚 Examples

### Basic Package Installation
```bash
# Install single package
zsvo install nginx.pkg.tar.zst

# Install with automatic dependency resolution
zsvo install --auto-resolve-deps webapp.pkg.tar.zst

# Install multiple packages
zsvo install app1.pkg.tar.zst app2.pkg.tar.zst
```

### Building from Source
```bash
# Build from Debian source
zsvo install --auto-source --auto-build-deps postgresql

# Build with custom recipe
cat > myapp.recipe.yaml << EOF
name: myapp
version: "1.0.0"
description: "My custom application"

source:
  url: "https://github.com/user/myapp/archive/v1.0.0.tar.gz"
  sha256: "abc123..."

build:
  - make
  - make install DESTDIR=\${DESTDIR}

deps:
  - "libssl >= 1.1"
  - "zlib"
EOF

zsvo build myapp.recipe.yaml
zsvo install myapp.pkg.tar.zst
```

### Dependency Management
```bash
# Check what depends on a package
zsvo info --reverse-deps openssl

# Find orphaned packages
zsvo list --orphans

# Remove package and its dependents
zsvo remove --cascade old-package
```

## 🤝 Contributing

1. Fork the repository
2. Create a feature branch (`git checkout -b feature/amazing-feature`)
3. Commit your changes (`git commit -m 'Add amazing feature'`)
4. Push to the branch (`git push origin feature/amazing-feature`)
5. Open a Pull Request

### Development Guidelines
- Follow Go best practices
- Add tests for new features
- Update documentation
- Ensure all tests pass (`make test`)
- Run linter (`make lint`)

## 📄 License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

## 🙏 Acknowledgments

- Inspired by traditional package managers like `apt`, `yum`, `pacman`
- Dependency resolution algorithms from SAT solvers
- Build system concepts from `pkgsrc` and `ports`

## 📞 Support

- 📖 [Documentation](DEPENDENCY_RESOLUTION.md)
- 🐛 [Issue Tracker](https://github.com/yourusername/zsvo/issues)
- 💬 [Discussions](https://github.com/yourusername/zsvo/discussions)

---

**ZSVO** - Zero-Snafu Version Operations  
Making package management simple, secure, and automatic.

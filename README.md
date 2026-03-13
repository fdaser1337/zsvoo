# ZSVO Package Manager

`zsvo` — source-based package manager.

Pipeline:

`recipe -> download source -> extract -> build -> install into DESTDIR -> create package -> install package`

## Commands

```bash
# Build package from recipe
zsvo build recipes/zlib.yaml

# Install package built by zsvo
zsvo install /path/to/name-version.pkg.tar.zst

# Auto-build from Debian source and install
zsvo install neofetch

# Install to custom root
zsvo install --root /mnt/root /path/to/name-version.pkg.tar.zst

# Remove package(s)
zsvo remove bash
zsvo remove -c libfoo   # cascade
zsvo remove -n bash     # dry-run

# List installed / orphan packages
zsvo list
zsvo list --orphans

# Show package metadata
zsvo info bash
```

## Recipe Format (YAML)

```yaml
name: bash
version: "5.2"

description: GNU Bourne Again SHell

source:
  url: https://ftp.gnu.org/gnu/bash/bash-5.2.tar.gz
  sha256: examplehash

build:
  - ./configure --prefix=/usr
  - make -j$(nproc)

install:
  - make DESTDIR={{pkgdir}} install

deps:
  - glibc
  - readline
```

Debian upstream source mode:

```yaml
name: bash
version: "5.2"

source:
  debian_dsc: https://deb.debian.org/debian/pool/main/b/bash/bash_5.2.37-2.dsc
  # optional: sha256 of the .dsc file itself
  # sha256: <dsc-sha256>
```

Notes:
- `deps` are simple package names (no version solver yet).
- `{{pkgdir}}` (and `${pkgdir}`) points to staging DESTDIR.
- If `source.debian_dsc` (or `.dsc` URL) is used, zsvo downloads only `*.orig*.tar.*` archives from that source package and ignores Debian patch archives (`debian.tar.*` / `diff.gz`).
- `zsvo install <name>` auto-resolves Debian source over HTTP from Debian `Sources` indexes, builds package in `--work-dir` and installs it.
- Package metadata is stored in `.zsvo.yml` (not `.PKGINFO`).

## Architecture

- `pkg/recipe` — YAML recipe parser.
- `pkg/fetcher` — download + checksum + extract + patch apply.
- `pkg/builder` — build/install pipeline in isolated workdir.
- `pkg/packager` — create/read `name-version.pkg.tar.zst` from staging.
- `pkg/installer` — install/remove packages into `--root` with file safety checks and rollback.

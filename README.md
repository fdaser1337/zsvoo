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
  - "zlib>=1.2.13"
  - "lua5.1 | luajit"
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
- `deps` support full constraints: `name`, `name>=1.2`, `name (<= 2.0)`, and alternatives via `|`.
- Installer resolves dependencies with version checks and transaction ordering (topological sort), including mixed installed + transaction packages.
- `{{pkgdir}}` (and `${pkgdir}`) points to staging DESTDIR.
- If `source.debian_dsc` (or `.dsc` URL) is used, zsvo downloads only `*.orig*.tar.*` archives from that source package and ignores Debian patch archives (`debian.tar.*` / `diff.gz`).
- `zsvo install <name>` auto-resolves Debian source over HTTP from Debian `Sources` indexes, builds package in `--work-dir` and installs it.
- Auto-build in `install <name>` is quiet by default and shows a colored live progress bar (spinner + percent + elapsed time) instead of raw compiler output; full command output tail is shown on failure.
- If build fails due to missing tools (`cmake`, `meson`, `pkg-config`, `lua` etc.), `zsvo install` auto-detects them, builds missing dependencies from Debian source with zsvo itself, installs them into `--work-dir/bootstrap-root`, and retries the build (`--auto-build-deps=false` to disable).
- Auto-build can be tuned with env flags: `ZSVO_AUTOGEN_ARGS`, `ZSVO_CONFIGURE_FLAGS`, `ZSVO_CMAKE_FLAGS`, `ZSVO_CMAKE_BUILD_FLAGS`, `ZSVO_MESON_SETUP_ARGS`, `ZSVO_MESON_COMPILE_ARGS`, `ZSVO_MESON_INSTALL_ARGS`, `ZSVO_MAKE_FLAGS`, `ZSVO_MAKE_INSTALL_FLAGS`.
- Package metadata is stored in `.zsvo.yml` (not `.PKGINFO`).

## Architecture

- `pkg/recipe` — YAML recipe parser.
- `pkg/fetcher` — download + checksum + extract + patch apply.
- `pkg/builder` — build/install pipeline in isolated workdir.
- `pkg/packager` — create/read `name-version.pkg.tar.zst` from staging.
- `pkg/installer` — install/remove packages into `--root` with file safety checks and rollback.





короче бляяяя для тестов
go build -o zsvo .
./zsvo install fastfetch --work-dir /tmp/zsvo-work --root /tmp/zsvo-root
find /tmp/zsvo-work/packages -name '*.pkg.tar.zst'
find /tmp/zsvo-root/var/lib/pkgdb -maxdepth 3 -type f
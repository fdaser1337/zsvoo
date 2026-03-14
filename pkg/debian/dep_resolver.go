package debian

import (
	"bufio"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/ulikunitz/xz"
)

// DependencyResolver provides comprehensive Debian dependency resolution
type DependencyResolver struct {
	resolver   *Resolver
	cache      *dependencyCache
	httpClient *http.Client
	mirrors    []string
	suites     []string
	components []string
	arch       string
}

// dependencyCache caches resolved dependencies
type dependencyCache struct {
	mu     sync.RWMutex
	deps   map[string]*PackageDeps
	srcMap map[string]string // binary pkg name -> source pkg name
}

// PackageDeps represents Debian package dependencies
type PackageDeps struct {
	Package      string
	Source       string
	Version      string
	Depends      []string
	BuildDepends []string
	Suggests     []string
	Recommends   []string
	Conflicts    []string
	Provides     []string
}

// NewDependencyResolver creates a comprehensive Debian dependency resolver
func NewDependencyResolver() *DependencyResolver {
	return &DependencyResolver{
		resolver: NewResolver(),
		cache: &dependencyCache{
			deps:   make(map[string]*PackageDeps),
			srcMap: make(map[string]string),
		},
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		mirrors: []string{
			"https://deb.debian.org/debian",
			"https://ftp.debian.org/debian",
			"https://mirrors.kernel.org/debian",
		},
		suites:     []string{"sid", "unstable", "testing", "stable"},
		components: []string{"main", "contrib", "non-free"},
		arch:       "amd64",
	}
}

// ResolveDependencies resolves all dependencies for a Debian binary package
func (r *DependencyResolver) ResolveDependencies(pkgName string) (*PackageDeps, error) {
	// Check cache first
	if cached := r.cache.get(pkgName); cached != nil {
		return cached, nil
	}

	// Try to find in Sources files (for build-deps) or Packages files (for runtime deps)
	deps, err := r.resolveFromDebian(pkgName)
	if err != nil {
		return nil, err
	}

	// Cache result
	r.cache.set(pkgName, deps)

	return deps, nil
}

// BinaryToSource maps a Debian binary package name to its source package name
func (r *DependencyResolver) BinaryToSource(binaryPkg string) (string, error) {
	// Check cache
	if src := r.cache.getSource(binaryPkg); src != "" {
		return src, nil
	}

	// Try built-in mappings first (fast path)
	if src := mapBinaryToSourceFast(binaryPkg); src != "" {
		r.cache.setSource(binaryPkg, src)
		return src, nil
	}

	// Look up in Debian Sources
	srcInfo, err := r.resolver.ResolveSource(binaryPkg)
	if err != nil {
		return "", fmt.Errorf("cannot resolve source for %s: %w", binaryPkg, err)
	}

	r.cache.setSource(binaryPkg, srcInfo.SourcePackage)
	return srcInfo.SourcePackage, nil
}

// ResolveDependencyChain resolves full dependency chain for a package
func (r *DependencyResolver) ResolveDependencyChain(rootPkg string, includeBuildDeps bool) ([]string, error) {
	visited := make(map[string]bool)
	result := make([]string, 0)
	queue := []string{rootPkg}

	for len(queue) > 0 {
		pkg := queue[0]
		queue = queue[1:]

		if visited[pkg] {
			continue
		}
		visited[pkg] = true

		// Get package dependencies
		deps, err := r.ResolveDependencies(pkg)
		if err != nil {
			// Try to resolve as source package
			srcPkg, srcErr := r.BinaryToSource(pkg)
			if srcErr != nil {
				continue // Skip unresolved
			}
			if !visited[srcPkg] {
				result = append(result, srcPkg)
			}
			continue
		}

		result = append(result, pkg)

		// Add runtime dependencies
		for _, dep := range deps.Depends {
			depName := r.extractDepName(dep)
			if !visited[depName] {
				queue = append(queue, depName)
			}
		}

		// Add build dependencies if requested
		if includeBuildDeps {
			for _, dep := range deps.BuildDepends {
				depName := r.extractDepName(dep)
				if !visited[depName] {
					queue = append(queue, depName)
				}
			}
		}
	}

	return result, nil
}

// extractDepName extracts package name from dependency string (e.g., "pkg (>= 1.0)" -> "pkg")
func (r *DependencyResolver) extractDepName(dep string) string {
	// Remove version constraints
	re := regexp.MustCompile(`^([a-zA-Z0-9+.-]+)`)
	matches := re.FindStringSubmatch(dep)
	if len(matches) > 1 {
		return matches[1]
	}
	return dep
}

// resolveFromDebian attempts to resolve dependencies from Debian repository
func (r *DependencyResolver) resolveFromDebian(pkgName string) (*PackageDeps, error) {
	// Try to find binary package info from Packages files
	for _, mirror := range r.mirrors {
		for _, suite := range r.suites {
			for _, component := range r.components {
				deps, err := r.fetchBinaryPackageInfo(mirror, suite, component, pkgName)
				if err == nil && deps != nil {
					return deps, nil
				}
			}
		}
	}

	return nil, fmt.Errorf("package %s not found in Debian repositories", pkgName)
}

// fetchBinaryPackageInfo fetches package info from Packages.xz
func (r *DependencyResolver) fetchBinaryPackageInfo(mirror, suite, component, pkgName string) (*PackageDeps, error) {
	url := fmt.Sprintf("%s/dists/%s/%c%s/binary-%s/Packages.xz",
		mirror, suite, component[0], component[1:], r.arch)

	resp, err := r.httpClient.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	// Decompress xz
	xzReader, err := xz.NewReader(resp.Body)
	if err != nil {
		return nil, err
	}

	// Parse Packages file looking for our package
	return r.parsePackagesFile(xzReader, pkgName)
}

// parsePackagesFile parses Debian Packages file to find package info
func (dr *DependencyResolver) parsePackagesFile(reader io.Reader, targetPkg string) (*PackageDeps, error) {
	scanner := bufio.NewScanner(reader)
	var currentPkg *PackageDeps
	inTarget := false

	for scanner.Scan() {
		line := scanner.Text()

		// Empty line = end of paragraph
		if line == "" {
			if inTarget && currentPkg != nil {
				return currentPkg, nil
			}
			currentPkg = nil
			inTarget = false
			continue
		}

		// Check if this is our target package
		if strings.HasPrefix(line, "Package: ") {
			pkg := strings.TrimPrefix(line, "Package: ")
			if pkg == targetPkg {
				inTarget = true
				currentPkg = &PackageDeps{Package: pkg}
			}
		}

		if !inTarget || currentPkg == nil {
			continue
		}

		// Parse fields
		switch {
		case strings.HasPrefix(line, "Source: "):
			currentPkg.Source = strings.TrimPrefix(line, "Source: ")
		case strings.HasPrefix(line, "Version: "):
			currentPkg.Version = strings.TrimPrefix(line, "Version: ")
		case strings.HasPrefix(line, "Depends: "):
			currentPkg.Depends = dr.parseDepLine(strings.TrimPrefix(line, "Depends: "))
		case strings.HasPrefix(line, "Suggests: "):
			currentPkg.Suggests = dr.parseDepLine(strings.TrimPrefix(line, "Suggests: "))
		case strings.HasPrefix(line, "Recommends: "):
			currentPkg.Recommends = dr.parseDepLine(strings.TrimPrefix(line, "Recommends: "))
		case strings.HasPrefix(line, "Conflicts: "):
			currentPkg.Conflicts = dr.parseDepLine(strings.TrimPrefix(line, "Conflicts: "))
		case strings.HasPrefix(line, "Provides: "):
			currentPkg.Provides = dr.parseDepLine(strings.TrimPrefix(line, "Provides: "))
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	if inTarget && currentPkg != nil {
		return currentPkg, nil
	}

	return nil, fmt.Errorf("package not found")
}

// parseDepLine parses dependency line (handles alternatives with |)
func (r *DependencyResolver) parseDepLine(line string) []string {
	result := make([]string, 0)

	// Split by comma (separate dependencies)
	parts := strings.Split(line, ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			result = append(result, part)
		}
	}

	return result
}

// Cache methods
func (c *dependencyCache) get(pkg string) *PackageDeps {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.deps[pkg]
}

func (c *dependencyCache) set(pkg string, deps *PackageDeps) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.deps[pkg] = deps
}

func (c *dependencyCache) getSource(binary string) string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.srcMap[binary]
}

func (c *dependencyCache) setSource(binary, source string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.srcMap[binary] = source
}

// mapBinaryToSourceFast provides fast static mappings for common packages
func mapBinaryToSourceFast(binary string) string {
	// Comprehensive mapping of Debian binary packages to source packages
	mappings := map[string]string{
		// Essential build tools
		"cmake":       "cmake",
		"ninja-build": "ninja-build",
		"meson":       "meson",
		"pkg-config":  "pkgconf",
		"pkgconf":     "pkgconf",

		// Compilers
		"gcc":   "gcc-defaults",
		"g++":   "gcc-defaults",
		"clang": "llvm-defaults",

		// SSL/TLS
		"libssl-dev": "openssl",
		"libssl3":    "openssl",
		"openssl":    "openssl",

		// Compression
		"zlib1g-dev":  "zlib",
		"zlib1g":      "zlib",
		"libbz2-dev":  "bzip2",
		"liblzma-dev": "xz-utils",
		"libzstd-dev": "zstd",

		// XML
		"libxml2-dev":  "libxml2",
		"libxml2":      "libxml2",
		"libxslt1-dev": "libxslt",

		// JSON
		"libjson-c-dev": "json-c",
		"libjson-c5":    "json-c",

		// cURL
		"libcurl4-openssl-dev": "curl",
		"libcurl4-gnutls-dev":  "curl",
		"curl":                 "curl",

		// Database
		"libsqlite3-dev": "sqlite3",
		"libsqlite3-0":   "sqlite3",

		// Terminal/UI
		"libncurses-dev":  "ncurses",
		"libncurses6":     "ncurses",
		"libreadline-dev": "readline",
		"libreadline8":    "readline",

		// Image
		"libpng-dev":  "libpng1.6",
		"libpng16-16": "libpng1.6",
		"libjpeg-dev": "libjpeg-turbo",
		"libtiff-dev": "tiff",

		// Crypto
		"libgcrypt20-dev":  "libgcrypt20",
		"libgpg-error-dev": "libgpg-error",

		// GNUTLS
		"libgnutls28-dev": "gnutls28",

		// PCRE
		"libpcre3-dev": "pcre3",
		"libpcre2-dev": "pcre2",

		// Expat
		"libexpat1-dev": "expat",

		// FFI
		"libffi-dev": "libffi",

		// UUID
		"libuuid-dev": "util-linux",

		// Git
		"git": "git",

		// Flex/Bison
		"flex":  "flex",
		"bison": "bison",
		"m4":    "m4",

		// Autotools
		"autoconf": "autoconf",
		"automake": "automake",
		"libtool":  "libtool",

		// Gettext
		"gettext":          "gettext",
		"libgettextpo-dev": "gettext",

		// Lua
		"liblua5.1-dev": "lua5.1",
		"liblua5.4-dev": "lua5.4",

		// Python
		"python3-dev": "python3-defaults",
		"python3":     "python3-defaults",

		// Perl
		"perl": "perl",

		// Procps
		"procps": "procps-ng",

		// Iconv
		"libiconv-hook-dev": "libiconv",

		// ICU
		"libicu-dev": "icu",

		// HarfBuzz
		"libharfbuzz-dev": "harfbuzz",

		// FontConfig
		"libfontconfig-dev": "fontconfig",

		// Freetype
		"libfreetype-dev": "freetype",

		// Cairo
		"libcairo2-dev": "cairo",

		// Pango
		"libpango1.0-dev": "pango1.0",

		// GDK-Pixbuf
		"libgdk-pixbuf-2.0-dev": "gdk-pixbuf",

		// GLib
		"libglib2.0-dev": "glib2.0",

		// GObject introspection
		"gobject-introspection": "gobject-introspection",

		// ATK
		"libatk1.0-dev": "atk1.0",

		// GTK
		"libgtk-3-dev": "gtk+3.0",
		"libgtk-4-dev": "gtk4",

		// GLibmm
		"libglibmm-2.4-dev": "glibmm2.4",

		// X11
		"libx11-dev":        "libx11",
		"libxext-dev":       "libxext",
		"libxrender-dev":    "libxrender",
		"libxft-dev":        "libxft",
		"libxpm-dev":        "libxpm",
		"libxmu-dev":        "libxmu",
		"libxaw7-dev":       "libxaw",
		"libxfixes-dev":     "libxfixes",
		"libxinerama-dev":   "libxinerama",
		"libxrandr-dev":     "libxrandr",
		"libxcursor-dev":    "libxcursor",
		"libxcomposite-dev": "libxcomposite",
		"libxdamage-dev":    "libxdamage",
		"libxkbfile-dev":    "libxkbfile",

		// XCB
		"libxcb1-dev":         "libxcb",
		"libxcb-keysyms1-dev": "xcb-util-keysyms",

		// Xt
		"libxt-dev": "libxt",

		// SM/ICE
		"libsm-dev":  "libsm",
		"libice-dev": "libice",

		// Xtrans
		"libxtrans-dev": "libxtrans",

		// X11proto
		"x11proto-dev":       "xorgproto",
		"xorg-sgml-doctools": "xorg-sgml-doctools",

		// Wayland
		"libwayland-dev":    "wayland",
		"wayland-protocols": "wayland-protocols",

		// OpenGL
		"libgl-dev":        "libglvnd",
		"libgl1-mesa-dev":  "mesa",
		"libegl1-mesa-dev": "mesa",
		"libgbm-dev":       "mesa",

		// Vulkan
		"libvulkan-dev": "vulkan-loader",

		// Shaderc
		"libshaderc-dev": "shaderc",

		// SPIRV
		"libspirv-tools-dev": "spirv-tools",

		// PulseAudio
		"libpulse-dev": "pulseaudio",

		// ALSA
		"libasound2-dev": "alsa-lib",

		// JACK
		"libjack-dev": "jack-audio-connection-kit",

		// PipeWire
		"libpipewire-0.3-dev": "pipewire",

		// D-Bus
		"libdbus-1-dev": "dbus",

		// GMP
		"libgmp-dev": "gmp",

		// MPFR
		"libmpfr-dev": "mpfr",

		// MPC
		"libmpc-dev": "mpc",

		// GPGME
		"libgpgme-dev": "gpgme1.0",

		// Assuan
		"libassuan-dev": "libassuan",

		// KSBA
		"libksba-dev": "libksba",

		// NTBTLS
		"libntbtls-dev": "ntbtls",

		// OpenLDAP
		"libldap2-dev": "openldap",

		// SASL
		"libsasl2-dev": "cyrus-sasl2",

		// Kerberos
		"libkrb5-dev": "krb5",

		// Keyutils
		"libkeyutils-dev": "keyutils",

		// Nettle
		"libnettle-dev": "nettle",

		// Hogweed
		"libhogweed6": "nettle",

		// TASN1
		"libtasn1-6-dev": "libtasn1-6",

		// IDN2
		"libidn2-dev": "libidn2",

		// Unistring
		"libunistring-dev": "libunistring",

		// PSL
		"libpsl-dev": "libpsl",

		// BROTLI
		"libbrotli-dev": "brotli",

		// NGHTTP2
		"libnghttp2-dev": "nghttp2",

		// Systemd
		"libsystemd-dev": "systemd",

		// Elogind
		"libelogind-dev": "elogind",

		// UPower
		"libupower-glib-dev": "upower",

		// UDisks
		"libudisks2-dev": "udisks2",

		// Polkit
		"libpolkit-gobject-1-dev": "policykit-1",

		// ConsoleKit
		"libck-connector-dev": "consolekit",

		// UDEV
		"libudev-dev": "systemd",

		// Eudev
		"libeudev-dev": "eudev",

		// KMod
		"libkmod-dev": "kmod",

		// PCIUtils
		"libpci-dev": "pciutils",

		// USBUtils
		"libusb-1.0-0-dev": "usbutils",

		// Libusb
		"libusb-1.0-dev": "libusb-1.0",

		// HIDAPI
		"libhidapi-dev": "hidapi",

		// SDL2
		"libsdl2-dev": "sdl2",

		// OpenAL
		"libopenal-dev": "openal-soft",

		// Ogg
		"libogg-dev": "libogg",

		// Vorbis
		"libvorbis-dev": "libvorbis",

		// FLAC
		"libflac-dev": "flac",

		// Opus
		"libopus-dev": "opus",

		// Theora
		"libtheora-dev": "libtheora",

		// VPX
		"libvpx-dev": "libvpx",

		// AOM
		"libaom-dev": "aom",

		// DAV1D
		"libdav1d-dev": "dav1d",

		// FFmpeg
		"libavcodec-dev":  "ffmpeg",
		"libavformat-dev": "ffmpeg",
		"libavutil-dev":   "ffmpeg",

		// X264
		"libx264-dev": "x264",

		// X265
		"libx265-dev": "x265",

		// WebP
		"libwebp-dev": "libwebp",

		// OpenEXR
		"libopenexr-dev": "openexr",

		// Imath
		"libimath-dev": "imath",

		// OpenJPEG
		"libopenjp2-7-dev": "openjpeg2",

		// LittleCMS
		"liblcms2-dev": "lcms2",

		// "libreadline-dev":     "readline",

		// History
		"libhistory-dev": "readline",

		// Termcap
		"libtinfo-dev": "ncurses",

		// Curses
		"libcurses-dev": "ncurses",

		// Panel
		"libpanel-dev": "ncurses",

		// Menu
		"libmenu-dev": "ncurses",

		// Form
		"libform-dev": "ncurses",

		// SLANG
		"libslang2-dev": "slang2",

		// Newt
		"libnewt-dev": "newt",

		// CDK
		"libcdk5-dev": "cdk",

		// ADNS
		"libadns1-dev": "adns",

		// Anet
		"libanet-dev": "anet",

		// APR
		"libapr1-dev": "apr",

		// APR-Util
		"libaprutil1-dev": "apr-util",

		// ASpell
		"libaspell-dev": "aspell",

		// Hunspell
		"libhunspell-dev": "hunspell",

		// HSpell
		"libhspell-dev": "hspell",

		// Voikko
		"libvoikko-dev": "libvoikko",

		// Zemberek
		"libzemberek-dev": "zemberek",

		// Zinnia
		"libzinnia-dev": "zinnia",

		// Teckit
		"libteckit-dev": "teckit",

		// Graphite
		"libgraphite2-dev": "graphite2",

		// Raptor
		"libraptor2-dev": "raptor2",

		// Rasqal
		"librasqal3-dev": "rasqal",

		// Redland
		"librdf0-dev": "redland",

		// Serd
		"libserd-dev": "serd",

		// Sord
		"libsord-dev": "sord",

		// SRATOM
		"libsratom-dev": "sratom",

		// LILV
		"liblilv-dev": "lilv",

		// LV2
		"lv2-dev": "lv2",

		// SUIL
		"libsuil-dev": "suil",

		// DSSI
		"libdssi-dev": "dssi",

		// LADSPA
		"libladspa-sdk-dev": "ladspa-sdk",

		// VST
		"vst3sdk-dev": "vst3sdk",

		// CLAP
		"clap-dev": "clap",

		// BS2B
		"libbs2b-dev": "libbs2b",

		// LADSPA (again)
		"ladspa-sdk": "ladspa-sdk",

		// CAPS
		"caps": "caps",

		// TAP
		"tap-plugins": "tap-plugins",

		// CMT
		"cmt": "cmt",

		// SWH
		"swh-plugins": "swh-plugins",

		// FFAUDIO
		"libffado-dev": "libffado",

		// ALSA (again)
		"libasound-dev": "alsa-lib",

		// FireWire
		"libraw1394-dev": "libraw1394",

		// IEC61883
		"libiec61883-dev": "libiec61883",

		// AV1394
		"libavc1394-dev": "libavc1394",

		// DC1394
		"libdc1394-dev": "libdc1394",

		// GLIB (already defined above)
		// "libglib2.0-dev":      "glib2.0",

		// GObject (again)
		"libgobject2.0-dev": "glib2.0",

		// GModule (again)
		"libgmodule2.0-dev": "glib2.0",

		// GIO (again)
		"libgio2.0-dev": "glib2.0",

		// GThread (again)
		"libgthread2.0-dev": "glib2.0",

		// Mount
		"libmount-dev": "util-linux",

		// BLKID
		"libblkid-dev": "util-linux",

		// UUID (already defined above)
		// "libuuid-dev":         "util-linux",

		// SELinux
		"libselinux1-dev": "selinux",

		// SEManage
		"libsemanage-dev": "semanage",

		// SECureDB
		"libsecomp-dev": "libseccomp",

		// Audit
		"libauparse-dev": "audit",

		// E2FSProgs
		"libext2fs-dev": "e2fsprogs",

		// ComErr
		"libcom-err2-dev": "e2fsprogs",

		// SS
		"libss2-dev": "e2fsprogs",

		// Magic
		"libmagic-dev": "file",

		// ACap
		"libcap-dev": "libcap2",

		// ACap-NG
		"libcap-ng-dev": "libcap-ng",

		// AppArmor
		"libapparmor-dev": "apparmor",

		// Tomoyo
		"libtomoyotools-dev": "tomoyo-tools",

		// SMACK
		"libsmack-dev": "smack",

		// TOMOYO (again)
		"tomoyo-tools": "tomoyo-tools",

		// IMA-EVM
		"ima-evm-utils": "ima-evm-utils",

		// TSS2
		"libtss2-dev": "tpm2-tss",

		// FAPI
		"libfapi-dev": "tpm2-tss",

		// TCTI
		"libtcti-dev": "tpm2-tss",

		// ESYS
		"libesys-dev": "tpm2-tss",

		// MU
		"libmu-dev": "mailutils",

		// Guile
		"guile-3.0-dev": "guile-3.0",

		// GMP (already defined at line 346)
		// "libgmp-dev": "gmp",

		// Ncurses (variant)
		"libncursesw5-dev": "ncurses",

		// Readline (already defined at line 369)
		// "libreadline-dev": "readline",

		// Texinfo
		"texinfo": "texinfo",

		// TexLive
		"texlive": "texlive-base",

		// Doxygen
		"doxygen": "doxygen",

		// Graphviz
		"graphviz-dev": "graphviz",

		// PLplot
		"libplplot-dev": "plplot",

		// GSL
		"libgsl-dev": "gsl",

		// FFTW
		"libfftw3-dev": "fftw3",

		// BLAS
		"libblas-dev": "blas",

		// LAPACK
		"liblapack-dev": "lapack",

		// ATLAS
		"libatlas-base-dev": "atlas",

		// OpenBLAS
		"libopenblas-dev": "openblas",

		// SuiteSparse
		"libsuitesparse-dev": "suitesparse",

		// ARPACK
		"libarpack2-dev": "arpack",

		// PARPACK
		"libparpack2-dev": "arpack",

		// SuperLU
		"libsuperlu-dev": "superlu",

		// METIS
		"libmetis-dev": "metis",

		// Scotch
		"libscotch-dev": "scotch",

		// MUMPS
		"libmumps-dev": "mumps",

		// Hypre
		"libhypre-dev": "hypre",

		// PETSc
		"libpetsc-dev": "petsc",

		// SLEPc
		"libslepc-dev": "slepc",

		// Trilinos
		"trilinos-all-dev": "trilinos",

		// Boost
		"libboost-all-dev": "boost-defaults",

		// Eigen3
		"libeigen3-dev": "eigen3",

		// Dune
		"libdune-common-dev": "dune-common",

		// FEniCS
		"fenics": "fenics",

		// Deal.II
		"libdeal.ii-dev": "deal.ii",

		// CGAL
		"libcgal-dev": "cgal",

		// PPL
		"libppl-dev": "ppl",

		// NTL
		"libntl-dev": "ntl",

		// FLINT
		"libflint-dev": "flint",

		// Arb
		"libarb-dev": "arb",

		// Antic
		"libantic-dev": "antic",

		// Calcium
		"libcalcium-dev": "calcium",

		// NF
		"libnf-dev": "nf",

		// GF2X
		"libgf2x-dev": "gf2x",

		// M4RI
		"libm4ri-dev": "m4ri",

		// M4RIE
		"libm4rie-dev": "m4rie",

		// LinBox
		"liblinbox-dev": "linbox",

		// IML
		"libiml-dev": "iml",

		// BLAD
		"libblad-dev": "blad",

		// CADO
		"cado-nfs-dev": "cado-nfs",

		// PARI
		"libpari-dev": "pari",

		// Symmetrica
		"symmetrica-dev": "symmetrica",

		// Lidia
		"liblidia-dev": "lidia",

		// NTL (already defined above)
		// "libntl-dev":          "ntl",

		// OpenSSL (already defined above)
		// "libssl-dev":          "openssl",

		// OpenSSH
		"libssh-dev": "libssh",

		// OpenSSH2
		"libssh2-1-dev": "libssh2",

		// Neon
		"libneon27-dev": "neon27",

		// Serf
		"libserf-1-dev": "serf",

		// Subversion
		"libsvn-dev": "subversion",

		// RHash
		"librhash-dev": "rhash",

		// XXHASH
		"libxxhash-dev": "xxhash",

		// HighwayHash
		"libhighwayhash-dev": "highwayhash",

		// CityHash
		"libcityhash-dev": "cityhash",

		// MurmurHash
		"libmurmurhash-dev": "murmurhash",

		// SpookyHash
		"libspookyhash-dev": "spookyhash",

		// FarmHash
		"libfarmhash-dev": "farmhash",

		// MetroHash
		"libmetrohash-dev": "metrohash",

		// t1ha
		"libt1ha-dev": "t1ha",

		// PRVHASH
		"libprvhash-dev": "prvhash",

		// Wyhash
		"libwyhash-dev": "wyhash",

		// rapidhash
		"librapidhash-dev": "rapidhash",

		// SHA3
		"libsha3-dev": "sha3",

		// Blake2
		"libblake2-dev": "blake2",

		// Blake3
		"libblake3-dev": "blake3",

		// KangarooTwelve
		"libk12-dev": "k12",

		// SHAKE
		"libshake-dev": "sha3",

		// TupleHash
		"libtuplehash-dev": "sha3",

		// ParallelHash
		"libparallelhash-dev": "sha3",

		// cSHAKE
		"libcshake-dev": "sha3",

		// TurboSHAKE
		"libturboshake-dev": "turbo",

		// Ascon
		"libascon-dev": "ascon",

		// ISAP
		"libisap-dev": "isap",

		// DryGascon
		"libdrygascon-dev": "drygascon",

		// Gimli
		"libgimli-dev": "gimli",

		// Xoodoo
		"libxoodoo-dev": "xoodoo",

		// Keccak
		"libkeccak-dev": "keccak",

		// FNV
		"libfnv-dev": "fnv",

		// Murmur (already defined above)
		"libmurmurhash3-dev": "murmurhash",

		// CityHash (already defined above)
		// "libcityhash-dev":     "cityhash",
	}

	return mappings[binary]
}

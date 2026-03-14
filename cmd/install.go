package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"zsvo/pkg/builder"
	"zsvo/pkg/debian"
	"zsvo/pkg/i18n"
	"zsvo/pkg/installer"
	"zsvo/pkg/recipe"
	"zsvo/pkg/ui"

	"github.com/spf13/cobra"
)

var InstallCmd = &cobra.Command{
	Use:   "install <package> [package...]",
	Short: i18n.T("install_cmd"),
	Long:  `Install one or more packages from local files or auto-build from Debian source by package name`,
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		rootDir, _ := cmd.Flags().GetString("root")
		if rootDir == "" {
			rootDir = "/"
		}
		workDir, _ := cmd.Flags().GetString("work-dir")
		if strings.TrimSpace(workDir) == "" {
			workDir = "/tmp/pkg-work"
		}
		autoSource, _ := cmd.Flags().GetBool("auto-source")
		autoBuildDeps, _ := cmd.Flags().GetBool("auto-build-deps")
		dryRun, _ := cmd.Flags().GetBool("dry-run")

		if dryRun {
			status := ui.NewStatusBar("", 1)
			status.SetTheme("neon")
			status.PrintHeader("DRY RUN MODE")
			status.PrintInfo(fmt.Sprintf("Root directory: %s", rootDir))
			status.PrintInfo(fmt.Sprintf("Work directory: %s", workDir))
			status.PrintInfo(fmt.Sprintf("Auto-source: %t", autoSource))
			status.PrintInfo(fmt.Sprintf("Auto-build-deps: %t", autoBuildDeps))
			status.PrintInfo(fmt.Sprintf("Auto-resolve-deps: enabled (default)"))
			status.PrintFooter()
		}

		installTargets := make([]string, 0, len(args))
		i := installer.NewInstaller(rootDir)

		var session *autoBuildSession
		if autoSource {
			session = newAutoBuildSession(workDir, autoBuildDeps)
		}

		for _, target := range args {
			isFile, err := isInstallFileTarget(target)
			if err != nil {
				return err
			}

			if isFile {
				if dryRun {
					status := ui.NewStatusBar("", 1)
					status.SetTheme("neon")
					status.PrintInfo(fmt.Sprintf(i18n.T("Would install package from file: %s"), target))
				}
				installTargets = append(installTargets, target)
				continue
			}

			if !autoSource {
				return fmt.Errorf(
					"%s is not a package file; pass a file path or enable --auto-source",
					target,
				)
			}

			if dryRun {
				status := ui.NewStatusBar("", 1)
				status.SetTheme("neon")
				status.PrintInfo(fmt.Sprintf(i18n.T("Would auto-build package: %s"), target))
				installTargets = append(installTargets, target) // для демонстрации
				continue
			}

			builtPackage, err := session.buildPackage(target, false, nil)
			if err != nil {
				return err
			}
			installTargets = append(installTargets, builtPackage)
		}

		if len(installTargets) == 1 {
			if dryRun {
				status := ui.NewStatusBar("", 1)
				status.SetTheme("neon")
				status.PrintInfo(i18n.T("Would install 1 package"))
			} else {
				fmt.Printf(i18n.T("Installing package from %s...")+"\n", installTargets[0])
			}
		} else {
			if dryRun {
				status := ui.NewStatusBar("", 1)
				status.SetTheme("neon")
				status.PrintInfo(fmt.Sprintf(i18n.T("Would install %d packages"), len(installTargets)))
			} else {
				fmt.Printf(i18n.T("Installing %d packages...")+"\n", len(installTargets))
			}
		}

		if dryRun {
			status := ui.NewStatusBar("", 1)
			status.SetTheme("neon")
			status.PrintHeader("DRY RUN COMPLETE")
			status.PrintInfo(i18n.T("No actual changes were made."))
			status.PrintFooter()
			return nil
		}

		// Choose installation method - always use auto-resolve
		searchPaths := []string{
			filepath.Join(rootDir, "var", "cache", "packages"),
			filepath.Join(workDir, "packages"),
			"/tmp/pkg-work/packages",
		}

		err := i.InstallWithAutoResolve(installTargets, searchPaths)
		if err != nil {
			return fmt.Errorf("failed to install packages: %w", err)
		}

		fmt.Printf(i18n.T("Package installation completed successfully") + "\n")
		return nil
	},
}

func init() {
	InstallCmd.Flags().StringP("root", "r", "/", "Root directory for installation")
	InstallCmd.Flags().StringP("work-dir", "w", "/tmp/pkg-work", "Working directory for source builds")
	InstallCmd.Flags().Bool("auto-source", true, "Auto-build package names from Debian source")
	InstallCmd.Flags().Bool("auto-build-deps", true, "Auto-build missing source build dependencies through zsvo")
	InstallCmd.Flags().Bool("dry-run", false, "Show what would be done without making changes")
}

func isInstallFileTarget(target string) (bool, error) {
	target = strings.TrimSpace(target)
	if target == "" {
		return false, fmt.Errorf("install target cannot be empty")
	}

	// Explicit file-like targets are validated as paths.
	if looksLikeFilePath(target) {
		info, err := os.Stat(target)
		if err != nil {
			return false, fmt.Errorf("package file %s not found: %w", target, err)
		}
		if info.IsDir() {
			return false, fmt.Errorf("package file %s is a directory", target)
		}
		return true, nil
	}

	// If a same-name local file exists, use it as package archive.
	if info, err := os.Stat(target); err == nil {
		if info.IsDir() {
			return false, fmt.Errorf("package file %s is a directory", target)
		}
		return true, nil
	}

	return false, nil
}

func looksLikeFilePath(target string) bool {
	return strings.Contains(target, string(os.PathSeparator)) ||
		strings.HasPrefix(target, ".") ||
		strings.HasSuffix(target, ".pkg.tar.zst") ||
		strings.HasSuffix(target, ".zov")
}

const maxAutoBuildDepth = 16

type autoBuildSession struct {
	workDir          string
	toolRoot         string
	autoBuildDeps    bool
	resolver         *debian.Resolver
	builder          *builder.Builder
	toolInstaller    *installer.Installer
	builtPackages    map[string]string
	toolDepsReady    map[string]struct{}
	buildingPackages map[string]struct{}
}

func newAutoBuildSession(workDir string, autoBuildDeps bool) *autoBuildSession {
	b := builder.NewBuilder(workDir)
	b.SetQuiet(true)

	s := &autoBuildSession{
		workDir:          workDir,
		toolRoot:         filepath.Join(workDir, "bootstrap-root"),
		autoBuildDeps:    autoBuildDeps,
		resolver:         debian.NewResolver(),
		builder:          b,
		toolInstaller:    installer.NewInstaller(filepath.Join(workDir, "bootstrap-root")),
		builtPackages:    make(map[string]string),
		toolDepsReady:    make(map[string]struct{}),
		buildingPackages: make(map[string]struct{}),
	}
	s.refreshBuildEnv()
	return s
}

func (s *autoBuildSession) refreshBuildEnv() {
	basePath := splitPathList(os.Getenv("PATH"))
	binPrefixes := []string{
		filepath.Join(s.toolRoot, "usr", "bin"),
		filepath.Join(s.toolRoot, "bin"),
		filepath.Join(s.toolRoot, "usr", "sbin"),
		filepath.Join(s.toolRoot, "sbin"),
	}
	mergedPath := joinPathListUnique(append(binPrefixes, basePath...))
	s.builder.SetEnvOverride("PATH", mergedPath)

	pkgConfigPath := splitPathList(os.Getenv("PKG_CONFIG_PATH"))
	pkgConfigPrefixes := []string{
		filepath.Join(s.toolRoot, "usr", "lib", "pkgconfig"),
		filepath.Join(s.toolRoot, "usr", "lib64", "pkgconfig"),
		filepath.Join(s.toolRoot, "usr", "share", "pkgconfig"),
		filepath.Join(s.toolRoot, "lib", "pkgconfig"),
		filepath.Join(s.toolRoot, "lib64", "pkgconfig"),
	}
	s.builder.SetEnvOverride("PKG_CONFIG_PATH", joinPathListUnique(append(pkgConfigPrefixes, pkgConfigPath...)))
	cmakePrefixes := []string{
		filepath.Join(s.toolRoot, "usr"),
		filepath.Join(s.toolRoot),
	}
	cmakePrefixes = append(cmakePrefixes, splitPathList(os.Getenv("CMAKE_PREFIX_PATH"))...)
	s.builder.SetEnvOverride("CMAKE_PREFIX_PATH", joinPathListUnique(cmakePrefixes))
}

func (s *autoBuildSession) buildPackage(requestName string, asBuildDep bool, stack []string) (string, error) {
	requestName = normalizePackageName(requestName)
	if requestName == "" {
		return "", fmt.Errorf("invalid package name")
	}

	if len(stack) >= maxAutoBuildDepth {
		return "", fmt.Errorf("dependency chain is too deep while building %s: %s", requestName, strings.Join(append(stack, requestName), " -> "))
	}
	if _, exists := s.buildingPackages[requestName]; exists {
		return "", fmt.Errorf("dependency cycle detected: %s", strings.Join(append(stack, requestName), " -> "))
	}

	if builtPath, ok := s.builtPackages[requestName]; ok {
		if asBuildDep {
			if err := s.installBuildDependency(requestName, builtPath); err != nil {
				return "", err
			}
		}
		return builtPath, nil
	}

	s.buildingPackages[requestName] = struct{}{}
	defer delete(s.buildingPackages, requestName)

	fmt.Printf("Resolving Debian source for %s...\n", requestName)
	srcInfo, err := s.resolver.ResolveSource(requestName)
	if err != nil {
		return "", fmt.Errorf("failed to resolve source for %s: %w", requestName, err)
	}

	rcp := autoRecipeFromDebian(srcInfo)
	normalizedRecipeName := normalizePackageName(rcp.Name)
	if normalizedRecipeName != "" && normalizedRecipeName != requestName {
		// Alias the resolved source package name to the requested name.
		if _, exists := s.buildingPackages[normalizedRecipeName]; exists {
			return "", fmt.Errorf("dependency cycle detected: %s", strings.Join(append(stack, requestName, normalizedRecipeName), " -> "))
		}
	}

	fmt.Printf("Building %s from %s...\n", rcp.GetPackageName(), srcInfo.DSCURL)
	var buildErr error
	for attempt := 0; attempt < 2; attempt++ {
		bar := newProgressUI(rcp.Name)
		s.builder.SetProgressCallback(func(p builder.BuildProgress) {
			bar.update(p.Step, p.Total, fmt.Sprintf("%s: %s", rcp.Name, p.Message))
		})

		buildErr = s.builder.Build(rcp)
		s.builder.SetProgressCallback(nil)
		if buildErr == nil {
			bar.finish(true, fmt.Sprintf("%s: complete", rcp.Name))
			break
		}
		bar.finish(false, fmt.Sprintf("%s: failed", rcp.Name))

		if !s.autoBuildDeps || attempt == 1 {
			hint := buildFailureHint(requestName, buildErr)
			if hint != "" {
				return "", fmt.Errorf("failed to auto-build %s: %w\n%s", requestName, buildErr, hint)
			}
			return "", fmt.Errorf("failed to auto-build %s: %w", requestName, buildErr)
		}

		missingDeps := inferMissingBuildDeps(buildErr)
		if len(missingDeps) == 0 {
			hint := buildFailureHint(requestName, buildErr)
			if hint != "" {
				return "", fmt.Errorf("failed to auto-build %s: %w\n%s", requestName, buildErr, hint)
			}
			return "", fmt.Errorf("failed to auto-build %s: %w", requestName, buildErr)
		}

		fmt.Printf("Detected missing build dependencies for %s: %s\n", requestName, strings.Join(missingDeps, ", "))
		for _, dep := range missingDeps {
			if dep == requestName || dep == normalizedRecipeName {
				continue
			}
			if _, ready := s.toolDepsReady[dep]; ready {
				continue
			}
			if err := s.ensureBuildDependency(dep, append(stack, requestName)); err != nil {
				return "", fmt.Errorf("failed to satisfy build dependency %s for %s: %w", dep, requestName, err)
			}
		}

		fmt.Printf("Retrying build for %s after auto-installing dependencies...\n", requestName)
	}

	if buildErr != nil {
		return "", fmt.Errorf("failed to auto-build %s: %w", requestName, buildErr)
	}

	builtPackage := filepath.Join(rcp.GetPackageDir(s.workDir), rcp.GetPackageFileName())
	fmt.Printf("Built package: %s\n", builtPackage)

	s.builtPackages[requestName] = builtPackage
	if normalizedRecipeName != "" {
		s.builtPackages[normalizedRecipeName] = builtPackage
	}

	if asBuildDep {
		if err := s.installBuildDependency(requestName, builtPackage); err != nil {
			return "", err
		}
	}

	return builtPackage, nil
}

func (s *autoBuildSession) ensureBuildDependency(dep string, stack []string) error {
	dep = normalizePackageName(dep)
	if dep == "" {
		return fmt.Errorf("invalid build dependency name")
	}

	if _, ready := s.toolDepsReady[dep]; ready {
		return nil
	}
	if toolAlreadyAvailable(dep) {
		s.toolDepsReady[dep] = struct{}{}
		return nil
	}

	packagePath, err := s.buildPackage(dep, true, stack)
	if err != nil {
		return err
	}

	return s.installBuildDependency(dep, packagePath)
}

func (s *autoBuildSession) installBuildDependency(dep, packagePath string) error {
	dep = normalizePackageName(dep)
	if dep == "" {
		return fmt.Errorf("invalid build dependency name")
	}
	if _, ready := s.toolDepsReady[dep]; ready {
		return nil
	}

	fmt.Printf("Installing build dependency %s into %s...\n", dep, s.toolRoot)
	if err := s.toolInstaller.Install(packagePath); err != nil {
		return fmt.Errorf("failed to install build dependency %s: %w", dep, err)
	}
	s.toolDepsReady[dep] = struct{}{}
	s.refreshBuildEnv()
	return nil
}

func splitPathList(value string) []string {
	parts := strings.Split(value, string(os.PathListSeparator))
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		out = append(out, part)
	}
	return out
}

func joinPathListUnique(parts []string) string {
	seen := make(map[string]struct{}, len(parts))
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if _, exists := seen[part]; exists {
			continue
		}
		seen[part] = struct{}{}
		out = append(out, part)
	}
	return strings.Join(out, string(os.PathListSeparator))
}

var simplePkgNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9+.-]*[a-z0-9]$`)
var missingCommandPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?m)(?:^|[\s:])(?:/bin/)?sh:\s*(?:\d+:\s*)?([a-zA-Z0-9+_.-]+):\s*(?:command not found|not found)\b`),
	regexp.MustCompile(`(?m)\b([a-zA-Z0-9+_.-]+):\s*command not found\b`),
	regexp.MustCompile(`(?m)\b([a-zA-Z0-9+_.-]+):\s*not found\b`),
}

func inferMissingBuildDeps(err error) []string {
	if err == nil {
		return nil
	}

	text := err.Error()
	lowerText := strings.ToLower(text)
	found := make(map[string]struct{})

	for _, pattern := range missingCommandPatterns {
		matches := pattern.FindAllStringSubmatch(text, -1)
		for _, match := range matches {
			if len(match) < 2 {
				continue
			}
			pkg := mapToolToSourcePackage(match[1])
			if pkg == "" {
				continue
			}
			found[pkg] = struct{}{}
		}
	}

	if strings.Contains(lowerText, "failed to find a lua 5.1-compatible interpreter") {
		found["lua5.1"] = struct{}{}
	}
	if strings.Contains(lowerText, "pkg-config") &&
		(strings.Contains(lowerText, "not found") || strings.Contains(lowerText, "could not find")) {
		found["pkgconf"] = struct{}{}
	}
	if strings.Contains(lowerText, "no acceptable c compiler found in $path") ||
		strings.Contains(lowerText, "c compiler cannot create executables") {
		found["gcc"] = struct{}{}
	}

	// Detect missing CMake
	if strings.Contains(lowerText, "cmake") &&
		(strings.Contains(lowerText, "not found") ||
			strings.Contains(lowerText, "command not found") ||
			strings.Contains(lowerText, "cmake: command not found") ||
			strings.Contains(lowerText, "cmake command not found") ||
			strings.Contains(lowerText, "no cmake") ||
			strings.Contains(lowerText, "could not find cmake")) {
		found["cmake"] = struct{}{}
	}

	// Detect missing Git
	if strings.Contains(lowerText, "git") &&
		(strings.Contains(lowerText, "not found") ||
			strings.Contains(lowerText, "command not found") ||
			strings.Contains(lowerText, "git: command not found") ||
			strings.Contains(lowerText, "git command not found") ||
			strings.Contains(lowerText, "no git") ||
			strings.Contains(lowerText, "could not find git")) {
		found["git"] = struct{}{}
	}

	// Detect missing make
	if strings.Contains(lowerText, "make") &&
		(strings.Contains(lowerText, "not found") ||
			strings.Contains(lowerText, "command not found") ||
			strings.Contains(lowerText, "make: command not found") ||
			strings.Contains(lowerText, "make command not found") ||
			strings.Contains(lowerText, "no make") ||
			strings.Contains(lowerText, "could not find make")) {
		found["make"] = struct{}{}
	}

	// Detect missing source files and CMake errors
	if strings.Contains(lowerText, "cannot find source file") ||
		strings.Contains(lowerText, "no sources given to target") ||
		strings.Contains(lowerText, "cmake generate step failed") ||
		strings.Contains(lowerText, "cmake error") ||
		strings.Contains(lowerText, "could not load cache") {
		found["cmake"] = struct{}{}
	}

	// Detect missing submodules/sources (git issues)
	if strings.Contains(lowerText, "yyjson.c") ||
		strings.Contains(lowerText, "3rdparty") ||
		strings.Contains(lowerText, "submodule") ||
		strings.Contains(lowerText, "git submodule") ||
		strings.Contains(lowerText, "fatal: not a git repository") ||
		strings.Contains(lowerText, "not a git repository") {
		found["git"] = struct{}{}
	}

	if len(found) == 0 {
		return nil
	}

	deps := make([]string, 0, len(found))
	for dep := range found {
		deps = append(deps, dep)
	}
	sort.Strings(deps)
	return deps
}

func mapToolToSourcePackage(tool string) string {
	tool = normalizePackageName(tool)
	if tool == "" || allDigits(tool) || len(tool) < 2 {
		return ""
	}

	// Filter out common words that are not package names
	commonWords := map[string]bool{
		"was": true, "the": true, "and": true, "for": true, "are": true,
		"but": true, "not": true, "you": true, "all": true, "can": true,
		"had": true, "her": true, "one": true, "our": true,
		"out": true, "day": true, "get": true, "has": true, "him": true,
		"his": true, "how": true, "man": true, "new": true, "now": true,
		"old": true, "see": true, "two": true, "way": true, "who": true,
		"its": true, "did": true, "yes": true, "she": true, "may": true,
		"why": true, "try": true, "use": true,
	}

	if commonWords[tool] {
		return ""
	}

	switch tool {
	case "sh", "bash", "dash", "zsh":
		return ""
	case "pkg-config":
		return "pkgconf"
	case "ninja":
		return "ninja-build"
	case "python":
		return "python3"
	case "python3":
		return "python3"
	case "lua":
		return "lua5.1"
	case "luajit":
		return "luajit"
	case "cc", "c++", "g++", "gcc":
		return "gcc"
	case "ld":
		return "binutils"
	case "xzcat":
		return "xz-utils"
	case "git":
		return "git"
	case "cmake":
		return "cmake"
	case "make":
		return "make"
	case "autoconf":
		return "autoconf"
	case "automake":
		return "automake"
	case "libtool":
		return "libtool"
	case "pkgconf":
		return "pkgconf"
	case "flex":
		return "flex"
	case "bison":
		return "bison"
	case "m4":
		return "m4"
	case "gettext":
		return "gettext"
	case "libssl-dev":
		return "libssl-dev"
	case "libcrypto-dev":
		return "libssl-dev"
	case "zlib1g-dev":
		return "zlib1g-dev"
	case "libpng-dev":
		return "libpng-dev"
	case "libjpeg-dev":
		return "libjpeg-dev"
	case "libxml2-dev":
		return "libxml2-dev"
	case "libcurl4-openssl-dev":
		return "libcurl4-openssl-dev"
	case "libffi-dev":
		return "libffi-dev"
	case "libreadline-dev":
		return "libreadline-dev"
	case "libncurses-dev":
		return "libncurses-dev"
	case "libsqlite3-dev":
		return "libsqlite3-dev"
	case "libbz2-dev":
		return "libbz2-dev"
	case "liblzma-dev":
		return "liblzma-dev"
	case "libiconv":
		return "libiconv"
	case "libintl":
		return "gettext"
	case "libuuid-dev":
		return "libuuid-dev"
	case "libexpat1-dev":
		return "libexpat1-dev"
	case "libpcre3-dev":
		return "libpcre3-dev"
	case "libpcre2-dev":
		return "libpcre2-dev"
	case "libgcrypt20-dev":
		return "libgcrypt20-dev"
	case "libgpg-error-dev":
		return "libgpg-error-dev"
	case "libgnutls28-dev":
		return "libgnutls28-dev"
	}

	if !simplePkgNamePattern.MatchString(tool) {
		return ""
	}
	return tool
}

func allDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, ch := range s {
		if ch < '0' || ch > '9' {
			return false
		}
	}
	return true
}

func normalizePackageName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

var commandHintsByDep = map[string][]string{
	"pkgconf":     {"pkg-config"},
	"ninja-build": {"ninja"},
	"python3":     {"python3", "python"},
	"lua5.1":      {"lua", "lua5.1"},
}

func toolAlreadyAvailable(dep string) bool {
	dep = normalizePackageName(dep)
	if dep == "" {
		return false
	}

	commands := commandHintsByDep[dep]
	if len(commands) == 0 {
		commands = []string{dep}
	}

	for _, name := range commands {
		if _, err := exec.LookPath(name); err == nil {
			return true
		}
	}
	return false
}

func autoRecipeFromDebian(src *debian.SourceInfo) *recipe.Recipe {
	name := src.SourcePackage
	if name == "" {
		name = src.RequestedPackage
	}
	version := src.UpstreamVersion
	if version == "" {
		version = "0"
	}

	// Map package names to binary names for common packages
	binaryName := name
	switch name {
	case "lua5.1":
		binaryName = "lua"
	case "python3":
		binaryName = "python3"
	case "python3-dev":
		binaryName = "python3-config"
	case "gcc":
		binaryName = "gcc"
	case "make":
		binaryName = "make"
	case "git":
		binaryName = "git"
	case "cmake":
		binaryName = "cmake"
	case "pkgconf":
		binaryName = "pkgconf"
	}

	// More robust install command with proper fallbacks
	installCmd := fmt.Sprintf(
		"if [ -f build/cmake_install.cmake ]; then DESTDIR={{pkgdir}} cmake --install build; "+
			"elif [ -f build/meson-private/coredata.dat ]; then DESTDIR={{pkgdir}} meson install -C build ${ZSVO_MESON_INSTALL_ARGS}; "+
			"elif [ -f Makefile ] || [ -f makefile ] || [ -f GNUmakefile ]; then make DESTDIR={{pkgdir}} PREFIX=/usr ${ZSVO_MAKE_INSTALL_FLAGS} install; "+
			"else "+
			"  # Try to find and install binaries manually "+
			"  bin_found=false; "+
			"  for bin_dir in src .; do "+
			"    if [ -f \"$$bin_dir/%s\" ]; then "+
			"      install -Dm755 \"$$bin_dir/%s\" \"{{pkgdir}}/usr/bin/%s\"; "+
			"      bin_found=true; "+
			"      break; "+
			"    fi; "+
			"  done; "+
			"  if [ \"$$bin_found\" = \"false\" ]; then "+
			"    echo \"Warning: No binary found for %s, installing common files\"; "+
			"    mkdir -p \"{{pkgdir}}/usr/bin\" \"{{pkgdir}}/usr/lib\" \"{{pkgdir}}/usr/include\" 2>/dev/null || true; "+
			"  fi; "+
			"fi",
		binaryName, binaryName, binaryName, name,
	)

	return &recipe.Recipe{
		Name:        name,
		Version:     version,
		Description: fmt.Sprintf("Auto-generated build recipe from Debian source %s", src.DSCURL),
		Source: recipe.Source{
			DebianDSC: src.DSCURL,
			Sha256:    src.DSCSHA256,
		},
		Build: []string{
			"if [ ! -f configure ] && [ -f autogen.sh ]; then sh ./autogen.sh --no-check ${ZSVO_AUTOGEN_ARGS}; fi; if [ -f configure ]; then ./configure --prefix=/usr ${ZSVO_CONFIGURE_FLAGS}; fi",
			"if [ -f CMakeLists.txt ]; then cmake -S . -B build -DCMAKE_BUILD_TYPE=Release -DCMAKE_INSTALL_PREFIX=/usr ${ZSVO_CMAKE_FLAGS} && cmake --build build -j${jobs} -- ${ZSVO_CMAKE_BUILD_FLAGS}; " +
				"elif [ -f meson.build ]; then meson setup build --prefix=/usr ${ZSVO_MESON_SETUP_ARGS} && meson compile -C build -j${jobs} ${ZSVO_MESON_COMPILE_ARGS}; " +
				"elif [ -f Makefile ] || [ -f makefile ] || [ -f GNUmakefile ]; then make -j${jobs} ${ZSVO_MAKE_FLAGS}; fi",
		},
		Install: []string{installCmd},
	}
}

// progressUI wrapper for compatibility
type progressUI struct {
	statusBar *ui.StatusBar
}

func newProgressUI(pkgName string) *progressUI {
	bar := ui.NewStatusBar(pkgName, 1)
	bar.SetTheme("neon")
	bar.SetSpinner("dots")
	return &progressUI{
		statusBar: bar,
	}
}

func (p *progressUI) update(step, total int, message string) {
	p.statusBar.Update(step, message)
}

func (p *progressUI) finish(ok bool, message string) {
	p.statusBar.Finish(ok, message)
}

var progressFrames = []string{"|", "/", "-", "\\"}

func renderColoredBar(width, filled int) string {
	if width <= 0 {
		return "[]"
	}

	if filled < 0 {
		filled = 0
	}
	if filled > width {
		filled = width
	}

	done := strings.Repeat("=", filled)
	todo := strings.Repeat(".", width-filled)
	return "[" + colorize("32", done) + colorize("2", todo) + "]"
}

func supportsANSIAndTTY() bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	term := strings.TrimSpace(strings.ToLower(os.Getenv("TERM")))
	if term == "" || term == "dumb" {
		return false
	}
	info, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return (info.Mode() & os.ModeCharDevice) != 0
}

func colorize(code, text string) string {
	if text == "" {
		return ""
	}
	return "\x1b[" + code + "m" + text + "\x1b[0m"
}

func formatElapsed(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	totalSeconds := int(d.Seconds())
	minutes := totalSeconds / 60
	seconds := totalSeconds % 60
	return fmt.Sprintf("%02d:%02d", minutes, seconds)
}

func truncateText(s string, max int) string {
	if max <= 3 || len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}

func visibleLen(s string) int {
	// This is enough here because we only inject ANSI codes ourselves.
	n := 0
	inEsc := false
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if inEsc {
			if ch == 'm' {
				inEsc = false
			}
			continue
		}
		if ch == 0x1b {
			inEsc = true
			continue
		}
		n++
	}
	return n
}

func buildFailureHint(pkgName string, err error) string {
	if err == nil {
		return ""
	}

	text := strings.ToLower(err.Error())
	pkgName = strings.TrimSpace(strings.ToLower(pkgName))
	hints := make([]string, 0, 4)

	missingDeps := inferMissingBuildDeps(err)
	if len(missingDeps) > 0 {
		hints = append(
			hints,
			fmt.Sprintf("Hint: missing build dependencies detected: %s.", strings.Join(missingDeps, ", ")),
			"Hint: добавь рецепты для этих пакетов (или алиасы к Debian source), затем повтори `zsvo install`.",
		)
	}
	if strings.Contains(text, "on systems using dpkg and apt, try: \"apt-get install package\"") {
		hints = append(hints,
			"Hint: upstream configure-script ожидает системные build-зависимости; в zsvo это нужно решать рецептами/автосборкой зависимостей.",
		)
	}

	if pkgName == "neovim" && strings.Contains(text, "lua") {
		hints = append(hints,
			"Hint (neovim): после установки Lua можно зафиксировать интерпретатор: `export ZSVO_CMAKE_FLAGS=\"-DLUA_PRG=$(which lua) -DLUA_GEN_PRG=$(which lua)\"`.",
		)
	}

	if len(hints) == 0 {
		return ""
	}

	return strings.Join(hints, "\n")
}

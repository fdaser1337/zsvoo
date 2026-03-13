package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"zsvo/pkg/builder"
	"zsvo/pkg/debian"
	"zsvo/pkg/installer"
	"zsvo/pkg/recipe"
)

var InstallCmd = &cobra.Command{
	Use:   "install <package> [package...]",
	Short: "Install package(s)",
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

		installTargets := make([]string, 0, len(args))
		var resolver *debian.Resolver
		var b *builder.Builder
		if autoSource {
			resolver = debian.NewResolver()
			b = builder.NewBuilder(workDir)
		}

		for _, target := range args {
			isFile, err := isInstallFileTarget(target)
			if err != nil {
				return err
			}

			if isFile {
				installTargets = append(installTargets, target)
				continue
			}

			if !autoSource {
				return fmt.Errorf(
					"%s is not a package file; pass a file path or enable --auto-source",
					target,
				)
			}

			fmt.Printf("Resolving Debian source for %s...\n", target)
			srcInfo, err := resolver.ResolveSource(target)
			if err != nil {
				return err
			}

			rcp := autoRecipeFromDebian(srcInfo)
			fmt.Printf("Building %s from %s...\n", rcp.GetPackageName(), srcInfo.DSCURL)
			if err := b.Build(rcp); err != nil {
				return fmt.Errorf("failed to auto-build %s: %w", target, err)
			}

			builtPackage := filepath.Join(rcp.GetPackageDir(workDir), rcp.GetPackageFileName())
			fmt.Printf("Built package: %s\n", builtPackage)
			installTargets = append(installTargets, builtPackage)
		}

		i := installer.NewInstaller(rootDir)

		if len(installTargets) == 1 {
			fmt.Printf("Installing package from %s...\n", installTargets[0])
		} else {
			fmt.Printf("Installing %d packages...\n", len(installTargets))
		}
		if err := i.InstallMany(installTargets); err != nil {
			return fmt.Errorf("failed to install packages: %w", err)
		}

		fmt.Printf("Package installation completed successfully\n")
		return nil
	},
}

func init() {
	InstallCmd.Flags().StringP("root", "r", "/", "Root directory for installation")
	InstallCmd.Flags().StringP("work-dir", "w", "/tmp/pkg-work", "Working directory for source builds")
	InstallCmd.Flags().Bool("auto-source", true, "Auto-build package names from Debian source")
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

func autoRecipeFromDebian(src *debian.SourceInfo) *recipe.Recipe {
	name := src.SourcePackage
	if name == "" {
		name = src.RequestedPackage
	}
	version := src.UpstreamVersion
	if version == "" {
		version = "0"
	}

	installCmd := fmt.Sprintf(
		"if [ -f build/cmake_install.cmake ]; then DESTDIR={{pkgdir}} cmake --install build; "+
			"elif [ -f build/meson-private/coredata.dat ]; then DESTDIR={{pkgdir}} meson install -C build; "+
			"elif [ -f Makefile ] || [ -f makefile ] || [ -f GNUmakefile ]; then make DESTDIR={{pkgdir}} PREFIX=/usr install; "+
			"elif [ -f %s ]; then install -Dm755 %s {{pkgdir}}/usr/bin/%s; fi",
		name,
		name,
		name,
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
			"[ -f configure ] && ./configure --prefix=/usr || true",
			"if [ -f CMakeLists.txt ]; then cmake -S . -B build -DCMAKE_BUILD_TYPE=Release -DCMAKE_INSTALL_PREFIX=/usr && cmake --build build -j${jobs}; " +
				"elif [ -f meson.build ]; then meson setup build --prefix=/usr && meson compile -C build -j${jobs}; " +
				"elif [ -f Makefile ] || [ -f makefile ] || [ -f GNUmakefile ]; then make -j${jobs}; fi",
		},
		Install: []string{installCmd},
	}
}

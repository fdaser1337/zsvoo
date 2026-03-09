package recipe

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// Recipe represents a package recipe
type Recipe struct {
	Name         string          `toml:"name"`
	Version      string          `toml:"version"`
	Description  string          `toml:"description,omitempty"`
	Source       Source          `toml:"source"`
	Build        Build           `toml:"build"`
	Package      Package         `toml:"package"`
	Dependencies []string        `toml:"dependencies,omitempty"`
	Options      map[string]bool `toml:"options,omitempty"`
}

// Source represents source information
type Source struct {
	URL     string   `toml:"url"`
	Sha256  string   `toml:"sha256"`
	Patches []string `toml:"patches,omitempty"`
}

// Build represents build configuration
type Build struct {
	Commands []string `toml:"commands"`
	Env      []string `toml:"env,omitempty"`
}

// Package represents package configuration
type Package struct {
	Commands []string `toml:"commands"`
}

// ParseRecipe parses a recipe from a TOML file
func ParseRecipe(path string) (*Recipe, error) {
	// Validate input path
	if path == "" {
		return nil, fmt.Errorf("recipe path cannot be empty")
	}

	// Clean and normalize path
	path = filepath.Clean(path)

	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open recipe file %s: %w", path, err)
	}
	defer file.Close()

	return ParseRecipeFromReader(file)
}

// ParseRecipeFromReader parses a recipe from an io.Reader
func ParseRecipeFromReader(r io.Reader) (*Recipe, error) {
	var recipe Recipe
	if _, err := toml.NewDecoder(r).Decode(&recipe); err != nil {
		return nil, fmt.Errorf("failed to parse recipe: %w", err)
	}

	if recipe.Name == "" {
		return nil, fmt.Errorf("recipe must have a name")
	}
	if recipe.Version == "" {
		return nil, fmt.Errorf("recipe must have a version")
	}
	if recipe.Source.URL == "" {
		return nil, fmt.Errorf("recipe must have a source URL")
	}
	if recipe.Source.Sha256 == "" {
		return nil, fmt.Errorf("recipe must have a source SHA256 checksum")
	}
	if len(recipe.Build.Commands) == 0 {
		return nil, fmt.Errorf("recipe must have build commands")
	}

	return &recipe, nil
}

// GetPackageName returns the package name in format name-version
func (r *Recipe) GetPackageName() string {
	return fmt.Sprintf("%s-%s", r.Name, r.Version)
}

// GetPackageFileName returns the package file name
func (r *Recipe) GetPackageFileName() string {
	return fmt.Sprintf("%s.pkg.tar.zst", r.GetPackageName())
}

// GetPackageDir returns the package directory path
func (r *Recipe) GetPackageDir(baseDir string) string {
	return filepath.Join(baseDir, "packages", r.GetPackageName())
}

// GetSourceDir returns the source directory path
func (r *Recipe) GetSourceDir(baseDir string) string {
	return filepath.Join(baseDir, "sources", r.GetPackageName())
}

// GetStagingDir returns the staging directory path
func (r *Recipe) GetStagingDir(baseDir string) string {
	return filepath.Join(baseDir, "staging", r.GetPackageName())
}

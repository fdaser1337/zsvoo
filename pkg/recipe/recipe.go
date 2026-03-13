package recipe

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"zsvo/pkg/deps"
)

// Recipe represents a package recipe.
type Recipe struct {
	Name        string
	Version     string
	Description string
	Source      Source
	Build       []string
	Install     []string
	Deps        []string
	Dir         string
}

// Source represents source information.
type Source struct {
	URL       string
	Sha256    string
	DebianDSC string
	Patches   []string
}

// ParseRecipe parses a recipe from a YAML file.
func ParseRecipe(path string) (*Recipe, error) {
	if path == "" {
		return nil, fmt.Errorf("recipe path cannot be empty")
	}

	path = filepath.Clean(path)
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open recipe file %s: %w", path, err)
	}
	defer file.Close()

	rcp, err := ParseRecipeFromReader(file)
	if err != nil {
		return nil, err
	}
	rcp.Dir = filepath.Dir(path)
	return rcp, nil
}

// ParseRecipeFromReader parses a recipe from an io.Reader.
func ParseRecipeFromReader(r io.Reader) (*Recipe, error) {
	rcp := &Recipe{}

	scanner := bufio.NewScanner(r)
	section := ""
	sourceSubsection := ""
	lineNo := 0

	for scanner.Scan() {
		lineNo++
		raw := strings.TrimRight(scanner.Text(), "\r")
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		indent := countLeadingSpaces(raw)

		if strings.HasPrefix(trimmed, "- ") {
			item := decodeYAMLScalar(strings.TrimSpace(trimmed[2:]))
			if item == "" {
				return nil, fmt.Errorf("invalid empty list item at line %d", lineNo)
			}

			switch section {
			case "build":
				rcp.Build = append(rcp.Build, item)
			case "install":
				rcp.Install = append(rcp.Install, item)
			case "deps":
				rcp.Deps = append(rcp.Deps, item)
			case "source":
				if sourceSubsection != "patches" {
					return nil, fmt.Errorf("unexpected source list item at line %d", lineNo)
				}
				rcp.Source.Patches = append(rcp.Source.Patches, item)
			default:
				return nil, fmt.Errorf("list item outside section at line %d", lineNo)
			}
			continue
		}

		if strings.HasSuffix(trimmed, ":") {
			key := strings.TrimSpace(strings.TrimSuffix(trimmed, ":"))
			if key == "" {
				return nil, fmt.Errorf("invalid key at line %d", lineNo)
			}

			if indent == 0 {
				section = key
				sourceSubsection = ""
				continue
			}

			if section == "source" && indent >= 2 {
				sourceSubsection = key
				continue
			}

			return nil, fmt.Errorf("unsupported nested section at line %d", lineNo)
		}

		key, val, ok := splitYAMLKeyValue(trimmed)
		if !ok {
			return nil, fmt.Errorf("invalid recipe line %d: %s", lineNo, trimmed)
		}
		value := decodeYAMLScalar(val)

		if indent == 0 {
			section = ""
			sourceSubsection = ""
			switch key {
			case "name":
				rcp.Name = value
			case "version":
				rcp.Version = value
			case "description":
				rcp.Description = value
			case "source", "build", "install", "deps":
				// Allowed as section headers only.
				return nil, fmt.Errorf("section %q must be a block (line %d)", key, lineNo)
			default:
				return nil, fmt.Errorf("unknown top-level key %q at line %d", key, lineNo)
			}
			continue
		}

		if section == "source" {
			sourceSubsection = ""
			switch key {
			case "url":
				rcp.Source.URL = value
			case "sha256":
				rcp.Source.Sha256 = value
			case "debian_dsc", "dsc_url":
				rcp.Source.DebianDSC = value
			case "patches":
				return nil, fmt.Errorf("source.patches must be a list (line %d)", lineNo)
			default:
				return nil, fmt.Errorf("unknown source key %q at line %d", key, lineNo)
			}
			continue
		}

		return nil, fmt.Errorf("unexpected nested key at line %d", lineNo)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("failed to read recipe: %w", err)
	}

	if rcp.Name == "" {
		return nil, fmt.Errorf("recipe must have a name")
	}
	if rcp.Version == "" {
		return nil, fmt.Errorf("recipe must have a version")
	}
	if rcp.Source.URL != "" && rcp.Source.DebianDSC != "" {
		return nil, fmt.Errorf("source.url and source.debian_dsc are mutually exclusive")
	}

	sourceURL := strings.TrimSpace(rcp.Source.URL)
	if rcp.Source.DebianDSC != "" {
		if !isDebianDSCURL(rcp.Source.DebianDSC) {
			return nil, fmt.Errorf("source.debian_dsc must point to a .dsc file")
		}
		sourceURL = strings.TrimSpace(rcp.Source.DebianDSC)
	}

	if sourceURL == "" {
		return nil, fmt.Errorf("recipe must have source.url or source.debian_dsc")
	}

	// SHA256 is mandatory for direct archives, optional for Debian .dsc flow.
	if rcp.Source.Sha256 == "" && !isDebianDSCURL(sourceURL) {
		return nil, fmt.Errorf("recipe must have a source SHA256 checksum")
	}
	if len(rcp.Build) == 0 {
		return nil, fmt.Errorf("recipe must have build commands")
	}
	if len(rcp.Install) == 0 {
		return nil, fmt.Errorf("recipe must have install commands")
	}

	for _, dep := range rcp.Deps {
		dep = strings.TrimSpace(dep)
		if dep == "" {
			return nil, fmt.Errorf("recipe deps cannot contain empty values")
		}
		if _, err := deps.ParseRequirement(dep); err != nil {
			return nil, fmt.Errorf("invalid recipe dependency %q: %w", dep, err)
		}
	}

	return rcp, nil
}

func splitYAMLKeyValue(line string) (string, string, bool) {
	idx := strings.Index(line, ":")
	if idx <= 0 {
		return "", "", false
	}
	key := strings.TrimSpace(line[:idx])
	val := strings.TrimSpace(line[idx+1:])
	if key == "" {
		return "", "", false
	}
	return key, val, true
}

func decodeYAMLScalar(v string) string {
	v = strings.TrimSpace(v)
	if len(v) >= 2 {
		if (v[0] == '"' && v[len(v)-1] == '"') || (v[0] == '\'' && v[len(v)-1] == '\'') {
			return v[1 : len(v)-1]
		}
	}
	return v
}

func countLeadingSpaces(s string) int {
	count := 0
	for _, ch := range s {
		if ch != ' ' {
			break
		}
		count++
	}
	return count
}

func isDebianDSCURL(raw string) bool {
	raw = strings.TrimSpace(strings.ToLower(raw))
	if raw == "" {
		return false
	}
	if idx := strings.IndexAny(raw, "?#"); idx >= 0 {
		raw = raw[:idx]
	}
	return strings.HasSuffix(raw, ".dsc")
}

// GetPackageName returns the package name in format name-version.
func (r *Recipe) GetPackageName() string {
	return fmt.Sprintf("%s-%s", r.Name, r.Version)
}

// GetPackageFileName returns the package file name.
func (r *Recipe) GetPackageFileName() string {
	return fmt.Sprintf("%s.pkg.tar.zst", r.GetPackageName())
}

// GetPackageDir returns the package directory path.
func (r *Recipe) GetPackageDir(baseDir string) string {
	return filepath.Join(baseDir, "packages", r.GetPackageName())
}

// GetSourceDir returns the source directory path.
func (r *Recipe) GetSourceDir(baseDir string) string {
	return filepath.Join(baseDir, "sources", r.GetPackageName())
}

// GetStagingDir returns the staging directory path.
func (r *Recipe) GetStagingDir(baseDir string) string {
	return filepath.Join(baseDir, "staging", r.GetPackageName())
}

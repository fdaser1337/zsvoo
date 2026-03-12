package types

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

// Validate ensures the package metadata is usable.
func (p *PkgInfo) Validate() error {
	if p == nil {
		return fmt.Errorf("package info cannot be nil")
	}
	if strings.TrimSpace(p.Name) == "" {
		return fmt.Errorf("package info: missing name")
	}
	if strings.TrimSpace(p.Version) == "" {
		return fmt.Errorf("package info: missing version")
	}
	for _, dep := range p.Dependencies {
		if strings.TrimSpace(dep) == "" {
			return fmt.Errorf("package info: empty dependency")
		}
	}
	for _, file := range p.Files {
		if strings.TrimSpace(file) == "" {
			return fmt.Errorf("package info: empty file path")
		}
	}
	return nil
}

// WritePkgInfo encodes package metadata as YAML.
func WritePkgInfo(w io.Writer, p *PkgInfo) error {
	if err := p.Validate(); err != nil {
		return err
	}

	if _, err := fmt.Fprintf(w, "name: %s\n", yamlScalar(p.Name)); err != nil {
		return fmt.Errorf("encode pkginfo: %w", err)
	}
	if _, err := fmt.Fprintf(w, "version: %s\n", yamlScalar(p.Version)); err != nil {
		return fmt.Errorf("encode pkginfo: %w", err)
	}
	if strings.TrimSpace(p.Description) != "" {
		if _, err := fmt.Fprintf(w, "description: %s\n", yamlScalar(p.Description)); err != nil {
			return fmt.Errorf("encode pkginfo: %w", err)
		}
	}

	if len(p.Dependencies) > 0 {
		if _, err := io.WriteString(w, "deps:\n"); err != nil {
			return fmt.Errorf("encode pkginfo: %w", err)
		}
		for _, dep := range p.Dependencies {
			if _, err := fmt.Fprintf(w, "  - %s\n", yamlScalar(dep)); err != nil {
				return fmt.Errorf("encode pkginfo: %w", err)
			}
		}
	}

	if len(p.Files) > 0 {
		if _, err := io.WriteString(w, "files:\n"); err != nil {
			return fmt.Errorf("encode pkginfo: %w", err)
		}
		for _, file := range p.Files {
			if _, err := fmt.Fprintf(w, "  - %s\n", yamlScalar(file)); err != nil {
				return fmt.Errorf("encode pkginfo: %w", err)
			}
		}
	}

	if strings.TrimSpace(p.InstallDate) != "" {
		if _, err := fmt.Fprintf(w, "install_date: %s\n", yamlScalar(p.InstallDate)); err != nil {
			return fmt.Errorf("encode pkginfo: %w", err)
		}
	}

	return nil
}

// ReadPkgInfo decodes package metadata from YAML.
func ReadPkgInfo(r io.Reader) (*PkgInfo, error) {
	var p PkgInfo

	scanner := bufio.NewScanner(r)
	section := ""
	lineNo := 0

	for scanner.Scan() {
		lineNo++
		raw := strings.TrimRight(scanner.Text(), "\r")
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		if strings.HasPrefix(trimmed, "- ") {
			item := yamlDecodeScalar(strings.TrimSpace(trimmed[2:]))
			if item == "" {
				return nil, fmt.Errorf("decode pkginfo: empty list item at line %d", lineNo)
			}
			switch section {
			case "deps":
				p.Dependencies = append(p.Dependencies, item)
			case "files":
				p.Files = append(p.Files, item)
			default:
				return nil, fmt.Errorf("decode pkginfo: list item outside section at line %d", lineNo)
			}
			continue
		}

		if strings.HasSuffix(trimmed, ":") {
			key := strings.TrimSpace(strings.TrimSuffix(trimmed, ":"))
			switch key {
			case "deps", "files":
				section = key
				continue
			default:
				return nil, fmt.Errorf("decode pkginfo: unsupported block key %q at line %d", key, lineNo)
			}
		}

		key, val, ok := splitPkgInfoKeyValue(trimmed)
		if !ok {
			return nil, fmt.Errorf("decode pkginfo: invalid line %d", lineNo)
		}
		section = ""
		decoded := yamlDecodeScalar(val)

		switch key {
		case "name":
			p.Name = decoded
		case "version":
			p.Version = decoded
		case "description":
			p.Description = decoded
		case "install_date":
			p.InstallDate = decoded
		default:
			return nil, fmt.Errorf("decode pkginfo: unknown key %q at line %d", key, lineNo)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("decode pkginfo: %w", err)
	}

	if err := p.Validate(); err != nil {
		return nil, err
	}
	return &p, nil
}

func yamlScalar(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return `""`
	}
	v = strings.ReplaceAll(v, `\`, `\\`)
	v = strings.ReplaceAll(v, `"`, `\"`)
	return `"` + v + `"`
}

func yamlDecodeScalar(v string) string {
	v = strings.TrimSpace(v)
	if len(v) >= 2 && v[0] == '"' && v[len(v)-1] == '"' {
		v = v[1 : len(v)-1]
		v = strings.ReplaceAll(v, `\"`, `"`)
		v = strings.ReplaceAll(v, `\\`, `\`)
		return v
	}
	if len(v) >= 2 && v[0] == '\'' && v[len(v)-1] == '\'' {
		return v[1 : len(v)-1]
	}
	return v
}

func splitPkgInfoKeyValue(line string) (string, string, bool) {
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

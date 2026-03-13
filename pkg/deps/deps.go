package deps

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

// VersionOp defines version comparison operator for dependency constraints.
type VersionOp int

const (
	OpAny VersionOp = iota
	OpEqual
	OpGreater
	OpGreaterOrEqual
	OpLess
	OpLessOrEqual
)

// Constraint describes one dependency alternative like "pkg>=1.2".
type Constraint struct {
	Name    string
	Op      VersionOp
	Version string
}

// Requirement describes one dependency expression with alternatives: "a | b>=2".
type Requirement struct {
	Raw          string
	Alternatives []Constraint
}

// ParseRequirements parses dependency expressions.
func ParseRequirements(raw []string) ([]Requirement, error) {
	reqs := make([]Requirement, 0, len(raw))
	for _, entry := range raw {
		req, err := ParseRequirement(entry)
		if err != nil {
			return nil, err
		}
		reqs = append(reqs, req)
	}
	return reqs, nil
}

// ParseRequirement parses one dependency expression.
func ParseRequirement(raw string) (Requirement, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return Requirement{}, fmt.Errorf("dependency cannot be empty")
	}

	parts := strings.Split(raw, "|")
	alts := make([]Constraint, 0, len(parts))
	for _, part := range parts {
		c, err := parseConstraint(part)
		if err != nil {
			return Requirement{}, fmt.Errorf("invalid dependency %q: %w", raw, err)
		}
		alts = append(alts, c)
	}
	if len(alts) == 0 {
		return Requirement{}, fmt.Errorf("invalid dependency %q", raw)
	}

	return Requirement{
		Raw:          raw,
		Alternatives: alts,
	}, nil
}

func parseConstraint(raw string) (Constraint, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return Constraint{}, fmt.Errorf("empty alternative")
	}

	// Debian style: "pkg (>= 1.2)".
	if open := strings.IndexByte(raw, '('); open >= 0 {
		close := strings.LastIndexByte(raw, ')')
		if close < open || close != len(raw)-1 {
			return Constraint{}, fmt.Errorf("invalid parentheses syntax")
		}

		name := strings.TrimSpace(raw[:open])
		if !isValidDepName(name) {
			return Constraint{}, fmt.Errorf("invalid package name %q", name)
		}

		inner := strings.TrimSpace(raw[open+1 : close])
		op, version, err := splitVersionConstraint(inner)
		if err != nil {
			return Constraint{}, err
		}
		return Constraint{Name: name, Op: op, Version: version}, nil
	}

	// Generic style: "pkg>=1.2" or "pkg >= 1.2" or plain "pkg".
	name, op, version, err := splitNameAndConstraint(raw)
	if err != nil {
		return Constraint{}, err
	}
	return Constraint{Name: name, Op: op, Version: version}, nil
}

func splitNameAndConstraint(raw string) (string, VersionOp, string, error) {
	name := strings.TrimSpace(raw)
	op := OpAny
	version := ""

	idx, opFound := findOp(raw)
	if opFound {
		name = strings.TrimSpace(raw[:idx])
		right := strings.TrimSpace(raw[idx:])
		var err error
		op, version, err = splitVersionConstraint(right)
		if err != nil {
			return "", OpAny, "", err
		}
	}

	if !isValidDepName(name) {
		return "", OpAny, "", fmt.Errorf("invalid package name %q", name)
	}
	return name, op, version, nil
}

func findOp(raw string) (int, bool) {
	for i, r := range raw {
		switch r {
		case '<', '>', '=':
			return i, true
		}
	}
	return 0, false
}

func splitVersionConstraint(raw string) (VersionOp, string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return OpAny, "", fmt.Errorf("empty version constraint")
	}

	opToken := ""
	version := ""

	switch {
	case strings.HasPrefix(raw, ">="):
		opToken = ">="
		version = strings.TrimSpace(raw[2:])
	case strings.HasPrefix(raw, "<="):
		opToken = "<="
		version = strings.TrimSpace(raw[2:])
	case strings.HasPrefix(raw, "="):
		opToken = "="
		version = strings.TrimSpace(raw[1:])
	case strings.HasPrefix(raw, ">"):
		opToken = ">"
		version = strings.TrimSpace(raw[1:])
	case strings.HasPrefix(raw, "<"):
		opToken = "<"
		version = strings.TrimSpace(raw[1:])
	default:
		parts := strings.Fields(raw)
		if len(parts) == 2 {
			opToken = parts[0]
			version = parts[1]
		}
	}

	if opToken == "" || version == "" {
		return OpAny, "", fmt.Errorf("invalid version constraint %q", raw)
	}
	if strings.HasPrefix(version, "<") || strings.HasPrefix(version, ">") || strings.HasPrefix(version, "=") {
		return OpAny, "", fmt.Errorf("invalid version %q", version)
	}
	if strings.ContainsAny(version, "()|") {
		return OpAny, "", fmt.Errorf("invalid version %q", version)
	}

	op, err := parseVersionOp(opToken)
	if err != nil {
		return OpAny, "", err
	}
	return op, version, nil
}

func parseVersionOp(raw string) (VersionOp, error) {
	switch strings.TrimSpace(raw) {
	case "=":
		return OpEqual, nil
	case ">":
		return OpGreater, nil
	case ">=":
		return OpGreaterOrEqual, nil
	case "<":
		return OpLess, nil
	case "<=":
		return OpLessOrEqual, nil
	default:
		return OpAny, fmt.Errorf("unsupported operator %q", raw)
	}
}

func isValidDepName(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}
	for i, r := range name {
		if i == 0 {
			if !unicode.IsDigit(r) && !unicode.IsLetter(r) {
				return false
			}
			continue
		}
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			continue
		}
		switch r {
		case '+', '.', '-', '_':
		default:
			return false
		}
	}
	return true
}

// MatchesVersion checks whether package version satisfies this constraint.
func (c Constraint) MatchesVersion(installedVersion string) bool {
	switch c.Op {
	case OpAny:
		return true
	case OpEqual:
		return CompareVersions(installedVersion, c.Version) == 0
	case OpGreater:
		return CompareVersions(installedVersion, c.Version) > 0
	case OpGreaterOrEqual:
		return CompareVersions(installedVersion, c.Version) >= 0
	case OpLess:
		return CompareVersions(installedVersion, c.Version) < 0
	case OpLessOrEqual:
		return CompareVersions(installedVersion, c.Version) <= 0
	default:
		return false
	}
}

// CompareVersions compares package versions.
// Returns -1 if a < b, 0 if a == b, and 1 if a > b.
func CompareVersions(a, b string) int {
	epochA, restA := splitEpoch(a)
	epochB, restB := splitEpoch(b)
	if epochA != epochB {
		if epochA < epochB {
			return -1
		}
		return 1
	}

	mainA, relA := splitRelease(restA)
	mainB, relB := splitRelease(restB)

	if c := compareVersionPart(mainA, mainB); c != 0 {
		return c
	}
	return compareVersionPart(relA, relB)
}

func splitEpoch(raw string) (int64, string) {
	raw = strings.TrimSpace(raw)
	if idx := strings.IndexByte(raw, ':'); idx > 0 {
		epochRaw := strings.TrimSpace(raw[:idx])
		if epoch, err := strconv.ParseInt(epochRaw, 10, 64); err == nil {
			return epoch, strings.TrimSpace(raw[idx+1:])
		}
	}
	return 0, raw
}

func splitRelease(raw string) (string, string) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "0", "0"
	}

	if idx := strings.LastIndex(raw, "-"); idx > 0 && idx < len(raw)-1 {
		return raw[:idx], raw[idx+1:]
	}
	return raw, "0"
}

func compareVersionPart(a, b string) int {
	a = strings.TrimSpace(a)
	b = strings.TrimSpace(b)

	i, j := 0, 0
	for i < len(a) || j < len(b) {
		// Handle tilde comparison - tilde sorts before everything
		aHasTilde := i < len(a) && a[i] == '~'
		bHasTilde := j < len(b) && b[j] == '~'
		
		switch {
		case aHasTilde && bHasTilde:
			// Both have tilde, compare the rest
			i++
			j++
			continue
		case aHasTilde:
			return -1 // a has tilde, sorts before b
		case bHasTilde:
			return 1  // b has tilde, sorts before a
		}

		// Skip separators
		for i < len(a) && isVersionSeparator(a[i]) {
			i++
		}
		for j < len(b) && isVersionSeparator(b[j]) {
			j++
		}

		if i >= len(a) && j >= len(b) {
			return 0
		}
		if i >= len(a) {
			return -1
		}
		if j >= len(b) {
			return 1
		}

		aNum := isDigit(a[i])
		bNum := isDigit(b[j])

		segA, nextI := readVersionSegment(a, i, aNum)
		segB, nextJ := readVersionSegment(b, j, bNum)
		i, j = nextI, nextJ

		var c int
		switch {
		case aNum && bNum:
			c = compareNumericSegment(segA, segB)
		case aNum && !bNum:
			c = 1
		case !aNum && bNum:
			c = -1
		default:
			c = strings.Compare(strings.ToLower(segA), strings.ToLower(segB))
		}
		if c < 0 {
			return -1
		}
		if c > 0 {
			return 1
		}
	}

	return 0
}

func isVersionSeparator(ch byte) bool {
	if ch == '~' {
		return false
	}
	return !isDigit(ch) && !isLetter(ch)
}

func readVersionSegment(s string, start int, numeric bool) (string, int) {
	i := start
	for i < len(s) {
		ch := s[i]
		if numeric {
			if !isDigit(ch) {
				break
			}
		} else {
			if !isLetter(ch) {
				break
			}
		}
		i++
	}
	return s[start:i], i
}

func compareNumericSegment(a, b string) int {
	a = strings.TrimLeft(a, "0")
	b = strings.TrimLeft(b, "0")
	if a == "" {
		a = "0"
	}
	if b == "" {
		b = "0"
	}

	if len(a) < len(b) {
		return -1
	}
	if len(a) > len(b) {
		return 1
	}
	return strings.Compare(a, b)
}

func isDigit(ch byte) bool {
	return ch >= '0' && ch <= '9'
}

func isLetter(ch byte) bool {
	return ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z'
}

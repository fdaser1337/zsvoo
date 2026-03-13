package security

import (
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"unicode"

	"zsvo/pkg/errors"
)

// PathValidator handles path validation and security
type PathValidator struct {
	allowedPaths []string
	strictMode   bool
}

// NewPathValidator creates a new path validator
func NewPathValidator(allowedPaths []string, strictMode bool) *PathValidator {
	return &PathValidator{
		allowedPaths: allowedPaths,
		strictMode:   strictMode,
	}
}

// ValidatePath validates a path for security issues
func (v *PathValidator) ValidatePath(path string) error {
	// Clean the path
	cleanPath := filepath.Clean(path)
	
	// Check for path traversal attempts
	if strings.Contains(cleanPath, "..") {
		return errors.NewInvalidPathError(path)
	}
	
	// Check for absolute paths (should be relative to package root)
	if filepath.IsAbs(cleanPath) && v.strictMode {
		return errors.NewInvalidPathError(path)
	}
	
	// Check for suspicious patterns
	if v.hasSuspiciousPatterns(cleanPath) {
		return errors.NewInvalidPathError(path)
	}
	
	// Check if path is within allowed directories
	if len(v.allowedPaths) > 0 {
		if !v.isPathAllowed(cleanPath) {
			return errors.NewInvalidPathError(path)
		}
	}
	
	// Cross-platform path validation
	if err := v.validateCrossPlatform(cleanPath); err != nil {
		return err
	}
	
	return nil
}

// SanitizePath sanitizes a path for safe use
func (v *PathValidator) SanitizePath(path string) string {
	// Remove any null bytes
	path = strings.ReplaceAll(path, "\x00", "")
	
	// Clean the path
	path = filepath.Clean(path)
	
	// Convert forward slashes to OS-specific separators
	path = filepath.FromSlash(path)
	
	// Remove consecutive separators
	for strings.Contains(path, string(filepath.Separator)+string(filepath.Separator)) {
		path = strings.ReplaceAll(path, string(filepath.Separator)+string(filepath.Separator), string(filepath.Separator))
	}
	
	return path
}

// ValidateFileName validates a filename for security
func (v *PathValidator) ValidateFileName(filename string) error {
	if filename == "" {
		return errors.NewInvalidPathError("empty filename")
	}
	
	// Check for reserved names
	if v.isReservedName(filename) {
		return errors.NewInvalidPathError(filename)
	}
	
	// Check for invalid characters
	if v.hasInvalidChars(filename) {
		return errors.NewInvalidPathError(filename)
	}
	
	// Check length limits
	if len(filename) > 255 {
		return errors.NewInvalidPathError(filename)
	}
	
	// Check for trailing whitespace
	if strings.HasSuffix(filename, " ") || strings.HasSuffix(filename, "\t") {
		return errors.NewInvalidPathError(filename)
	}
	
	return nil
}

// hasSuspiciousPatterns checks for suspicious path patterns
func (v *PathValidator) hasSuspiciousPatterns(path string) bool {
	suspicious := []string{
		"../",
		"..\\",
		"$",
		"<",
		">",
		"|",
		"\"",
	}
	
	pathLower := strings.ToLower(path)
	for _, pattern := range suspicious {
		if strings.Contains(pathLower, pattern) {
			return true
		}
	}
	
	// Check for regex patterns (but allow ~ for home directories)
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`^\.+$`),           // Hidden files with only dots
		regexp.MustCompile(`[^\x20-\x7E]`),    // Non-ASCII characters
		regexp.MustCompile(`\s+$`),             // Trailing whitespace
	}
	
	for _, pattern := range patterns {
		if pattern.MatchString(path) {
			return true
		}
	}
	
	return false
}

// isPathAllowed checks if path is within allowed directories
func (v *PathValidator) isPathAllowed(path string) bool {
	// If no allowed paths specified, allow all
	if len(v.allowedPaths) == 0 {
		return true
	}
	
	absPath, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	
	for _, allowed := range v.allowedPaths {
		allowedAbs, err := filepath.Abs(allowed)
		if err != nil {
			continue
		}
		
		if strings.HasPrefix(absPath+string(filepath.Separator), allowedAbs+string(filepath.Separator)) {
			return true
		}
		// Also check exact match
		if absPath == allowedAbs {
			return true
		}
	}
	
	return false
}

// validateCrossPlatform performs cross-platform validation
func (v *PathValidator) validateCrossPlatform(path string) error {
	// Windows-specific validations
	if runtime.GOOS == "windows" {
		return v.validateWindowsPath(path)
	}
	
	// Unix-specific validations
	return v.validateUnixPath(path)
}

// validateWindowsPath validates Windows-specific path issues
func (v *PathValidator) validateWindowsPath(path string) error {
	// Check for invalid Windows characters
	invalidChars := []string{"<", ">", ":", "\"", "|", "?", "*"}
	for _, char := range invalidChars {
		if strings.Contains(path, char) {
			return errors.NewInvalidPathError(path)
		}
	}
	
	// Check for reserved device names
	reserved := []string{
		"CON", "PRN", "AUX", "NUL",
		"COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9",
		"LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9",
	}
	
	pathUpper := strings.ToUpper(path)
	for _, name := range reserved {
		if strings.HasPrefix(pathUpper, name) {
			return errors.NewInvalidPathError(path)
		}
	}
	
	return nil
}

// validateUnixPath validates Unix-specific path issues
func (v *PathValidator) validateUnixPath(path string) error {
	// Unix paths shouldn't contain backslashes
	if strings.Contains(path, "\\") {
		return errors.NewInvalidPathError(path)
	}
	
	return nil
}

// isReservedName checks if filename is reserved
func (v *PathValidator) isReservedName(name string) bool {
	reserved := []string{
		"CON", "PRN", "AUX", "NUL",
		"COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9",
		"LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9",
	}
	
	nameUpper := strings.ToUpper(strings.TrimSuffix(name, filepath.Ext(name)))
	for _, reserved := range reserved {
		if nameUpper == reserved {
			return true
		}
	}
	
	return false
}

// hasInvalidChars checks for invalid characters in filename
func (v *PathValidator) hasInvalidChars(filename string) bool {
	// Control characters
	for _, r := range filename {
		if unicode.IsControl(r) && r != '\t' && r != '\n' && r != '\r' {
			return true
		}
	}
	
	// Platform-specific invalid characters
	if runtime.GOOS == "windows" {
		invalid := "<>:\"|?*"
		for _, char := range invalid {
			if strings.ContainsRune(filename, char) {
				return true
			}
		}
	}
	
	return false
}

// SafeJoin safely joins path components
func (v *PathValidator) SafeJoin(base, path string) (string, error) {
	// If path is empty, just return cleaned base
	if path == "" {
		cleaned := filepath.Clean(base)
		return cleaned, nil
	}
	
	// Validate the path component
	if err := v.ValidatePath(path); err != nil {
		return "", err
	}
	
	// Join paths
	joined := filepath.Join(base, path)
	
	// Clean and validate the result
	cleaned := filepath.Clean(joined)
	if err := v.ValidatePath(cleaned); err != nil {
		return "", err
	}
	
	return cleaned, nil
}

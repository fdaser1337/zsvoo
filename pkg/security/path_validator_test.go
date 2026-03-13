package security

import (
	"path/filepath"
	"runtime"
	"testing"
)

func TestPathValidator_ValidatePath(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		path      string
		strict    bool
		wantError bool
	}{
		{
			name:      "valid relative path",
			path:      "usr/bin/test",
			strict:    true,
			wantError: false,
		},
		{
			name:      "path traversal attempt",
			path:      "../../../etc/passwd",
			strict:    true,
			wantError: true,
		},
		{
			name:      "absolute path in strict mode",
			path:      "/usr/bin/test",
			strict:    true,
			wantError: true,
		},
		{
			name:      "absolute path in non-strict mode",
			path:      "/usr/bin/test",
			strict:    false,
			wantError: false,
		},
		{
			name:      "suspicious characters",
			path:      "test$HOME",
			strict:    true,
			wantError: true,
		},
		{
			name:      "hidden file with dots only",
			path:      "...",
			strict:    true,
			wantError: true,
		},
		{
			name:      "valid hidden file",
			path:      ".config",
			strict:    true,
			wantError: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			validator := NewPathValidator(nil, tc.strict)
			err := validator.ValidatePath(tc.path)
			gotError := err != nil
			
			if gotError != tc.wantError {
				t.Errorf("ValidatePath(%q) error = %v, wantError %v", tc.path, err, tc.wantError)
			}
		})
	}
}

func TestPathValidator_SanitizePath(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "normal path",
			input:    "usr/bin/test",
			expected: "usr/bin/test",
		},
		{
			name:     "path with null bytes",
			input:    "test\x00file",
			expected: "testfile",
		},
		{
			name:     "path with forward slashes",
			input:    "usr/bin/test",
			expected: joinOSPath("usr", "bin", "test"),
		},
		{
			name:     "path with consecutive separators",
			input:    "usr//bin///test",
			expected: joinOSPath("usr", "bin", "test"),
		},
		{
			name:     "path with . and ..",
			input:    "usr/./bin/../test",
			expected: joinOSPath("usr", "test"),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			validator := NewPathValidator(nil, true)
			result := validator.SanitizePath(tc.input)
			
			if result != tc.expected {
				t.Errorf("SanitizePath(%q) = %q, expected %q", tc.input, result, tc.expected)
			}
		})
	}
}

func TestPathValidator_ValidateFileName(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		filename  string
		wantError bool
	}{
		{
			name:      "valid filename",
			filename:  "test.txt",
			wantError: false,
		},
		{
			name:      "empty filename",
			filename:  "",
			wantError: true,
		},
		{
			name:      "filename too long",
			filename:  string(make([]byte, 256)),
			wantError: true,
		},
		{
			name:      "filename with trailing whitespace",
			filename:  "test   ",
			wantError: true,
		},
	}

	// Add platform-specific tests
	if runtime.GOOS == "windows" {
		cases = append(cases, []struct {
			name      string
			filename  string
			wantError bool
		}{
			{
				name:      "windows reserved name",
				filename:  "CON",
				wantError: true,
			},
			{
				name:      "windows invalid characters",
				filename:  "test<file",
				wantError: true,
			},
		}...)
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			validator := NewPathValidator(nil, true)
			err := validator.ValidateFileName(tc.filename)
			gotError := err != nil
			
			if gotError != tc.wantError {
				t.Errorf("ValidateFileName(%q) error = %v, wantError %v", tc.filename, err, tc.wantError)
			}
		})
	}
}

func TestPathValidator_SafeJoin(t *testing.T) {
	t.Parallel()

	validator := NewPathValidator(nil, false) // Non-strict mode
	
	cases := []struct {
		name      string
		base      string
		path      string
		wantError bool
		expected  string
	}{
		{
			name:      "valid join",
			base:      "/usr",
			path:      "bin/test",
			wantError: false,
			expected:  joinOSPath("/usr", "bin", "test"),
		},
		{
			name:      "path traversal attempt",
			base:      "/usr",
			path:      "../../../etc/passwd",
			wantError: true,
		},
		{
			name:      "empty path",
			base:      "/usr",
			path:      "",
			wantError: false,
			expected:  "/usr",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := validator.SafeJoin(tc.base, tc.path)
			
			if tc.wantError {
				if err == nil {
					t.Errorf("SafeJoin(%q, %q) expected error", tc.base, tc.path)
				}
			} else {
				if err != nil {
					t.Errorf("SafeJoin(%q, %q) unexpected error: %v", tc.base, tc.path, err)
				}
				if result != tc.expected {
					t.Errorf("SafeJoin(%q, %q) = %q, expected %q", tc.base, tc.path, result, tc.expected)
				}
			}
		})
	}
}

func TestPathValidator_AllowedPaths(t *testing.T) {
	t.Parallel()

	allowed := []string{"/tmp", "/var/tmp"}
	validator := NewPathValidator(allowed, false) // Non-strict mode for absolute paths
	
	cases := []struct {
		name      string
		path      string
		wantError bool
	}{
		{
			name:      "allowed path",
			path:      "/tmp/test",
			wantError: false,
		},
		{
			name:      "disallowed path",
			path:      "/etc/passwd",
			wantError: true,
		},
		{
			name:      "subdirectory of allowed path",
			path:      "/var/tmp/subdir/file",
			wantError: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validator.ValidatePath(tc.path)
			gotError := err != nil
			
			if gotError != tc.wantError {
				t.Errorf("ValidatePath(%q) error = %v, wantError %v", tc.path, err, tc.wantError)
			}
		})
	}
}

// Helper function for cross-platform path joining
func joinOSPath(parts ...string) string {
	if len(parts) == 0 {
		return ""
	}
	result := parts[0]
	for _, part := range parts[1:] {
		result += string(filepath.Separator) + part
	}
	return result
}

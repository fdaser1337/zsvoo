package errors

import (
	"errors"
	"testing"
)

func TestError_Error(t *testing.T) {
	t.Parallel()

	// Test error without cause
	err1 := NewPackageNotFoundError("testpkg")
	if err1.Error() != "package not found" {
		t.Errorf("Expected 'package not found', got %q", err1.Error())
	}

	// Test error with cause
	cause := errors.New("underlying error")
	err2 := WrapError(ErrCodeBuildFailed, "build failed", cause, map[string]string{"package": "testpkg"})
	expected := "build failed: underlying error"
	if err2.Error() != expected {
		t.Errorf("Expected %q, got %q", expected, err2.Error())
	}
}

func TestError_Unwrap(t *testing.T) {
	t.Parallel()

	cause := errors.New("cause")
	err := WrapError(ErrCodeTransactionFailed, "transaction failed", cause, nil)

	if err.Unwrap() != cause {
		t.Errorf("Expected unwrapped error to be the cause")
	}
}

func TestIsErrorCode(t *testing.T) {
	t.Parallel()

	err := NewPackageNotFoundError("testpkg")

	if !IsErrorCode(err, ErrCodePackageNotFound) {
		t.Errorf("Expected error to be ErrCodePackageNotFound")
	}

	if IsErrorCode(err, ErrCodeDependencyMissing) {
		t.Errorf("Expected error not to be ErrCodeDependencyMissing")
	}

	// Test with non-typed error
	plainErr := errors.New("plain error")
	if IsErrorCode(plainErr, ErrCodePackageNotFound) {
		t.Errorf("Expected plain error not to match any error code")
	}
}

func TestErrorConstructors(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		constructor func() *Error
		expectedCode ErrorCode
		expectedMessage string
	}{
		{
			name: "PackageNotFound",
			constructor: func() *Error { return NewPackageNotFoundError("testpkg") },
			expectedCode: ErrCodePackageNotFound,
			expectedMessage: "package not found",
		},
		{
			name: "DependencyMissing",
			constructor: func() *Error { return NewDependencyMissingError("pkg", "dep") },
			expectedCode: ErrCodeDependencyMissing,
			expectedMessage: "missing dependency",
		},
		{
			name: "VersionMismatch",
			constructor: func() *Error { return NewVersionMismatchError("pkg", "1.0", "0.9") },
			expectedCode: ErrCodeVersionMismatch,
			expectedMessage: "version mismatch",
		},
		{
			name: "FileConflict",
			constructor: func() *Error { return NewFileConflictError("pkg", "/path") },
			expectedCode: ErrCodeFileConflict,
			expectedMessage: "file conflict",
		},
		{
			name: "InvalidPath",
			constructor: func() *Error { return NewInvalidPathError("../../../etc/passwd") },
			expectedCode: ErrCodeInvalidPath,
			expectedMessage: "invalid path",
		},
		{
			name: "ChecksumMismatch",
			constructor: func() *Error { return NewChecksumMismatchError("file.tar.gz", "abc123", "def456") },
			expectedCode: ErrCodeChecksumMismatch,
			expectedMessage: "checksum mismatch",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.constructor()
			
			if err.Code != tc.expectedCode {
				t.Errorf("Expected error code %v, got %v", tc.expectedCode, err.Code)
			}
			
			if err.Message != tc.expectedMessage {
				t.Errorf("Expected message %q, got %q", tc.expectedMessage, err.Message)
			}
			
			if err.Context == nil {
				t.Errorf("Expected context to be set")
			}
		})
	}
}

func TestErrorContext(t *testing.T) {
	t.Parallel()

	err := NewPackageNotFoundError("testpkg")
	
	if err.Context["package"] != "testpkg" {
		t.Errorf("Expected package context to be 'testpkg', got %q", err.Context["package"])
	}
}

func TestErrorWrapping(t *testing.T) {
	t.Parallel()

	cause := errors.New("underlying")
	err := NewTransactionFailedError("install", cause)
	
	if err.Code != ErrCodeTransactionFailed {
		t.Errorf("Expected ErrCodeTransactionFailed, got %v", err.Code)
	}
	
	if err.Cause != cause {
		t.Errorf("Expected cause to be preserved")
	}
	
	if err.Context["operation"] != "install" {
		t.Errorf("Expected operation context to be 'install'")
	}
}

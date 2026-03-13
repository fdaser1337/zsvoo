package errors

import (
	"fmt"
)

// ErrorCode represents different types of errors
type ErrorCode int

const (
	ErrCodeUnknown ErrorCode = iota
	ErrCodePackageNotFound
	ErrCodeDependencyMissing
	ErrCodeDependencyConflict
	ErrCodeVersionMismatch
	ErrCodeFileConflict
	ErrCodeInvalidPath
	ErrCodeTransactionFailed
	ErrCodeBuildFailed
	ErrCodeChecksumMismatch
)

// Error represents a typed error with context
type Error struct {
	Code    ErrorCode
	Message string
	Context map[string]string
	Cause   error
}

func (e *Error) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: %v", e.Message, e.Cause)
	}
	return e.Message
}

func (e *Error) Unwrap() error {
	return e.Cause
}

// NewError creates a new typed error
func NewError(code ErrorCode, message string, context map[string]string) *Error {
	return &Error{
		Code:    code,
		Message: message,
		Context: context,
	}
}

// WrapError wraps an existing error with context
func WrapError(code ErrorCode, message string, cause error, context map[string]string) *Error {
	return &Error{
		Code:    code,
		Message: message,
		Context: context,
		Cause:   cause,
	}
}

// IsErrorCode checks if error matches specific code
func IsErrorCode(err error, code ErrorCode) bool {
	if typedErr, ok := err.(*Error); ok {
		return typedErr.Code == code
	}
	return false
}

// Common error constructors
func NewPackageNotFoundError(pkg string) *Error {
	return NewError(ErrCodePackageNotFound, "package not found", map[string]string{
		"package": pkg,
	})
}

func NewDependencyMissingError(pkg, dep string) *Error {
	return NewError(ErrCodeDependencyMissing, "missing dependency", map[string]string{
		"package":    pkg,
		"dependency": dep,
	})
}

func NewVersionMismatchError(pkg, required, installed string) *Error {
	return NewError(ErrCodeVersionMismatch, "version mismatch", map[string]string{
		"package":   pkg,
		"required":  required,
		"installed": installed,
	})
}

func NewFileConflictError(pkg, path string) *Error {
	return NewError(ErrCodeFileConflict, "file conflict", map[string]string{
		"package": pkg,
		"path":    path,
	})
}

func NewInvalidPathError(path string) *Error {
	return NewError(ErrCodeInvalidPath, "invalid path", map[string]string{
		"path": path,
	})
}

func NewTransactionFailedError(operation string, cause error) *Error {
	return WrapError(ErrCodeTransactionFailed, "transaction failed: "+operation, cause, map[string]string{
		"operation": operation,
	})
}

func NewBuildFailedError(pkg string, cause error) *Error {
	return WrapError(ErrCodeBuildFailed, "build failed", cause, map[string]string{
		"package": pkg,
	})
}

func NewChecksumMismatchError(file, expected, actual string) *Error {
	return NewError(ErrCodeChecksumMismatch, "checksum mismatch", map[string]string{
		"file":     file,
		"expected": expected,
		"actual":   actual,
	})
}

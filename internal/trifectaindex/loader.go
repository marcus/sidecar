package trifectaindex

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

type LoadErrorCode string

const (
	LoadErrorIndexMissing      LoadErrorCode = "INDEX_MISSING"
	LoadErrorInvalidJSON       LoadErrorCode = "INDEX_INVALID_JSON"
	LoadErrorSchemaUnsupported LoadErrorCode = "INDEX_SCHEMA_UNSUPPORTED"
	LoadErrorRepoMismatch      LoadErrorCode = "INDEX_REPO_MISMATCH"
	LoadErrorReadFailed        LoadErrorCode = "INDEX_READ_FAILED"
)

type LoadError struct {
	Code LoadErrorCode
	Path string
	Err  error
}

func (e *LoadError) Error() string {
	if e.Err == nil {
		return string(e.Code)
	}
	return fmt.Sprintf("%s: %v", e.Code, e.Err)
}

func (e *LoadError) Unwrap() error { return e.Err }

// LoadIndex reads and unmarshals a WOIndex from the given path
func LoadIndex(path string) (*WOIndex, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, &LoadError{Code: LoadErrorIndexMissing, Path: path, Err: err}
		}
		return nil, &LoadError{Code: LoadErrorReadFailed, Path: path, Err: err}
	}
	var idx WOIndex
	if err := json.Unmarshal(data, &idx); err != nil {
		return nil, &LoadError{Code: LoadErrorInvalidJSON, Path: path, Err: err}
	}
	return &idx, nil
}

// LoadAndValidate loads the index and verifies schema + repo scope.
func LoadAndValidate(path string, expectedRepoRoot string) (*WOIndex, error) {
	idx, err := LoadIndex(path)
	if err != nil {
		return nil, err
	}
	if err := idx.Validate(); err != nil {
		return nil, &LoadError{Code: LoadErrorSchemaUnsupported, Path: path, Err: err}
	}
	if expectedRepoRoot != "" {
		idxRoot, err := canonicalPath(idx.RepoRoot)
		if err != nil {
			return nil, &LoadError{Code: LoadErrorRepoMismatch, Path: path, Err: err}
		}
		expected, err := canonicalPath(expectedRepoRoot)
		if err != nil {
			return nil, &LoadError{Code: LoadErrorRepoMismatch, Path: path, Err: err}
		}
		if idxRoot != expected {
			return nil, &LoadError{
				Code: LoadErrorRepoMismatch,
				Path: path,
				Err:  fmt.Errorf("index repo_root=%s expected=%s", idx.RepoRoot, expectedRepoRoot),
			}
		}
	}
	return idx, nil
}

func canonicalPath(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("abs path failed for %q: %w", path, err)
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		// fallback controlled: for non-existent paths, use cleaned absolute
		if errors.Is(err, os.ErrNotExist) {
			return filepath.Clean(abs), nil
		}
		return "", fmt.Errorf("eval symlinks failed for %q: %w", abs, err)
	}
	return filepath.Clean(resolved), nil
}

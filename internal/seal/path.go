package seal

import (
	"fmt"
	pathpkg "path"
	"path/filepath"
	"strings"
)

// NormalizePath converts rawPath to a clean repository-relative path.
// rawPath must be relative and is interpreted relative to the repository root.
func NormalizePath(repoRoot, rawPath string) (string, error) {
	if filepath.IsAbs(rawPath) {
		return "", fmt.Errorf("path %q must be relative to the repository root", rawPath)
	}
	abs := filepath.Clean(filepath.Join(repoRoot, filepath.FromSlash(rawPath)))
	rel, err := filepath.Rel(repoRoot, abs)
	if err != nil {
		return "", fmt.Errorf("resolve path relative to repo root: %w", err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q is outside the repository root", rawPath)
	}
	return rel, nil
}

// CanonicalStoredPath validates and cleans a stored path key.
func CanonicalStoredPath(rawPath string) (string, error) {
	if rawPath == "" {
		return "", fmt.Errorf("store entry has empty path")
	}
	if strings.Contains(rawPath, `\`) {
		return "", fmt.Errorf("store entry %q contains a non-/ path separator", rawPath)
	}
	if pathpkg.IsAbs(rawPath) || filepath.IsAbs(rawPath) {
		return "", fmt.Errorf("store entry %q must be repository-relative", rawPath)
	}
	clean := pathpkg.Clean(rawPath)
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("store entry %q escapes the repository root", rawPath)
	}
	return clean, nil
}

// isDecimalDigits reports whether s is a non-empty string of ASCII decimal digits only.
func isDecimalDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

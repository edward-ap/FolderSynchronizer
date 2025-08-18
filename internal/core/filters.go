// Package core provides file filtering functionality for the FolderSynchronizer application.
// It handles include/exclude pattern matching using file extensions and glob patterns.
package core

import (
	"path/filepath"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
)

// ===== FILE FILTERING FUNCTIONS =====

// MatchesInclude checks if a file should be included based on extension filtering.
// If the include list is empty, all files are included by default.
// Extension matching is case-insensitive for better cross-platform compatibility.
//
// Parameters:
//   - extensions: List of allowed file extensions (e.g., [".jpg", ".png"])
//   - filePath: Full or relative path to the file being checked
//
// Returns:
//   - true if the file should be included, false otherwise
func MatchesInclude(extensions []string, filePath string) bool {
	// Empty include list means include all files
	if len(extensions) == 0 {
		return true
	}

	fileExt := strings.ToLower(filepath.Ext(filePath))
	for _, allowedExt := range extensions {
		if strings.ToLower(allowedExt) == fileExt {
			return true
		}
	}

	return false
}

// MatchesExclude checks if a file should be excluded based on glob pattern matching.
// Uses doublestar library for advanced glob pattern support including ** wildcards.
// Path separators are normalized to forward slashes for consistent matching.
//
// Parameters:
//   - globs: List of glob patterns to exclude (e.g., ["**/*.tmp", "**/node_modules/**"])
//   - filePath: Full or relative path to the file being checked
//
// Returns:
//   - true if the file should be excluded, false otherwise
func MatchesExclude(globs []string, filePath string) bool {
	if len(globs) == 0 {
		return false
	}

	// Normalize path separators for consistent glob matching
	normalizedPath := filepath.ToSlash(filePath)

	for _, pattern := range globs {
		if matched, _ := doublestar.PathMatch(pattern, normalizedPath); matched {
			return true
		}
	}

	return false
}

// MatchesAnyGlob checks if a file path matches any of the provided glob patterns.
// This is a generic utility function that can be used for both include and exclude scenarios.
// Uses the same path normalization as MatchesExclude for consistency.
//
// Parameters:
//   - globs: List of glob patterns to match against
//   - filePath: Full or relative path to the file being checked
//
// Returns:
//   - true if any pattern matches the file path, false otherwise
func MatchesAnyGlob(globs []string, filePath string) bool {
	if len(globs) == 0 {
		return false
	}

	// Normalize path separators for consistent glob matching
	normalizedPath := filepath.ToSlash(filePath)

	for _, pattern := range globs {
		if matched, _ := doublestar.PathMatch(pattern, normalizedPath); matched {
			return true
		}
	}

	return false
}

// ===== COMPOSITE FILTERING FUNCTIONS =====

// ShouldIncludeFile determines if a file should be included in synchronization
// based on both include extensions and exclude glob patterns.
// This is the main entry point for file filtering decisions.
//
// Parameters:
//   - includeExtensions: List of allowed file extensions (empty means include all)
//   - excludeGlobs: List of glob patterns to exclude
//   - filePath: Full or relative path to the file being checked
//
// Returns:
//   - true if the file should be synchronized, false otherwise
func ShouldIncludeFile(includeExtensions []string, excludeGlobs []string, filePath string) bool {
	// First check if file extension is allowed
	if !MatchesInclude(includeExtensions, filePath) {
		return false
	}

	// Then check if file is excluded by any glob pattern
	if MatchesExclude(excludeGlobs, filePath) {
		return false
	}

	return true
}

// ===== HOOK FILTERING FUNCTIONS =====

// ShouldTriggerHook determines if a hook should be triggered for a given file
// based on the hook's extension and glob pattern filters.
//
// Parameters:
//   - hookExtensions: List of extensions that trigger this hook
//   - hookGlobs: List of glob patterns that trigger this hook
//   - filePath: Full or relative path to the file being checked
//
// Returns:
//   - true if the hook should be triggered, false otherwise
func ShouldTriggerHook(hookExtensions []string, hookGlobs []string, filePath string) bool {
	// If both lists are empty, hook triggers for all files
	if len(hookExtensions) == 0 && len(hookGlobs) == 0 {
		return true
	}

	// Check extension match
	if len(hookExtensions) > 0 && MatchesInclude(hookExtensions, filePath) {
		return true
	}

	// Check glob pattern match
	if len(hookGlobs) > 0 && MatchesAnyGlob(hookGlobs, filePath) {
		return true
	}

	return false
}

// ===== UTILITY FUNCTIONS =====

// NormalizeExtension ensures file extensions are in a consistent format.
// Adds a leading dot if missing and converts to lowercase.
//
// Parameters:
//   - extension: File extension to normalize (e.g., "jpg", ".JPG", "PNG")
//
// Returns:
//   - Normalized extension (e.g., ".jpg", ".jpg", ".png")
func NormalizeExtension(extension string) string {
	if extension == "" {
		return ""
	}

	normalized := strings.ToLower(strings.TrimSpace(extension))
	if !strings.HasPrefix(normalized, ".") {
		normalized = "." + normalized
	}

	return normalized
}

// NormalizeExtensions processes a slice of extensions to ensure consistent formatting.
// Removes empty extensions and duplicates while normalizing each one.
//
// Parameters:
//   - extensions: List of file extensions to normalize
//
// Returns:
//   - Normalized and deduplicated list of extensions
func NormalizeExtensions(extensions []string) []string {
	if len(extensions) == 0 {
		return extensions
	}

	seen := make(map[string]bool)
	result := make([]string, 0, len(extensions))

	for _, ext := range extensions {
		normalized := NormalizeExtension(ext)
		if normalized != "" && !seen[normalized] {
			seen[normalized] = true
			result = append(result, normalized)
		}
	}

	return result
}

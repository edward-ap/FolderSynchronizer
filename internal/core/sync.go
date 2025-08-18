// Package core provides file synchronization functionality for the FolderSynchronizer application.
// It handles comparison, copying, and change detection between source and target directories.
package core

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	cfg "FolderSynchronizer/internal/config"

	"github.com/rs/zerolog/log"
)

// ===== CONSTANTS AND CONFIGURATION =====

const (
	// File operation buffer sizes
	HashBufferSize = 32 * 1024 * 1024 // 32MB buffer for hash calculations
	CopyBufferSize = 8 * 1024 * 1024  // 8MB buffer for file copying

	// Time comparison tolerance for cross-filesystem compatibility
	ModTimeToleranceSeconds = 2

	// Sync strategies
	SyncStrategyMTime = "mtime" // Modification time + size comparison
	SyncStrategyHash  = "hash"  // SHA256 hash comparison
)

// ===== SYNCHRONIZATION STRUCTURES =====

// Copier handles file synchronization operations between source and target directories.
// It supports different comparison strategies and provides comprehensive sync statistics.
type Copier struct {
	pair *cfg.Pair // Current sync pair configuration
}

// SyncResult contains detailed statistics about a synchronization operation.
type SyncResult struct {
	FilesCopied  int           // Number of files successfully copied
	BytesCopied  int64         // Total bytes copied
	FilesDeleted int           // Number of files deleted (mirror mode)
	FilesSkipped int           // Number of files skipped (unchanged)
	Duration     time.Duration // Total sync operation duration
	Errors       []error       // Any non-fatal errors encountered
}

// ===== MAIN SYNCHRONIZATION LOGIC =====

// CompareAndSync performs a comprehensive synchronization from source to target directory.
// It compares files using the specified strategy, copies changed files, and optionally
// mirrors deletions. Returns statistics about the operation.
func (c *Copier) CompareAndSync(ctx context.Context, pair *cfg.Pair) (int, int64, error) {
	startTime := time.Now()
	c.pair = pair

	result, err := c.performSync(ctx, pair)
	if err != nil {
		return result.FilesCopied, result.BytesCopied, err
	}

	log.Info().
		Str("pair", pair.ID).
		Int("files", result.FilesCopied).
		Int64("bytes", result.BytesCopied).
		Dur("duration", time.Since(startTime)).
		Msg("sync completed")

	return result.FilesCopied, result.BytesCopied, nil
}

// performSync executes the main synchronization logic with proper error handling.
func (c *Copier) performSync(ctx context.Context, pair *cfg.Pair) (*SyncResult, error) {
	result := &SyncResult{}

	// Ensure target directory exists
	if err := os.MkdirAll(pair.Target, 0o755); err != nil {
		return result, err
	}

	// Sync files from source to target
	if err := c.syncSourceToTarget(ctx, pair, result); err != nil {
		return result, err
	}

	// Handle mirror deletions if enabled
	if pair.MirrorDeletes {
		if err := c.mirrorDeletions(pair, result); err != nil {
			return result, err
		}
	}

	return result, nil
}

// syncSourceToTarget walks the source directory and synchronizes files to target.
func (c *Copier) syncSourceToTarget(ctx context.Context, pair *cfg.Pair, result *SyncResult) error {
	return filepath.WalkDir(pair.Source, func(path string, dirEntry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// Skip directories
		if dirEntry.IsDir() {
			return nil
		}

		// Get relative path for filtering and target calculation
		relativePath, err := filepath.Rel(pair.Source, path)
		if err != nil {
			return err
		}

		// Apply file filters
		if !c.shouldSyncFile(pair, path, relativePath) {
			result.FilesSkipped++
			return nil
		}

		// Check if file needs to be copied
		if changed, err := c.isFileChanged(path, pair, relativePath); err != nil {
			return err
		} else if !changed {
			result.FilesSkipped++
			return nil
		}

		// Copy the file
		bytesCopied, err := c.copyFile(ctx, path, pair, relativePath)
		if err != nil {
			return err
		}

		result.FilesCopied++
		result.BytesCopied += bytesCopied

		log.Info().
			Str("pair", pair.ID).
			Str("file", relativePath).
			Int64("bytes", bytesCopied).
			Msg("copied")

		// Execute hooks for the synchronized file
		RunHooks(ctx, pair, NormalizePath(relativePath))

		return nil
	})
}

// shouldSyncFile determines if a file should be synchronized based on filters.
func (c *Copier) shouldSyncFile(pair *cfg.Pair, fullPath, relativePath string) bool {
	// Check include extensions filter
	if !MatchesInclude(pair.IncludeExt, fullPath) {
		return false
	}

	// Check exclude globs filter
	if MatchesExclude(pair.ExcludeGlobs, fullPath) {
		return false
	}

	return true
}

// isFileChanged determines if a file has changed and needs to be copied.
func (c *Copier) isFileChanged(sourcePath string, pair *cfg.Pair, relativePath string) (bool, error) {
	sourceInfo, err := os.Stat(sourcePath)
	if err != nil {
		return false, err
	}

	targetPath := filepath.Join(pair.Target, relativePath)
	targetInfo, err := os.Stat(targetPath)
	if err != nil {
		if os.IsNotExist(err) {
			return true, nil // Target doesn't exist, needs copying
		}
		return false, err
	}

	return c.filesAreDifferent(sourcePath, targetPath, sourceInfo, targetInfo, pair.SyncStrategy)
}

// filesAreDifferent compares two files using the specified strategy.
func (c *Copier) filesAreDifferent(sourcePath, targetPath string, sourceInfo, targetInfo os.FileInfo, strategy string) (bool, error) {
	switch strategy {
	case SyncStrategyHash:
		return c.compareByHash(sourcePath, targetPath)
	case SyncStrategyMTime:
		fallthrough
	default:
		return c.compareByModTimeAndSize(sourceInfo, targetInfo), nil
	}
}

// compareByHash compares files using SHA256 hash calculation.
func (c *Copier) compareByHash(sourcePath, targetPath string) (bool, error) {
	sourceHash, err := calculateFileHash(sourcePath)
	if err != nil {
		return false, err
	}

	targetHash, err := calculateFileHash(targetPath)
	if err != nil {
		return false, err
	}

	return sourceHash != targetHash, nil
}

// compareByModTimeAndSize compares files using modification time and size.
func (c *Copier) compareByModTimeAndSize(sourceInfo, targetInfo os.FileInfo) bool {
	// Different sizes means different files
	if sourceInfo.Size() != targetInfo.Size() {
		return true
	}

	// Compare modification times with tolerance for filesystem differences
	timeDiff := sourceInfo.ModTime().Sub(targetInfo.ModTime())
	if timeDiff < 0 {
		timeDiff = -timeDiff
	}

	return timeDiff > ModTimeToleranceSeconds*time.Second
}

// copyFile copies a single file from source to target with atomic operations.
func (c *Copier) copyFile(ctx context.Context, sourcePath string, pair *cfg.Pair, relativePath string) (int64, error) {
	targetPath := filepath.Join(pair.Target, relativePath)

	// Ensure target directory exists
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		return 0, err
	}

	// Copy file atomically
	return copyAtomic(sourcePath, targetPath)
}

// mirrorDeletions removes files from target that no longer exist in source.
func (c *Copier) mirrorDeletions(pair *cfg.Pair, result *SyncResult) error {
	return filepath.WalkDir(pair.Target, func(path string, dirEntry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// Skip directories
		if dirEntry.IsDir() {
			return nil
		}

		// Get relative path
		relativePath, err := filepath.Rel(pair.Target, path)
		if err != nil {
			return err
		}

		// Check if corresponding source file exists
		sourcePath := filepath.Join(pair.Source, relativePath)
		if _, err := os.Stat(sourcePath); os.IsNotExist(err) {
			// Source file doesn't exist, remove target file
			if err := os.Remove(path); err != nil {
				log.Error().
					Str("pair", pair.ID).
					Str("file", relativePath).
					Err(err).
					Msg("failed to delete target file")
				return err
			}

			result.FilesDeleted++
			log.Info().
				Str("pair", pair.ID).
				Str("file", relativePath).
				Msg("deleted (mirror)")
		}

		return nil
	})
}

// ===== FILE OPERATIONS =====

// calculateFileHash computes SHA256 hash of a file using optimized buffering.
func calculateFileHash(filePath string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	hasher := sha256.New()
	buffer := make([]byte, HashBufferSize)

	for {
		bytesRead, err := file.Read(buffer)
		if bytesRead > 0 {
			hasher.Write(buffer[:bytesRead])
		}

		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
	}

	return hex.EncodeToString(hasher.Sum(nil)), nil
}

// copyAtomic performs atomic file copying using temporary file and rename.
// This ensures that the target file is never in a partially written state.
func copyAtomic(sourcePath, targetPath string) (int64, error) {
	tempPath := targetPath + ".tmp"

	// Open source file
	sourceFile, err := os.Open(sourcePath)
	if err != nil {
		return 0, err
	}
	defer sourceFile.Close()

	// Create temporary target file
	tempFile, err := os.Create(tempPath)
	if err != nil {
		return 0, err
	}

	// Copy data with optimized buffer
	buffer := make([]byte, CopyBufferSize)
	bytesCopied, copyErr := io.CopyBuffer(tempFile, sourceFile, buffer)

	// Close temp file and handle any close errors
	if closeErr := tempFile.Close(); copyErr == nil {
		copyErr = closeErr
	}

	if copyErr != nil {
		os.Remove(tempPath) // Clean up on failure
		return bytesCopied, copyErr
	}

	// Preserve file modification time as best effort
	if sourceInfo, err := os.Stat(sourcePath); err == nil {
		os.Chtimes(tempPath, time.Now(), sourceInfo.ModTime())
	}

	// Atomic rename to final destination
	if err := os.Rename(tempPath, targetPath); err != nil {
		os.Remove(tempPath) // Clean up on failure
		return bytesCopied, err
	}

	return bytesCopied, nil
}

// ===== EVENT DEBOUNCING =====

// Debouncer coalesces rapid file system events per key to prevent excessive processing.
// This is particularly useful for file watchers that may receive multiple events for a single file change.
type Debouncer struct {
	mutex  sync.Mutex             // Thread-safe access to timers map
	timers map[string]*time.Timer // Active timers per key
	window time.Duration          // Debounce window duration
}

// NewDebouncer creates a new debouncer with the specified debounce window in milliseconds.
func NewDebouncer(milliseconds int) *Debouncer {
	if milliseconds <= 0 {
		milliseconds = 300 // Default debounce window
	}

	return &Debouncer{
		timers: make(map[string]*time.Timer),
		window: time.Duration(milliseconds) * time.Millisecond,
	}
}

// Trigger schedules a function to be executed after the debounce window.
// If called again with the same key before the window expires, the previous
// execution is cancelled and a new one is scheduled.
func (d *Debouncer) Trigger(key string, fn func()) {
	d.mutex.Lock()
	defer d.mutex.Unlock()

	// Cancel existing timer if present
	if existingTimer, exists := d.timers[key]; exists {
		existingTimer.Stop()
	}

	// Create wrapped function that cleans up after execution
	wrappedFn := func() {
		// Execute the user's function
		fn()

		// Clean up timer entry to prevent memory leaks
		d.mutex.Lock()
		delete(d.timers, key)
		d.mutex.Unlock()
	}

	// Schedule new execution
	timer := time.AfterFunc(d.window, wrappedFn)
	d.timers[key] = timer
}

// Close cancels all pending debounced operations and cleans up resources.
func (d *Debouncer) Close() {
	d.mutex.Lock()
	defer d.mutex.Unlock()

	for key, timer := range d.timers {
		timer.Stop()
		delete(d.timers, key)
	}
}

// ===== UTILITY FUNCTIONS =====

// NormalizePath converts a file path to use forward slashes consistently,
// which is useful for templates and cross-platform compatibility.
func NormalizePath(path string) string {
	return strings.ReplaceAll(path, string(os.PathSeparator), "/")
}

// RelPath returns the relative path from base to target, normalized with forward slashes.
// This is a convenience wrapper around filepath.Rel with path normalization.
func RelPath(base, target string) string {
	relativePath, _ := filepath.Rel(base, target)
	return NormalizePath(relativePath)
}

// IsFileExists checks if a file exists and is not a directory.
func IsFileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// IsDirectoryExists checks if a directory exists.
func IsDirectoryExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// GetFileSize returns the size of a file in bytes.
func GetFileSize(path string) (int64, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	return info.Size(), nil
}

// FormatByteSize formats a byte count as a human-readable string.
func FormatByteSize(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return string(rune(bytes)) + " B"
	}

	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}

	units := []string{"KB", "MB", "GB", "TB", "PB"}
	return strings.Join([]string{
		string(rune(bytes / div)),
		".",
		string(rune((bytes % div) * 10 / div)),
		" ",
		units[exp],
	}, "")
}

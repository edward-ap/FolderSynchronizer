// Package config provides configuration management for the FolderSynchronizer application.
// It handles loading, saving, and managing sync pair configurations with proper path resolution
// for different operating systems.
package config

import (
	"FolderSynchronizer/internal/scheduler"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"

	"github.com/adrg/xdg"
)

// ===== CONSTANTS =====

// AppName is used for directory naming across different platforms
const AppName = "FolderSynchronizer"

// Default configuration values
const (
	DefaultListen      = "127.0.0.1:8080"
	DefaultDebounceMs  = 500
	DefaultCopyWorkers = 4
	DefaultRetries     = 3
)

// ===== CONFIGURATION STRUCTURES =====

// Config represents the root configuration that is persisted to disk and served via API.
// It acts as an in-memory state holder for sync pairs managed by the core.
type Config struct {
	Listen string  `json:"listen"` // HTTP server listen address
	Pairs  []*Pair `json:"pairs"`  // Collection of sync pair configurations
}

// Pair represents a single source->target sync configuration with all its settings.
// Runtime fields (like last activity) are maintained elsewhere and not persisted
// to keep the configuration file small and focused on core settings.
type Pair struct {
	// Core identification and state
	ID      string `json:"id"`      // Unique identifier for the sync pair
	Enabled bool   `json:"enabled"` // Whether this pair is active

	// Path configuration
	Source string `json:"source"` // Source directory path
	Target string `json:"target"` // Target directory path

	// File filtering
	IncludeExt   []string `json:"includeExtensions"` // File extensions to include (e.g., [".jar", ".war"])
	ExcludeGlobs []string `json:"excludeGlobs"`      // Glob patterns to exclude (e.g., ["**/*.bak"])

	// Synchronization behavior
	SyncStrategy  string `json:"syncStrategy"`  // "mtime" or "hash" comparison strategy
	DebounceMs    int    `json:"debounceMs"`    // Milliseconds to wait before processing file changes
	MirrorDeletes bool   `json:"mirrorDeletes"` // Whether to delete files in target that don't exist in source

	// Performance tuning
	CopyWorkers    int `json:"copyWorkers,omitempty"`    // Number of concurrent copy operations
	HookMaxRetries int `json:"hookMaxRetries,omitempty"` // Maximum retry attempts for failed hooks

	// Automation and notifications
	Hooks []Hook `json:"hooks"` // Post-sync notification/action hooks

	// Scheduling configuration
	Schedule scheduler.Schedule `json:"schedule"` // When and how often to sync

	// User interface
	Description string `json:"description,omitempty"` // Human-readable description for UI display

	// Extensibility
	Extra map[string]string `json:"extra,omitempty"` // Additional custom fields for future use
}

// Hook represents a post-sync action that can be triggered when files are synchronized.
// Hooks can be either HTTP requests or command executions, with optional file filtering.
type Hook struct {
	MatchExtensions []string     `json:"matchExtensions"`   // File extensions that trigger this hook
	MatchGlobs      []string     `json:"matchGlobs"`        // Glob patterns that trigger this hook
	HTTP            *HTTPHook    `json:"http,omitempty"`    // HTTP request configuration
	Command         *CommandHook `json:"command,omitempty"` // Command execution configuration
}

// HTTPHook configures an HTTP request to be made after successful file synchronization.
// Supports templating in the body to include information about synchronized files.
type HTTPHook struct {
	Method       string            `json:"method"`       // HTTP method (GET, POST, PUT, etc.)
	URL          string            `json:"url"`          // Target URL for the request
	Headers      map[string]string `json:"headers"`      // HTTP headers to include
	BodyTemplate string            `json:"bodyTemplate"` // Request body template with variable substitution
}

// CommandHook configures a command to be executed after successful file synchronization.
// Supports environment variable injection and working directory specification.
type CommandHook struct {
	Executable string            `json:"executable"`        // Command or executable to run
	Args       []string          `json:"args"`              // Command line arguments
	WorkDir    string            `json:"workDir,omitempty"` // Working directory for command execution
	EnvVars    map[string]string `json:"envVars,omitempty"` // Environment variables to set
}

// ===== PATH MANAGEMENT =====

// Paths holds resolved directory paths used by the application for configuration,
// logs, and other persistent data storage.
type Paths struct {
	ConfigDir  string // Directory containing configuration files
	ConfigFile string // Full path to the main configuration file
	LogsDir    string // Directory for log file storage
}

// ResolvePaths determines appropriate configuration directories based on the operating system
// and follows platform conventions (XDG on Linux, AppData on Windows).
// If configOverride is provided, it uses that path's directory instead.
func ResolvePaths(configOverride string) (Paths, error) {
	var dir string

	if configOverride != "" {
		// Custom configuration file path provided
		abs, err := filepath.Abs(configOverride)
		if err != nil {
			return Paths{}, fmt.Errorf("failed to resolve config override path: %w", err)
		}
		return Paths{
			ConfigDir:  filepath.Dir(abs),
			ConfigFile: abs,
			LogsDir:    filepath.Join(filepath.Dir(abs), "logs"),
		}, nil
	}

	// Используем папку рядом с исполняемым файлом
	execPath, err := os.Executable()
	if err == nil {
		dir = filepath.Dir(execPath)
	} else {
		// Fallback к стандартным путям
		if runtime.GOOS == "windows" {
			base := os.Getenv("AppData")
			if base == "" {
				base = xdg.ConfigHome
			}
			dir = filepath.Join(base, AppName)
		} else {
			dir = filepath.Join(xdg.ConfigHome, "gofoldersync")
		}
	}

	return Paths{
		ConfigDir:  dir,
		ConfigFile: filepath.Join(dir, "config.json"),
		LogsDir:    filepath.Join(dir, "logs"),
	}, nil
}

// EnsureDirs creates the necessary directories for configuration and logs if they don't exist.
// Uses appropriate permissions (755) for directory creation.
func EnsureDirs(paths Paths) error {
	if err := os.MkdirAll(paths.ConfigDir, 0o755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}
	if err := os.MkdirAll(paths.LogsDir, 0o755); err != nil {
		return fmt.Errorf("failed to create logs directory: %w", err)
	}
	return nil
}

// ===== CONFIGURATION PERSISTENCE =====

// Load reads the configuration file from the specified path.
// If the file doesn't exist, returns a default configuration.
// Applies default values for missing fields and validates the configuration.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			// Return default configuration if file doesn't exist
			return createDefaultConfig(), nil
		}
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var config Config
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("invalid config JSON: %w", err)
	}

	// Apply default values and validate
	applyDefaults(&config)
	if err := validateConfig(&config); err != nil {
		return nil, fmt.Errorf("config validation failed: %w", err)
	}

	return &config, nil
}

// Save persists the configuration to disk atomically using a temporary file
// to ensure data integrity even if the operation is interrupted.
func Save(path string, config *Config) error {
	tempPath := path + ".tmp"

	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := os.WriteFile(tempPath, data, 0o644); err != nil {
		return fmt.Errorf("failed to write temp config file: %w", err)
	}

	if err := os.Rename(tempPath, path); err != nil {
		// Clean up temp file on failure
		_ = os.Remove(tempPath)
		return fmt.Errorf("failed to rename config file: %w", err)
	}

	return nil
}

// ===== CONFIGURATION UTILITIES =====

// createDefaultConfig returns a new configuration with sensible defaults
func createDefaultConfig() *Config {
	return &Config{
		Listen: DefaultListen,
		Pairs:  []*Pair{},
	}
}

// applyDefaults ensures all configuration fields have appropriate default values
func applyDefaults(config *Config) {
	// Set server defaults
	if config.Listen == "" {
		config.Listen = DefaultListen
	}

	// Set pair defaults
	for _, pair := range config.Pairs {
		applyPairDefaults(pair)
	}
}

// applyPairDefaults sets default values for a sync pair configuration
func applyPairDefaults(pair *Pair) {
	// Set default schedule if not specified
	if pair.Schedule.Type == "" {
		pair.Schedule = scheduler.NewWatcherSchedule()
	}

	// Set performance defaults
	if pair.DebounceMs == 0 {
		pair.DebounceMs = DefaultDebounceMs
	}
	if pair.CopyWorkers == 0 {
		pair.CopyWorkers = DefaultCopyWorkers
	}
	if pair.HookMaxRetries == 0 {
		pair.HookMaxRetries = DefaultRetries
	}

	// Set sync strategy default
	if pair.SyncStrategy == "" {
		pair.SyncStrategy = "mtime"
	}

	// Initialize empty slices to prevent nil issues
	if pair.IncludeExt == nil {
		pair.IncludeExt = []string{}
	}
	if pair.ExcludeGlobs == nil {
		pair.ExcludeGlobs = []string{}
	}
	if pair.Hooks == nil {
		pair.Hooks = []Hook{}
	}
}

// validateConfig performs basic validation on the configuration
func validateConfig(config *Config) error {
	if config.Listen == "" {
		return errors.New("listen address cannot be empty")
	}

	// Validate each pair
	for i, pair := range config.Pairs {
		if err := validatePair(pair); err != nil {
			return fmt.Errorf("pair %d (%s): %w", i, pair.ID, err)
		}
	}

	return nil
}

// validatePair performs validation on a single sync pair configuration
func validatePair(pair *Pair) error {
	if pair.ID == "" {
		return errors.New("pair ID cannot be empty")
	}
	if pair.Source == "" {
		return errors.New("source path cannot be empty")
	}
	if pair.Target == "" {
		return errors.New("target path cannot be empty")
	}
	if pair.Source == pair.Target {
		return errors.New("source and target paths cannot be the same")
	}

	// Validate sync strategy
	switch pair.SyncStrategy {
	case "mtime", "hash", "":
		// Valid strategies (empty will be defaulted)
	default:
		return fmt.Errorf("invalid sync strategy: %s (must be 'mtime' or 'hash')", pair.SyncStrategy)
	}

	// Validate performance settings
	if pair.DebounceMs < 0 {
		return errors.New("debounce milliseconds cannot be negative")
	}
	if pair.CopyWorkers < 0 {
		return errors.New("copy workers cannot be negative")
	}
	if pair.HookMaxRetries < 0 {
		return errors.New("hook max retries cannot be negative")
	}

	// Validate hooks
	for j, hook := range pair.Hooks {
		if err := validateHook(&hook); err != nil {
			return fmt.Errorf("hook %d: %w", j, err)
		}
	}

	return nil
}

// validateHook performs validation on a hook configuration
func validateHook(hook *Hook) error {
	// Must have either HTTP or Command configuration, but not both
	if hook.HTTP == nil && hook.Command == nil {
		return errors.New("hook must have either HTTP or Command configuration")
	}
	if hook.HTTP != nil && hook.Command != nil {
		return errors.New("hook cannot have both HTTP and Command configurations")
	}

	// Validate HTTP hook
	if hook.HTTP != nil {
		if hook.HTTP.URL == "" {
			return errors.New("HTTP hook URL cannot be empty")
		}
		if hook.HTTP.Method == "" {
			hook.HTTP.Method = "POST" // Default method
		}
	}

	// Validate Command hook
	if hook.Command != nil {
		if hook.Command.Executable == "" {
			return errors.New("Command hook executable cannot be empty")
		}
	}

	return nil
}

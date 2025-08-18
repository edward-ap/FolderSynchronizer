// Package logging provides centralized logging configuration for the FolderSynchronizer application.
// It sets up structured logging with file rotation, console output, and configurable log levels.
package logging

import (
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"gopkg.in/natefinch/lumberjack.v2"
)

// ===== LOGGING CONFIGURATION CONSTANTS =====

const (
	// Default log file configuration
	DefaultLogFileName = "app.log"
	DefaultMaxSizeMB   = 10   // Maximum size per log file in megabytes
	DefaultMaxBackups  = 3    // Number of old log files to retain
	DefaultMaxAgeDays  = 28   // Maximum age of log files in days
	DefaultCompress    = true // Whether to compress rotated log files

	// Directory permissions for log directory creation
	LogDirPermissions = 0o755
)

// ===== LOGGING CONFIGURATION STRUCTURES =====

// Config holds logging configuration options for customizable setup.
type Config struct {
	LogsDir    string        // Directory where log files will be stored
	FileName   string        // Name of the main log file
	MaxSizeMB  int           // Maximum size per log file in megabytes
	MaxBackups int           // Number of old log files to retain
	MaxAgeDays int           // Maximum age of log files in days
	Compress   bool          // Whether to compress rotated log files
	Level      zerolog.Level // Minimum log level to output
	ConsoleOut bool          // Whether to output to console/stdout
	PrettyLog  bool          // Whether to use pretty console formatting
}

// ===== DEFAULT CONFIGURATION =====

// DefaultConfig returns a sensible default logging configuration.
func DefaultConfig(logsDir string) *Config {
	return &Config{
		LogsDir:    logsDir,
		FileName:   DefaultLogFileName,
		MaxSizeMB:  DefaultMaxSizeMB,
		MaxBackups: DefaultMaxBackups,
		MaxAgeDays: DefaultMaxAgeDays,
		Compress:   DefaultCompress,
		Level:      zerolog.InfoLevel,
		ConsoleOut: true,
		PrettyLog:  false, // JSON format by default for better parsing
	}
}

// ===== LOGGING SETUP FUNCTIONS =====

// Setup configures zerolog with the default configuration.
// This is a convenience function for backward compatibility.
func Setup(logsDir string) (zerolog.Logger, error) {
	config := DefaultConfig(logsDir)
	return SetupWithConfig(config)
}

// SetupWithConfig configures zerolog with a custom configuration.
// It sets up file rotation, console output, and log formatting based on the provided config.
func SetupWithConfig(config *Config) (zerolog.Logger, error) {
	// Ensure logs directory exists
	if err := os.MkdirAll(config.LogsDir, LogDirPermissions); err != nil {
		return log.Logger, err
	}

	// Create file writer with rotation
	fileWriter, err := createFileWriter(config)
	if err != nil {
		return log.Logger, err
	}

	// Create console writer if enabled
	var writers []io.Writer
	if config.ConsoleOut {
		consoleWriter := createConsoleWriter(config.PrettyLog)
		writers = append(writers, consoleWriter)
	}
	writers = append(writers, fileWriter)

	// Combine all writers
	multiWriter := io.MultiWriter(writers...)

	// Configure zerolog global settings
	setupZerologGlobals(config)

	// Create and configure logger
	logger := zerolog.New(multiWriter).
		With().
		Timestamp().
		Caller(). // Add caller information for better debugging
		Logger().
		Level(config.Level)

	// Set as global logger
	log.Logger = logger

	// Log the setup completion
	logger.Info().
		Str("logs_dir", config.LogsDir).
		Str("log_file", config.FileName).
		Str("level", config.Level.String()).
		Bool("console_output", config.ConsoleOut).
		Bool("pretty_console", config.PrettyLog).
		Msg("logging configured")

	return logger, nil
}

// ===== HELPER FUNCTIONS =====

// createFileWriter creates a rotating file writer using lumberjack.
func createFileWriter(config *Config) (io.Writer, error) {
	logFilePath := filepath.Join(config.LogsDir, config.FileName)

	fileRotator := &lumberjack.Logger{
		Filename:   logFilePath,
		MaxSize:    config.MaxSizeMB,
		MaxBackups: config.MaxBackups,
		MaxAge:     config.MaxAgeDays,
		Compress:   config.Compress,
	}

	return fileRotator, nil
}

// createConsoleWriter creates a console writer with optional pretty formatting.
func createConsoleWriter(prettyLog bool) io.Writer {
	if prettyLog {
		// Pretty console output for development
		return zerolog.ConsoleWriter{
			Out:        os.Stdout,
			TimeFormat: time.RFC3339,
		}
	}

	// JSON output to stdout for production
	return os.Stdout
}

// setupZerologGlobals configures global zerolog settings.
func setupZerologGlobals(config *Config) {
	// Set global time format
	zerolog.TimeFieldFormat = time.RFC3339

	// Set global log level
	zerolog.SetGlobalLevel(config.Level)

	// Configure time field name (optional customization)
	zerolog.TimestampFieldName = "timestamp"
	zerolog.LevelFieldName = "level"
	zerolog.MessageFieldName = "message"
}

// ===== UTILITY FUNCTIONS =====

// SetLogLevel changes the global log level at runtime.
func SetLogLevel(level zerolog.Level) {
	zerolog.SetGlobalLevel(level)
	log.Info().
		Str("new_level", level.String()).
		Msg("log level changed")
}

// GetLogLevel returns the current global log level.
func GetLogLevel() zerolog.Level {
	return zerolog.GlobalLevel()
}

// ParseLogLevel converts a string to a zerolog.Level.
func ParseLogLevel(levelStr string) (zerolog.Level, error) {
	return zerolog.ParseLevel(levelStr)
}

// ===== DEVELOPMENT HELPERS =====

// SetupDevelopment configures logging optimized for development with pretty console output.
func SetupDevelopment(logsDir string) (zerolog.Logger, error) {
	config := DefaultConfig(logsDir)
	config.Level = zerolog.DebugLevel
	config.PrettyLog = true
	config.ConsoleOut = true

	return SetupWithConfig(config)
}

// SetupProduction configures logging optimized for production with JSON output.
func SetupProduction(logsDir string) (zerolog.Logger, error) {
	config := DefaultConfig(logsDir)
	config.Level = zerolog.InfoLevel
	config.PrettyLog = false
	config.ConsoleOut = true

	return SetupWithConfig(config)
}

// SetupSilent configures logging for testing or headless operation (file only, no console).
func SetupSilent(logsDir string) (zerolog.Logger, error) {
	config := DefaultConfig(logsDir)
	config.Level = zerolog.WarnLevel
	config.ConsoleOut = false
	config.PrettyLog = false

	return SetupWithConfig(config)
}

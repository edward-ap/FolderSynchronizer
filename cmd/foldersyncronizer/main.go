// Package main provides the entry point for the FolderSynchronizer application.
// It handles initialization, configuration, server startup, and system tray integration.
package main

import (
	"context"
	"embed"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"FolderSynchronizer/internal/api"
	cfg "FolderSynchronizer/internal/config"
	"FolderSynchronizer/internal/core"
	"FolderSynchronizer/internal/logging"
	"FolderSynchronizer/internal/scheduler"
	"FolderSynchronizer/internal/tray"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// ===== BUILD INFORMATION =====

// version is set during build time and represents the current application version
var version = "0.2.0"

// ===== EMBEDDED ASSETS =====

//go:embed assets/*
var assetsFS embed.FS

// Global tray icon data loaded from embedded assets
var trayIcon []byte

// ===== CONFIGURATION CONSTANTS =====

const (
	// Default configuration values
	DefaultListenAddress = "127.0.0.1:8080"

	// Asset file paths
	PrimaryIconAsset  = "assets/icon.ico"
	FallbackIconAsset = "assets/icon.Ico"

	// ICO file header validation
	ICOHeaderSize = 4

	// Browser opening delays
	CommandRetryDelay = 50 * time.Millisecond

	// Application metadata
	AppName = "FolderSynchronizer"
)

// ===== COMMAND LINE CONFIGURATION =====

// config holds command line arguments and application configuration
type config struct {
	Listen     string // HTTP server listen address
	ConfigPath string // Path to configuration file
	NoTray     bool   // Whether to disable system tray
}

// ===== MAIN APPLICATION ENTRY POINT =====

func main() {
	defer func() {
		if r := recover(); r != nil {
			// Пытаемся записать панику в файл рядом с exe
			execPath, _ := os.Executable()
			execDir := filepath.Dir(execPath)
			crashFile := filepath.Join(execDir, "crash.log")

			crashMsg := fmt.Sprintf("PANIC at %s: %v\n", time.Now().Format(time.RFC3339), r)
			os.WriteFile(crashFile, []byte(crashMsg), 0644)
		}
	}()

	// Parse command line arguments
	appConfig := parseCommandLineArgs()

	// Initialize application paths and directories
	paths, err := initializePaths(appConfig.ConfigPath)
	if err != nil {
		exitWithError("resolve paths", err)
	}

	// Set up logging system
	logger, err := initializeLogging(paths.LogsDir)
	if err != nil {
		exitWithError("logging setup", err)
	}
	_ = logger // Suppress unused variable warning

	// Load application configuration
	appConf, err := loadConfiguration(paths.ConfigFile, &appConfig.Listen)
	if err != nil {
		exitWithError("load config", err)
	}

	// Log startup diagnostics
	logStartupDiagnostics(appConfig.Listen, paths.ConfigFile, appConf)

	// Initialize and start HTTP server
	server, httpServer, err := initializeServer(paths, appConf, appConfig.Listen)
	if err != nil {
		exitWithError("create server", err)
	}

	// Load tray icon from embedded assets
	loadTrayIcon()

	// Auto-start enabled sync pairs
	autoStartEnabledPairs(server, appConf)

	// Run application in appropriate mode (tray or headless)
	runApplication(appConfig, httpServer, server)
}

// ===== INITIALIZATION FUNCTIONS =====

// parseCommandLineArgs parses and returns command line arguments
func parseCommandLineArgs() config {
	var appConfig config

	flag.StringVar(&appConfig.Listen, "listen", DefaultListenAddress,
		"HTTP listen address, e.g. 127.0.0.1:8080")
	flag.StringVar(&appConfig.ConfigPath, "config", "",
		"Path to config.json (optional)")
	flag.BoolVar(&appConfig.NoTray, "no-tray", false,
		"Disable system tray icon")

	flag.Parse()
	return appConfig
}

// initializePaths resolves and creates necessary application directories
func initializePaths(configPath string) (cfg.Paths, error) {
	paths, err := cfg.ResolvePaths(configPath)
	if err != nil {
		return paths, err
	}

	if err := cfg.EnsureDirs(paths); err != nil {
		return paths, err
	}

	return paths, nil
}

// initializeLogging sets up the logging system
func initializeLogging(logsDir string) (interface{}, error) {
	// Определяем путь к логам относительно исполняемого файла
	execPath, err := os.Executable()
	if err != nil {
		return nil, err
	}

	// Папка логов рядом с исполняемым файлом
	execDir := filepath.Dir(execPath)
	logsDir = filepath.Join(execDir, "logs")

	// Создаем конфигурацию только для файлового логирования
	config := &logging.Config{
		LogsDir:    logsDir,
		FileName:   "syncronizer.log",
		MaxSizeMB:  10,
		MaxBackups: 5,
		MaxAgeDays: 30,
		Compress:   true,
		Level:      zerolog.InfoLevel,
		ConsoleOut: false, // Отключаем вывод в консоль
		PrettyLog:  false,
	}

	logger, err := logging.SetupWithConfig(config)
	return logger, err
}

// loadConfiguration loads application configuration from file
func loadConfiguration(configFile string, listen *string) (*cfg.Config, error) {
	conf, err := cfg.Load(configFile)
	if err != nil {
		return nil, err
	}

	// Override listen address if specified in config
	if conf.Listen != "" {
		*listen = conf.Listen
	}

	return conf, nil
}

// logStartupDiagnostics logs comprehensive startup information
func logStartupDiagnostics(listenAddr, configFile string, conf *cfg.Config) {
	log.Info().
		Str("version", version).
		Str("listen", listenAddr).
		Str("config", configFile).
		Int("pairs", len(conf.Pairs)).
		Str("goos", runtime.GOOS).
		Str("goarch", runtime.GOARCH).
		Bool("tray_windows", tray.WindowsBuild).
		Bool("scheduler_enabled", true).
		Msg("starting " + AppName + " with scheduler")
}

// initializeServer creates and starts the HTTP server
func initializeServer(paths cfg.Paths, conf *cfg.Config, listenAddr string) (*api.Server, *http.Server, error) {
	server, err := api.NewServer(paths, conf)
	if err != nil {
		return nil, nil, err
	}

	httpServer := server.StartHTTP(listenAddr)
	return server, httpServer, nil
}

// ===== TRAY ICON MANAGEMENT =====

// loadTrayIcon loads tray icon from embedded assets with fallback options
func loadTrayIcon() {
	var usedAsset string

	// Try primary icon asset first
	if iconData, err := assetsFS.ReadFile(PrimaryIconAsset); err == nil {
		trayIcon = iconData
		usedAsset = PrimaryIconAsset
	} else if iconData, err := assetsFS.ReadFile(FallbackIconAsset); err == nil {
		// Fallback to secondary asset
		trayIcon = iconData
		usedAsset = FallbackIconAsset
	}

	// Log icon diagnostics
	logIconDiagnostics(usedAsset)
}

// logIconDiagnostics logs information about the loaded tray icon
func logIconDiagnostics(usedAsset string) {
	var headerInfo string
	if len(trayIcon) >= ICOHeaderSize {
		headerInfo = fmt.Sprintf("%02x %02x %02x %02x",
			trayIcon[0], trayIcon[1], trayIcon[2], trayIcon[3])
	}

	log.Info().
		Str("asset", usedAsset).
		Int("bytes", len(trayIcon)).
		Str("header", headerInfo).
		Msg("tray icon diagnostics")

	// Log warnings for problematic icons
	if len(trayIcon) == 0 {
		log.Warn().Msg("tray icon asset missing or empty; tray will show default placeholder")
	} else if !isValidICOIcon(trayIcon) {
		log.Warn().Msg("tray icon asset is not a valid .ico; icon will be skipped")
	}
}

// isValidICOIcon checks if the icon data has a valid ICO header
func isValidICOIcon(iconData []byte) bool {
	if len(iconData) < ICOHeaderSize {
		return false
	}

	return iconData[0] == 0x00 && iconData[1] == 0x00 &&
		iconData[2] == 0x01 && iconData[3] == 0x00
}

// ===== SYNC PAIR MANAGEMENT =====

// autoStartEnabledPairs automatically starts all enabled sync pairs
func autoStartEnabledPairs(server *api.Server, conf *cfg.Config) {
	for _, pair := range conf.Pairs {
		if !pair.Enabled {
			continue
		}

		// Set default schedule for legacy configurations
		if pair.Schedule.Type == "" {
			pair.Schedule = scheduler.NewWatcherSchedule()
		}

		if err := server.PairManager.StartPair(pair); err != nil {
			log.Error().
				Str("pair", pair.ID).
				Err(err).
				Msg("failed to start pair")
		} else {
			log.Info().
				Str("pair", pair.ID).
				Str("schedule", string(pair.Schedule.Type)).
				Msg("auto-started sync pair")
		}
	}
}

// ===== APPLICATION EXECUTION MODES =====

// runApplication runs the application in either tray mode or headless mode
func runApplication(appConfig config, httpServer *http.Server, server *api.Server) {
	if appConfig.NoTray {
		runHeadlessMode(httpServer, server)
	} else {
		runTrayMode(appConfig.Listen, httpServer, server)
	}
}

// runHeadlessMode runs the application without system tray (headless mode)
func runHeadlessMode(httpServer *http.Server, server *api.Server) {
	log.Info().Msg("running in headless mode (no system tray)")

	// Set up signal handling for graceful shutdown
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, os.Interrupt, syscall.SIGTERM)

	// Wait for shutdown signal
	sig := <-signalChan
	log.Info().Str("signal", sig.String()).Msg("shutdown signal received")

	// Perform graceful shutdown
	gracefulShutdown(httpServer, server)
}

// runTrayMode runs the application with system tray integration
func runTrayMode(listenAddr string, httpServer *http.Server, server *api.Server) {
	log.Info().Msg("running with system tray integration")

	// Configure tray callbacks
	callbacks := createTrayCallbacks(listenAddr, httpServer, server)

	// Run tray (blocks until quit)
	tray.Run(trayIcon, callbacks)
}

// createTrayCallbacks creates the callback functions for tray interaction
func createTrayCallbacks(listenAddr string, httpServer *http.Server, server *api.Server) tray.Callbacks {
	return tray.Callbacks{
		OnOpenUI: func() {
			url := buildLocalURL(listenAddr)
			if err := openBrowser(url); err != nil {
				log.Error().
					Err(err).
					Str("url", url).
					Msg("failed to open UI in browser")
			} else {
				log.Info().
					Str("url", url).
					Msg("opened UI in browser")
			}
		},

		OnSyncAll: func() {
			log.Info().Msg("sync all triggered from tray")
			performSyncAll(server)
		},

		OnQuit: func() {
			log.Info().Msg("quit triggered from tray")
			gracefulShutdown(httpServer, server)
			log.Info().Msg("shutdown complete; exiting now")
			os.Exit(0)
		},

		ListPairs: func() []tray.PairSummary {
			return server.ListPairsSummary()
		},

		TogglePair: func(id string, enabled bool) {
			if err := server.SetEnabled(id, enabled); err != nil {
				log.Error().
					Str("pair", id).
					Bool("enabled", enabled).
					Err(err).
					Msg("failed to toggle pair from tray")
			} else {
				log.Info().
					Str("pair", id).
					Bool("enabled", enabled).
					Msg("pair toggled from tray")
			}
		},
	}
}

// ===== SHUTDOWN MANAGEMENT =====

// gracefulShutdown performs a graceful shutdown of the application
func gracefulShutdown(httpServer *http.Server, server *api.Server) {
	log.Info().Msg("initiating graceful shutdown")

	// Shutdown HTTP server
	server.ShutdownHTTP(httpServer)

	// Close PairManager which stops all scheduled tasks
	server.Close()

	log.Info().Msg("graceful shutdown completed")
}

// ===== URL AND BROWSER UTILITIES =====

// buildLocalURL constructs a local URL from the listen address
func buildLocalURL(listenAddr string) string {
	host, port, err := net.SplitHostPort(listenAddr)
	if err != nil {
		// Try lenient parsing for complex addresses like ::1:8080
		if idx := strings.LastIndex(listenAddr, ":"); idx > 0 && idx < len(listenAddr)-1 {
			host = listenAddr[:idx]
			port = listenAddr[idx+1:]
		} else {
			return "http://127.0.0.1/"
		}
	}

	// Normalize host for local access
	localHost := normalizeHostForLocal(host)

	return fmt.Sprintf("http://%s:%s/", localHost, port)
}

// normalizeHostForLocal converts bind addresses to localhost for browser opening
func normalizeHostForLocal(host string) string {
	// Convert bind-all addresses to localhost
	if host == "" || host == "0.0.0.0" || host == "::" || host == "[::]" {
		return "127.0.0.1"
	}

	// Wrap IPv6 addresses in brackets if not already wrapped
	if strings.Contains(host, ":") && !strings.HasPrefix(host, "[") {
		return "[" + host + "]"
	}

	return host
}

// openBrowser attempts to open a URL in the default browser using platform-specific methods
func openBrowser(url string) error {
	switch runtime.GOOS {
	case "windows":
		return openBrowserWindows(url)
	case "darwin":
		return exec.Command("open", url).Start()
	default:
		return exec.Command("xdg-open", url).Start()
	}
}

// openBrowserWindows attempts multiple strategies to open browser on Windows
func openBrowserWindows(url string) error {
	// Strategy 1: rundll32
	if err := exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start(); err == nil {
		return nil
	} else {
		log.Debug().Err(err).Msg("rundll32 browser opening failed")
		time.Sleep(CommandRetryDelay)
	}

	// Strategy 2: cmd start
	if err := exec.Command("cmd", "/c", "start", "", url).Start(); err == nil {
		return nil
	} else {
		log.Debug().Err(err).Msg("cmd start browser opening failed")
		time.Sleep(CommandRetryDelay)
	}

	// Strategy 3: PowerShell
	if err := exec.Command("powershell", "-NoProfile", "Start-Process", url).Start(); err == nil {
		return nil
	} else {
		log.Debug().Err(err).Msg("PowerShell browser opening failed")
	}

	return fmt.Errorf("failed to open browser on Windows using all available methods")
}

// ===== SYNC OPERATIONS =====

// performSyncAll executes synchronization for all enabled pairs
func performSyncAll(server *api.Server) {
	// Get current pairs safely
	server.CfgMu.Lock()
	pairs := make([]*cfg.Pair, len(server.Cfg.Pairs))
	copy(pairs, server.Cfg.Pairs)
	server.CfgMu.Unlock()

	var totalFiles int
	var totalBytes int64
	successCount := 0

	log.Info().Int("total_pairs", len(pairs)).Msg("starting sync all operation")

	for _, pair := range pairs {
		if !pair.Enabled {
			continue
		}

		log.Debug().Str("pair", pair.ID).Msg("syncing pair")

		copier := &core.Copier{}
		files, bytes, err := copier.CompareAndSync(context.Background(), pair)

		if err != nil {
			log.Error().
				Str("pair", pair.ID).
				Err(err).
				Msg("sync failed for pair")
		} else {
			totalFiles += files
			totalBytes += bytes
			successCount++

			log.Info().
				Str("pair", pair.ID).
				Int("files", files).
				Int64("bytes", bytes).
				Msg("pair sync completed")
		}
	}

	log.Info().
		Int("successful_pairs", successCount).
		Int("total_files", totalFiles).
		Int64("total_bytes", totalBytes).
		Msg("sync all operation completed")
}

// ===== ERROR HANDLING =====

// exitWithError logs an error message and exits the application
func exitWithError(operation string, err error) {
	fmt.Printf("%s: %v\n", operation, err)
	os.Exit(1)
}

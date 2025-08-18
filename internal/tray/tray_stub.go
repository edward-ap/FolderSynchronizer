//go:build !windows

// Package tray provides system tray integration for the FolderSynchronizer application.
// This file contains stub implementations for non-Windows platforms where
// system tray functionality is not currently supported.
package tray

import (
	"github.com/rs/zerolog/log"
)

// ===== BUILD CONFIGURATION =====

// WindowsBuild indicates whether the Windows tray implementation is compiled in.
// This is always false for non-Windows platforms using this stub implementation.
var WindowsBuild = false

// ===== CALLBACK FUNCTION TYPES =====

// Callbacks defines the interface between the tray and the main application.
// These callback functions allow the tray to interact with core application functionality.
type Callbacks struct {
	// OnOpenUI is called when the user requests to open the main UI
	OnOpenUI func()

	// OnSyncAll is called when the user requests to sync all pairs
	OnSyncAll func()

	// OnQuit is called when the user requests to quit the application
	OnQuit func()

	// ListPairs returns a summary of all sync pairs for tray display
	ListPairs func() []PairSummary

	// TogglePair enables or disables a specific sync pair
	TogglePair func(id string, enabled bool)
}

// ===== DATA STRUCTURES =====

// PairSummary contains minimal information about a sync pair for tray display.
// This structure provides just enough information for tray menu items.
type PairSummary struct {
	ID      string // Unique identifier for the sync pair
	Enabled bool   // Whether the pair is currently active
}

// ===== PLATFORM-SPECIFIC IMPLEMENTATION =====

// Run initializes and starts the system tray functionality.
// On non-Windows platforms, this is a no-op stub that logs the unavailability.
//
// Parameters:
//   - icon: Icon data (usually PNG or ICO format) for the tray icon
//   - callbacks: Application callback functions for tray interactions
func Run(icon []byte, callbacks Callbacks) {
	log.Info().
		Str("platform", "non-windows").
		Msg("system tray functionality not available on this platform")

	// Log callback availability for debugging
	logCallbackAvailability(callbacks)

	// No-op implementation for non-Windows platforms
	// In a future version, this could be extended to support:
	// - Linux (using libappindicator or systray)
	// - macOS (using NSStatusBar)
	// - Other platforms with appropriate tray libraries
}

// ===== UTILITY FUNCTIONS =====

// logCallbackAvailability logs which callbacks are available for debugging purposes
func logCallbackAvailability(callbacks Callbacks) {
	callbackStatus := map[string]bool{
		"OnOpenUI":   callbacks.OnOpenUI != nil,
		"OnSyncAll":  callbacks.OnSyncAll != nil,
		"OnQuit":     callbacks.OnQuit != nil,
		"ListPairs":  callbacks.ListPairs != nil,
		"TogglePair": callbacks.TogglePair != nil,
	}

	log.Debug().
		Interface("callbacks", callbackStatus).
		Msg("tray callback availability")
}

// IsSupported returns whether system tray functionality is supported on the current platform
func IsSupported() bool {
	return false // Always false for non-Windows stub
}

// GetSupportedPlatforms returns a list of platforms where tray functionality is available
func GetSupportedPlatforms() []string {
	return []string{"windows"} // Currently only Windows is supported
}

// ===== FUTURE PLATFORM SUPPORT =====

// The following functions provide a foundation for future cross-platform support:

// runLinux would implement Linux system tray support using libappindicator or systray
// func runLinux(icon []byte, callbacks Callbacks) {
//     // Implementation for Linux using appropriate libraries
//     // Such as: github.com/getlantern/systray or native GTK bindings
// }

// runMacOS would implement macOS system tray support using NSStatusBar
// func runMacOS(icon []byte, callbacks Callbacks) {
//     // Implementation for macOS using Cocoa/NSStatusBar
//     // Such as: github.com/caseymrm/menuet or native Objective-C bindings
// }

// runGeneric would provide a fallback implementation for unsupported platforms
// func runGeneric(icon []byte, callbacks Callbacks) {
//     // Generic fallback that could show a notification or log message
//     // indicating that tray functionality is not available
// }

//go:build windows

// Package tray provides system tray integration for the FolderSynchronizer application on Windows.
// It creates a system tray icon with context menu for quick access to application functions.
package tray

import (
	"fmt"
	"time"

	"github.com/getlantern/systray"
	"github.com/rs/zerolog/log"
)

// ===== BUILD CONFIGURATION =====

// WindowsBuild indicates whether the Windows tray implementation is compiled in.
// This is always true for Windows builds using this implementation.
var WindowsBuild = true

// ===== CONSTANTS AND CONFIGURATION =====

const (
	// Application branding
	AppTitle   = "FolderSynchronizer"
	AppTooltip = "FolderSynchronizer - File Synchronization Tool"

	// Menu item labels
	MenuOpenUI  = "Open UI"
	MenuSyncAll = "Sync all now"
	MenuPairs   = "Pairs"
	MenuQuit    = "Quit"

	// Menu item descriptions
	DescOpenUI  = "Open the web UI in your browser"
	DescSyncAll = "Run synchronization for all enabled pairs"
	DescPairs   = "Enable/disable sync pairs"
	DescQuit    = "Quit the application"

	// ICO file header signature for icon validation
	ICOHeaderByte0 = 0x00
	ICOHeaderByte1 = 0x00
	ICOHeaderByte2 = 0x01
	ICOHeaderByte3 = 0x00

	// Update intervals
	PairRefreshInterval = 8 * time.Second
)

// ===== TYPE DEFINITIONS =====

// ItemHandle provides a type alias for systray menu items for better API clarity
type ItemHandle = *systray.MenuItem

// Callbacks defines the interface between the tray and the main application.
// These callback functions allow the tray to interact with core application functionality.
type Callbacks struct {
	// OnOpenUI is called when the user clicks "Open UI" in the tray menu
	OnOpenUI func()

	// OnSyncAll is called when the user clicks "Sync all now" in the tray menu
	OnSyncAll func()

	// OnQuit is called when the user clicks "Quit" in the tray menu
	OnQuit func()

	// ListPairs returns current sync pairs for displaying in the tray menu
	ListPairs func() []PairSummary

	// TogglePair enables or disables a specific sync pair
	TogglePair func(id string, enabled bool)
}

// PairSummary contains minimal information about a sync pair for tray display.
type PairSummary struct {
	ID      string // Unique identifier for the sync pair
	Enabled bool   // Whether the pair is currently active
}

// ===== TRAY MANAGEMENT STRUCTURE =====

// trayManager manages the system tray state and menu items
type trayManager struct {
	callbacks Callbacks
	pairItems map[string]*systray.MenuItem

	// Main menu items
	openUIItem  *systray.MenuItem
	syncAllItem *systray.MenuItem
	pairsRoot   *systray.MenuItem
	quitItem    *systray.MenuItem
}

// ===== MAIN ENTRY POINT =====

// Run initializes and starts the Windows system tray functionality.
// This function blocks until the tray is closed.
//
// Parameters:
//   - icon: Icon data in ICO format for the tray icon
//   - callbacks: Application callback functions for tray interactions
func Run(icon []byte, callbacks Callbacks) {
	log.Info().Msg("starting Windows system tray")

	systray.Run(
		func() { onReady(icon, callbacks) },
		func() { onExit() },
	)
}

// ===== SYSTRAY LIFECYCLE HANDLERS =====

// onReady is called when the system tray is ready to be configured
func onReady(icon []byte, callbacks Callbacks) {
	log.Debug().Msg("system tray ready, initializing")

	// Initialize tray manager
	manager := &trayManager{
		callbacks: callbacks,
		pairItems: make(map[string]*systray.MenuItem),
	}

	// Set up basic tray properties
	setupTrayProperties(icon)

	// Create menu structure
	manager.createMainMenu()

	// Start event handlers
	manager.startEventHandlers()

	// Start periodic updates
	manager.startPeriodicUpdates()

	log.Info().Msg("system tray initialized successfully")
}

// onExit is called when the system tray is being shut down
func onExit() {
	log.Info().Msg("system tray shutting down")
}

// ===== TRAY SETUP FUNCTIONS =====

// setupTrayProperties configures the basic tray icon properties
func setupTrayProperties(icon []byte) {
	systray.SetTitle(AppTitle)
	systray.SetTooltip(AppTooltip)

	// Validate and set icon if it's in ICO format
	if isValidICOIcon(icon) {
		systray.SetIcon(icon)
		log.Debug().Msg("tray icon set successfully")
	} else if len(icon) > 0 {
		log.Warn().Msg("provided icon is not in ICO format, using default icon")
	} else {
		log.Debug().Msg("no icon provided, using default system icon")
	}
}

// isValidICOIcon checks if the provided icon data has a valid ICO header
func isValidICOIcon(icon []byte) bool {
	if len(icon) < 4 {
		return false
	}

	return icon[0] == ICOHeaderByte0 &&
		icon[1] == ICOHeaderByte1 &&
		icon[2] == ICOHeaderByte2 &&
		icon[3] == ICOHeaderByte3
}

// ===== MENU CREATION =====

// createMainMenu creates the main tray menu structure
func (tm *trayManager) createMainMenu() {
	tm.openUIItem = systray.AddMenuItem(MenuOpenUI, DescOpenUI)
	tm.syncAllItem = systray.AddMenuItem(MenuSyncAll, DescSyncAll)
	tm.pairsRoot = systray.AddMenuItem(MenuPairs, DescPairs)

	systray.AddSeparator()
	tm.quitItem = systray.AddMenuItem(MenuQuit, DescQuit)

	// Initialize pairs submenu
	tm.rebuildPairsMenu()

	log.Debug().Msg("main menu structure created")
}

// ===== EVENT HANDLING =====

// startEventHandlers starts goroutines to handle menu item clicks
func (tm *trayManager) startEventHandlers() {
	// Handle "Open UI" clicks
	go tm.handleOpenUIClicks()

	// Handle "Sync All" clicks
	go tm.handleSyncAllClicks()

	// Handle "Quit" clicks
	go tm.handleQuitClicks()

	log.Debug().Msg("event handlers started")
}

// handleOpenUIClicks processes "Open UI" menu item clicks
func (tm *trayManager) handleOpenUIClicks() {
	for range tm.openUIItem.ClickedCh {
		log.Info().Msg("tray: Open UI clicked")

		if tm.callbacks.OnOpenUI != nil {
			tm.callbacks.OnOpenUI()
		} else {
			log.Warn().Msg("OnOpenUI callback not provided")
		}
	}
}

// handleSyncAllClicks processes "Sync All" menu item clicks
func (tm *trayManager) handleSyncAllClicks() {
	for range tm.syncAllItem.ClickedCh {
		log.Info().Msg("tray: Sync all clicked")

		if tm.callbacks.OnSyncAll != nil {
			tm.callbacks.OnSyncAll()
		} else {
			log.Warn().Msg("OnSyncAll callback not provided")
		}
	}
}

// handleQuitClicks processes "Quit" menu item clicks
func (tm *trayManager) handleQuitClicks() {
	for range tm.quitItem.ClickedCh {
		log.Info().Msg("tray: Quit clicked")

		if tm.callbacks.OnQuit != nil {
			tm.callbacks.OnQuit()
		}

		systray.Quit()
		return
	}
}

// ===== PAIRS MENU MANAGEMENT =====

// startPeriodicUpdates starts the periodic refresh of the pairs menu
func (tm *trayManager) startPeriodicUpdates() {
	if tm.callbacks.ListPairs == nil {
		log.Debug().Msg("ListPairs callback not provided, skipping periodic updates")
		return
	}

	go func() {
		ticker := time.NewTicker(PairRefreshInterval)
		defer ticker.Stop()

		log.Debug().
			Dur("interval", PairRefreshInterval).
			Msg("starting periodic pairs menu refresh")

		for range ticker.C {
			tm.rebuildPairsMenu()
		}
	}()
}

// rebuildPairsMenu rebuilds the pairs submenu based on current sync pairs
func (tm *trayManager) rebuildPairsMenu() {
	if tm.callbacks.ListPairs == nil {
		return
	}

	currentPairs := tm.callbacks.ListPairs()

	// Remove pairs that no longer exist
	tm.removeObsoletePairs(currentPairs)

	// Add or update existing pairs
	tm.updatePairsMenu(currentPairs)
}

// removeObsoletePairs removes menu items for pairs that no longer exist
func (tm *trayManager) removeObsoletePairs(currentPairs []PairSummary) {
	for pairID, menuItem := range tm.pairItems {
		stillExists := false
		for _, pair := range currentPairs {
			if pair.ID == pairID {
				stillExists = true
				break
			}
		}

		if !stillExists {
			log.Debug().Str("pair_id", pairID).Msg("removing obsolete pair from tray menu")
			menuItem.Hide()
			delete(tm.pairItems, pairID)
		}
	}
}

// updatePairsMenu adds new pairs and updates existing ones in the menu
func (tm *trayManager) updatePairsMenu(currentPairs []PairSummary) {
	for _, pair := range currentPairs {
		menuItem := tm.pairItems[pair.ID]

		if menuItem == nil {
			// Create new menu item for this pair
			tm.createPairMenuItem(pair)
		} else {
			// Update existing menu item state
			tm.updatePairMenuItem(menuItem, pair)
		}
	}
}

// createPairMenuItem creates a new menu item for a sync pair
func (tm *trayManager) createPairMenuItem(pair PairSummary) {
	menuItem := tm.pairsRoot.AddSubMenuItemCheckbox(
		pair.ID,
		fmt.Sprintf("Toggle sync pair: %s", pair.ID),
		pair.Enabled,
	)

	tm.pairItems[pair.ID] = menuItem

	// Start click handler for this pair
	go tm.handlePairToggle(menuItem, pair.ID)

	log.Debug().
		Str("pair_id", pair.ID).
		Bool("enabled", pair.Enabled).
		Msg("created new pair menu item")
}

// updatePairMenuItem updates the state of an existing pair menu item
func (tm *trayManager) updatePairMenuItem(menuItem *systray.MenuItem, pair PairSummary) {
	if pair.Enabled {
		menuItem.Check()
	} else {
		menuItem.Uncheck()
	}
}

// handlePairToggle handles clicks on individual pair menu items
func (tm *trayManager) handlePairToggle(menuItem *systray.MenuItem, pairID string) {
	for range menuItem.ClickedCh {
		newState := !menuItem.Checked()

		log.Info().
			Str("pair_id", pairID).
			Bool("new_state", newState).
			Msg("tray: pair toggle clicked")

		if tm.callbacks.TogglePair != nil {
			tm.callbacks.TogglePair(pairID, newState)
		} else {
			log.Warn().Msg("TogglePair callback not provided")
		}

		// Update menu item state
		if newState {
			menuItem.Check()
		} else {
			menuItem.Uncheck()
		}
	}
}

// ===== UTILITY FUNCTIONS =====

// IsSupported returns whether system tray functionality is supported on Windows
func IsSupported() bool {
	return true // Always supported on Windows
}

// GetSupportedPlatforms returns the list of platforms where tray functionality is available
func GetSupportedPlatforms() []string {
	return []string{"windows"}
}

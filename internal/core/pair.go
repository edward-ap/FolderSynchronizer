// Package core provides sync pair management functionality for the FolderSynchronizer application.
// It handles lifecycle management of sync pairs with scheduling integration and file watching capabilities.
package core

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	cfg "FolderSynchronizer/internal/config"
	"FolderSynchronizer/internal/scheduler"

	"github.com/fsnotify/fsnotify"
	"github.com/rs/zerolog/log"
)

// ===== CONSTANTS AND CONFIGURATION =====

const (
	// Default configuration values
	DefaultSyncStrategy = "mtime"
	DefaultDebounceMs   = 300

	// File watcher retry delays for handling file locks (especially on Windows)
	FirstRetryDelay  = 100 * time.Millisecond
	SecondRetryDelay = 300 * time.Millisecond
	ThirdRetryDelay  = 600 * time.Millisecond

	// Directory permissions for creating target directories
	DefaultDirPerms = 0o755

	// Delay for mirror delete operations to handle race conditions
	MirrorDeleteDelay = 100 * time.Millisecond
)

// ===== PAIR MANAGEMENT STRUCTURES =====

// PairManager manages multiple sync pairs with integrated scheduling and file watching.
// It provides centralized control over pair lifecycle, status tracking, and execution.
type PairManager struct {
	scheduler *scheduler.Scheduler   // Centralized task scheduler
	workers   map[string]*PairWorker // File watchers for watcher-type pairs
	mutex     sync.RWMutex           // Thread-safe access to workers map
	ctx       context.Context        // Manager context for shutdown coordination
	cancel    context.CancelFunc     // Cancel function for graceful shutdown
}

// PairStatus contains comprehensive status information about a sync pair,
// including both scheduler state and file watcher activity.
type PairStatus struct {
	ID            string     `json:"id"`                  // Unique pair identifier
	Name          string     `json:"name"`                // Human-readable name/description
	Enabled       bool       `json:"enabled"`             // Whether the pair is active
	ScheduleType  string     `json:"scheduleType"`        // Type of schedule (watcher, interval, cron, etc.)
	LastRun       *time.Time `json:"lastRun"`             // Last execution timestamp
	NextRun       *time.Time `json:"nextRun"`             // Next scheduled execution
	RunCount      int        `json:"runCount"`            // Total successful executions
	FailCount     int        `json:"failCount"`           // Total failed executions
	LastError     string     `json:"lastError,omitempty"` // Last error message
	WatcherActive bool       `json:"watcherActive"`       // Whether file watcher is running
}

// PairWorker handles file system monitoring for watcher-type sync pairs.
// It remains separate from the scheduler for real-time file change detection.
type PairWorker struct {
	Pair   *cfg.Pair          // Pair configuration
	ctx    context.Context    // Worker context
	cancel context.CancelFunc // Worker cancellation
	wg     sync.WaitGroup     // Wait group for graceful shutdown
}

// ===== PAIR MANAGER LIFECYCLE =====

// NewPairManager creates a new pair manager with integrated scheduler.
// Uses local timezone for scheduling by default.
func NewPairManager() (*PairManager, error) {
	// Initialize scheduler with local timezone
	sched, err := scheduler.NewScheduler("")
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithCancel(context.Background())

	pm := &PairManager{
		scheduler: sched,
		workers:   make(map[string]*PairWorker),
		ctx:       ctx,
		cancel:    cancel,
	}

	sched.Start()
	return pm, nil
}

// Close gracefully shuts down the pair manager, stopping all pairs and the scheduler.
func (pm *PairManager) Close() {
	pm.cancel()
	pm.scheduler.Stop()

	pm.mutex.Lock()
	defer pm.mutex.Unlock()

	// Stop all workers
	for _, worker := range pm.workers {
		worker.Stop()
	}
	pm.workers = make(map[string]*PairWorker)
}

// ===== PAIR LIFECYCLE OPERATIONS =====

// StartPair starts a sync pair with appropriate scheduling based on its configuration.
// For watcher-type pairs, creates both a scheduler task and a file watcher.
func (pm *PairManager) StartPair(pair *cfg.Pair) error {
	pm.mutex.Lock()
	defer pm.mutex.Unlock()

	// Stop existing task and worker if they exist
	if worker, exists := pm.workers[pair.ID]; exists {
		worker.Stop()
		pm.scheduler.RemoveTask(pair.ID)
		delete(pm.workers, pair.ID)
	}

	// Create sync function for the scheduler
	syncFunc := func(ctx context.Context) error {
		copier := &Copier{}
		_, _, err := copier.CompareAndSync(ctx, pair)
		return err
	}

	// Prepare task description
	description := pair.Description
	if description == "" {
		description = "Sync " + pair.ID
	}

	// Add task to scheduler
	err := pm.scheduler.AddTask(
		pair.ID,
		description,
		pair.Schedule,
		syncFunc,
	)
	if err != nil {
		return err
	}

	// For watcher mode, create additional file system watcher
	if pair.Schedule.Type == scheduler.ScheduleTypeWatcher {
		worker := NewPairWorker(pair)
		if err := worker.Start(pm.ctx); err != nil {
			pm.scheduler.RemoveTask(pair.ID)
			return err
		}
		pm.workers[pair.ID] = worker
	}

	log.Info().
		Str("pair", pair.ID).
		Str("schedule", string(pair.Schedule.Type)).
		Msg("pair started")

	return nil
}

// StopPair stops a sync pair, removing it from both scheduler and file watcher.
func (pm *PairManager) StopPair(pairID string) error {
	pm.mutex.Lock()
	defer pm.mutex.Unlock()

	// Stop worker if exists
	if worker, exists := pm.workers[pairID]; exists {
		worker.Stop()
		delete(pm.workers, pairID)
	}

	// Remove from scheduler
	return pm.scheduler.RemoveTask(pairID)
}

// SyncPairNow triggers immediate synchronization for a pair, bypassing the schedule.
func (pm *PairManager) SyncPairNow(pairID string) error {
	return pm.scheduler.RunTaskNow(pairID)
}

// UpdatePair updates a pair's configuration, handling schedule type changes appropriately.
func (pm *PairManager) UpdatePair(pair *cfg.Pair) error {
	pm.mutex.Lock()
	defer pm.mutex.Unlock()

	// Update schedule in scheduler
	err := pm.scheduler.UpdateTask(pair.ID, pair.Schedule)
	if err != nil {
		return err
	}

	// Handle watcher mode transitions
	if pair.Schedule.Type == scheduler.ScheduleTypeWatcher {
		// Need a watcher but don't have one - create it
		if _, exists := pm.workers[pair.ID]; !exists {
			worker := NewPairWorker(pair)
			if err := worker.Start(pm.ctx); err != nil {
				return err
			}
			pm.workers[pair.ID] = worker
		}
	} else {
		// No longer need watcher - remove it
		if worker, exists := pm.workers[pair.ID]; exists {
			worker.Stop()
			delete(pm.workers, pair.ID)
		}
	}

	return nil
}

// ===== STATUS AND MONITORING =====

// GetPairStatus returns comprehensive status information for a sync pair.
func (pm *PairManager) GetPairStatus(pairID string) (*PairStatus, error) {
	task, err := pm.scheduler.GetTask(pairID)
	if err != nil {
		return nil, err
	}

	status := &PairStatus{
		ID:           task.ID,
		Name:         task.Name,
		Enabled:      task.Enabled,
		ScheduleType: string(task.Schedule.Type),
		LastRun:      task.LastRun,
		NextRun:      task.NextRun,
		RunCount:     task.RunCount,
		FailCount:    task.FailCount,
		LastError:    task.LastError,
	}

	// Check watcher status
	pm.mutex.RLock()
	_, hasWorker := pm.workers[pairID]
	pm.mutex.RUnlock()

	status.WatcherActive = hasWorker

	return status, nil
}

// ListPairStatuses returns status information for all managed pairs.
func (pm *PairManager) ListPairStatuses() []*PairStatus {
	tasks := pm.scheduler.ListTasks()
	statuses := make([]*PairStatus, len(tasks))

	for i, task := range tasks {
		statuses[i] = &PairStatus{
			ID:           task.ID,
			Name:         task.Name,
			Enabled:      task.Enabled,
			ScheduleType: string(task.Schedule.Type),
			LastRun:      task.LastRun,
			NextRun:      task.NextRun,
			RunCount:     task.RunCount,
			FailCount:    task.FailCount,
			LastError:    task.LastError,
		}

		// Check watcher status
		pm.mutex.RLock()
		_, hasWorker := pm.workers[task.ID]
		pm.mutex.RUnlock()
		statuses[i].WatcherActive = hasWorker
	}

	return statuses
}

// ===== FILE WATCHER IMPLEMENTATION =====

// NewPairWorker creates a new file watcher worker for a sync pair.
func NewPairWorker(pair *cfg.Pair) *PairWorker {
	return &PairWorker{Pair: pair}
}

// Start begins file system monitoring for the pair.
func (w *PairWorker) Start(parent context.Context) error {
	if w.cancel != nil {
		return nil // Already started
	}

	ctx, cancel := context.WithCancel(parent)
	w.ctx = ctx
	w.cancel = cancel
	w.wg.Add(1)
	go w.run()
	return nil
}

// Stop gracefully stops the file watcher.
func (w *PairWorker) Stop() {
	if w.cancel == nil {
		return // Already stopped
	}

	w.cancel()
	w.wg.Wait()
	w.cancel = nil
}

// run implements the main file watching loop with event processing.
func (w *PairWorker) run() {
	defer w.wg.Done()

	pair := w.Pair
	log.Info().Str("pair", pair.ID).Msg("watcher starting")

	// Perform initial synchronization
	copier := &Copier{}
	if _, _, err := copier.CompareAndSync(w.ctx, pair); err != nil {
		log.Error().Str("pair", pair.ID).Err(err).Msg("initial sync failed")
	}

	// Set up file system watcher
	if err := w.watchFileSystem(); err != nil {
		log.Error().Str("pair", pair.ID).Err(err).Msg("file system watching failed")
	}

	log.Info().Str("pair", pair.ID).Msg("watcher stopping")
}

// watchFileSystem sets up and runs the file system watcher with event processing.
func (w *PairWorker) watchFileSystem() error {
	pair := w.Pair

	// Create debouncer for event throttling
	debouncer := NewDebouncer(pair.DebounceMs)

	// Create file system watcher
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	defer watcher.Close()

	// Add all source directories to watcher
	if err := w.addDirectoriesToWatcher(watcher, pair.Source); err != nil {
		return err
	}

	// Process file system events
	for {
		select {
		case event := <-watcher.Events:
			w.handleFileSystemEvent(event, watcher, debouncer)

		case err := <-watcher.Errors:
			if err != nil {
				log.Error().Err(err).Msg("watcher")
			}

		case <-w.ctx.Done():
			return nil
		}
	}
}

// addDirectoriesToWatcher recursively adds directories to the file system watcher.
func (w *PairWorker) addDirectoriesToWatcher(watcher *fsnotify.Watcher, sourcePath string) error {
	return filepath.WalkDir(sourcePath, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() {
			if err := watcher.Add(path); err != nil {
				log.Error().Err(err).Str("dir", path).Msg("watch add failed")
			}
		}
		return nil
	})
}

// handleFileSystemEvent processes individual file system events with appropriate actions.
func (w *PairWorker) handleFileSystemEvent(event fsnotify.Event, watcher *fsnotify.Watcher, debouncer *Debouncer) {
	pair := w.Pair

	if event.Name == "" {
		return
	}

	// Skip excluded files
	if MatchesExclude(pair.ExcludeGlobs, event.Name) {
		return
	}

	// Handle directory creation
	if event.Op&fsnotify.Create == fsnotify.Create {
		if w.handleDirectoryCreation(event.Name, watcher) {
			return // Directory handled, skip file processing
		}
	}

	relativePath := RelPath(pair.Source, event.Name)

	// Debounce the event processing
	debouncer.Trigger(event.Name, func() {
		w.processFileEvent(event, relativePath)
	})
}

// handleDirectoryCreation adds newly created directories to the watcher.
func (w *PairWorker) handleDirectoryCreation(path string, watcher *fsnotify.Watcher) bool {
	if fileInfo, err := os.Stat(path); err == nil && fileInfo.IsDir() {
		// Add the new directory to watcher
		_ = watcher.Add(path)

		// Add all nested subdirectories
		filepath.WalkDir(path, func(walkPath string, d os.DirEntry, err error) error {
			if err != nil {
				log.Error().Err(err).Str("dir", walkPath).Msg("watch add failed")
				return nil
			}
			if d.IsDir() && walkPath != path {
				_ = watcher.Add(walkPath)
			}
			return nil
		})
		return true
	}
	return false
}

// processFileEvent handles individual file events with retry logic for locked files.
func (w *PairWorker) processFileEvent(event fsnotify.Event, relativePath string) {
	pair := w.Pair

	// Handle file modifications (Create, Write, Rename, Chmod)
	if event.Op&(fsnotify.Create|fsnotify.Write|fsnotify.Rename|fsnotify.Chmod) != 0 {
		w.handleFileModification(event.Name, relativePath)
	} else if pair.MirrorDeletes && event.Op&fsnotify.Remove == fsnotify.Remove {
		// Handle file deletion
		targetPath := filepath.Join(pair.Target, relativePath)
		_ = os.Remove(targetPath)
	}
}

// handleFileModification processes file creation/modification events with retry logic.
func (w *PairWorker) handleFileModification(sourcePath, relativePath string) {
	pair := w.Pair

	// Check if file still exists and is not a directory
	fileInfo, err := os.Stat(sourcePath)
	if err != nil || fileInfo.IsDir() {
		// Handle potential rename/move for mirror deletes
		if pair.MirrorDeletes && (err != nil || os.IsNotExist(err)) {
			time.Sleep(MirrorDeleteDelay)
			if _, checkErr := os.Stat(sourcePath); os.IsNotExist(checkErr) {
				targetPath := filepath.Join(pair.Target, relativePath)
				_ = os.Remove(targetPath)
			}
		}
		return
	}

	// Check file inclusion filters
	if !MatchesInclude(pair.IncludeExt, sourcePath) {
		return
	}

	// Prepare target path
	targetPath := filepath.Join(pair.Target, relativePath)
	if err := os.MkdirAll(filepath.Dir(targetPath), DefaultDirPerms); err != nil {
		return
	}

	// Retry copy operation to handle file locks (common on Windows)
	retryDelays := []time.Duration{FirstRetryDelay, SecondRetryDelay, ThirdRetryDelay}
	var copyErr error

	for i, delay := range retryDelays {
		_, copyErr = copyAtomic(sourcePath, targetPath)
		if copyErr == nil {
			break
		}

		if i < len(retryDelays)-1 {
			time.Sleep(delay)
		}
	}

	if copyErr == nil {
		log.Info().
			Str("pair", pair.ID).
			Str("file", relativePath).
			Msg("copied (event)")

		// Execute hooks for successful copy
		RunHooks(w.ctx, pair, relativePath)
	} else {
		log.Error().
			Str("pair", pair.ID).
			Str("file", relativePath).
			Err(copyErr).
			Msg("copy failed after retries")
	}
}

// ===== PAIR VALIDATION =====

// ValidatePair performs comprehensive validation of sync pair configuration,
// including schedule validation and path normalization.
func ValidatePair(pair *cfg.Pair) error {
	if pair.ID == "" {
		return errors.New("id is required")
	}

	if pair.Source == "" || pair.Target == "" {
		return errors.New("source and target are required")
	}

	// Normalize paths for Windows long path support
	if runtime.GOOS == "windows" {
		pair.Source = normalizeWindowsLongPath(pair.Source)
		pair.Target = normalizeWindowsLongPath(pair.Target)
	}

	// Validate schedule configuration
	if err := validateScheduleConfiguration(&pair.Schedule); err != nil {
		return err
	}

	// Apply default values
	applyPairDefaults(pair)

	// Normalize include extensions
	normalizeIncludeExtensions(pair)

	return nil
}

// applyPairDefaults sets default values for pair configuration fields.
func applyPairDefaults(pair *cfg.Pair) {
	if pair.SyncStrategy == "" {
		pair.SyncStrategy = DefaultSyncStrategy
	}

	if pair.DebounceMs <= 0 {
		pair.DebounceMs = DefaultDebounceMs
	}

	// Set default schedule if not specified
	if pair.Schedule.Type == "" {
		pair.Schedule = scheduler.NewWatcherSchedule()
	}
}

// validateScheduleConfiguration validates the schedule configuration based on its type.
func validateScheduleConfiguration(schedule *scheduler.Schedule) error {
	switch schedule.Type {
	case scheduler.ScheduleTypeDisabled, scheduler.ScheduleTypeWatcher:
		// No additional validation required
		return nil

	case scheduler.ScheduleTypeInterval:
		return validateIntervalSchedule(schedule)

	case scheduler.ScheduleTypeCron:
		return validateCronSchedule(schedule)

	case scheduler.ScheduleTypeCustom:
		return validateCustomSchedule(schedule)

	default:
		return errors.New("unsupported schedule type")
	}
}

// validateIntervalSchedule validates interval-based schedule configuration.
func validateIntervalSchedule(schedule *scheduler.Schedule) error {
	if schedule.Interval == "" {
		return errors.New("interval is required for interval schedule")
	}

	if _, err := time.ParseDuration(schedule.Interval); err != nil {
		return errors.New("invalid interval format")
	}

	return nil
}

// validateCronSchedule validates cron-based schedule configuration.
func validateCronSchedule(schedule *scheduler.Schedule) error {
	if schedule.CronExpr == "" {
		return errors.New("cron expression is required for cron schedule")
	}

	// Additional cron expression validation could be added here
	return nil
}

// validateCustomSchedule validates custom schedule configuration.
func validateCustomSchedule(schedule *scheduler.Schedule) error {
	if schedule.Custom == nil {
		return errors.New("custom configuration is required for custom schedule")
	}

	custom := schedule.Custom

	if custom.StartTime == "" || custom.EndTime == "" {
		return errors.New("start time and end time are required for custom schedule")
	}

	if _, err := time.Parse("15:04", custom.StartTime); err != nil {
		return errors.New("invalid start time format (use HH:MM)")
	}

	if _, err := time.Parse("15:04", custom.EndTime); err != nil {
		return errors.New("invalid end time format (use HH:MM)")
	}

	if custom.Interval == "" {
		return errors.New("interval is required for custom schedule")
	}

	if _, err := time.ParseDuration(custom.Interval); err != nil {
		return errors.New("invalid interval format")
	}

	return nil
}

// ===== UTILITY FUNCTIONS =====

// normalizeWindowsLongPath prefixes absolute Windows paths with \\?\ or \\?\UNC\ for UNC paths.
// This enables support for paths longer than 260 characters on Windows.
func normalizeWindowsLongPath(path string) string {
	if path == "" {
		return path
	}

	// Already in long path format
	if strings.HasPrefix(path, `\\?\`) {
		return path
	}

	// UNC path: \\server\share\...
	if strings.HasPrefix(path, `\\`) {
		return `\\?\UNC` + path[1:] // Replace leading \\ with \\?\UNC\
	}

	// Absolute path with drive letter: C:\...
	if filepath.IsAbs(path) {
		return `\\?\` + path
	}

	return path
}

// normalizeIncludeExtensions normalizes the include extensions list for a pair.
// Rules:
//   - Trim spaces, convert to lowercase, ensure each starts with '.'
//   - If "*" or ".*" is specified or list is empty, treat as match-all (clear list)
//   - Remove duplicates
func normalizeIncludeExtensions(pair *cfg.Pair) {
	if pair == nil || len(pair.IncludeExt) == 0 {
		return
	}

	var normalized []string
	matchAll := false
	seen := make(map[string]bool)

	for _, ext := range pair.IncludeExt {
		cleanExt := strings.TrimSpace(strings.ToLower(ext))
		if cleanExt == "" {
			continue
		}

		// Check for wildcard patterns
		if cleanExt == "*" || cleanExt == ".*" {
			matchAll = true
			continue
		}

		// Ensure extension starts with dot
		if !strings.HasPrefix(cleanExt, ".") {
			cleanExt = "." + cleanExt
		}

		// Add if not already seen
		if !seen[cleanExt] {
			seen[cleanExt] = true
			normalized = append(normalized, cleanExt)
		}
	}

	// If wildcard specified or no valid extensions, clear the list (match all)
	if matchAll || len(normalized) == 0 {
		pair.IncludeExt = nil
	} else {
		pair.IncludeExt = normalized
	}
}

// Package api provides HTTP server functionality for the FolderSynchronizer application.
// It handles REST API endpoints for managing sync pairs, schedules, and hooks.
package api

import (
	"context"
	"embed"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	cfg "FolderSynchronizer/internal/config"
	"FolderSynchronizer/internal/core"
	"FolderSynchronizer/internal/scheduler"
	"FolderSynchronizer/internal/tray"

	"github.com/rs/zerolog/log"
)

//go:embed web/*
var webFS embed.FS

// ===== SERVER TYPES AND STRUCTURES =====

// Server represents the main HTTP server with sync pair management capabilities
type Server struct {
	Cfg         *cfg.Config        // Application configuration
	CfgMu       sync.Mutex         // Mutex for thread-safe config access
	Paths       cfg.Paths          // File system paths configuration
	PairManager *core.PairManager  // Manager for sync pairs instead of individual workers
	ctx         context.Context    // Server context for graceful shutdown
	cancel      context.CancelFunc // Cancel function for server context
}

// PairWithStatus combines a sync pair with its current status information
type PairWithStatus struct {
	cfg.Pair
	Status *core.PairStatus `json:"status,omitempty"`
}

// ScheduleExample represents a predefined schedule configuration for API responses
type ScheduleExample struct {
	Name        string             `json:"name"`        // Human-readable name
	Description string             `json:"description"` // Detailed description
	Schedule    scheduler.Schedule `json:"schedule"`    // Schedule configuration
}

// statusRecorder wraps http.ResponseWriter to capture status codes for logging
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (sr *statusRecorder) WriteHeader(code int) {
	sr.status = code
	sr.ResponseWriter.WriteHeader(code)
}

// ===== SERVER LIFECYCLE MANAGEMENT =====

// NewServer creates a new HTTP server instance with the provided configuration
func NewServer(paths cfg.Paths, conf *cfg.Config) (*Server, error) {
	ctx, cancel := context.WithCancel(context.Background())

	pairManager, err := core.NewPairManager()
	if err != nil {
		cancel()
		return nil, err
	}

	return &Server{
		Cfg:         conf,
		Paths:       paths,
		PairManager: pairManager,
		ctx:         ctx,
		cancel:      cancel,
	}, nil
}

// StartHTTP initializes and starts the HTTP server on the specified address
func (s *Server) StartHTTP(listen string) *http.Server {
	mux := http.NewServeMux()

	// REST API endpoints
	mux.HandleFunc("/api/pairs", s.handlePairs)
	mux.HandleFunc("/api/pairs/", s.handlePairByID)
	mux.HandleFunc("/api/syncAll", s.handleSyncAll)
	mux.HandleFunc("/api/schedules/examples", s.handleScheduleExamples)

	// Health check endpoint
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	// Static UI files
	mux.HandleFunc("/", s.serveIndex)
	mux.Handle("/web/", http.FileServer(http.FS(webFS)))

	hs := &http.Server{
		Addr:    listen,
		Handler: logRequest(mux),
	}

	go func() {
		log.Info().Str("listen", listen).Msg("http server starting")
		if err := hs.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error().Err(err).Msg("http server")
		}
	}()

	return hs
}

// ShutdownHTTP gracefully shuts down the HTTP server
func (s *Server) ShutdownHTTP(hs *http.Server) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = hs.Shutdown(ctx)
}

// Close cancels the server's background context and closes the pair manager
func (s *Server) Close() {
	if s.cancel != nil {
		s.cancel()
	}
	if s.PairManager != nil {
		s.PairManager.Close()
	}
}

// ===== HTTP MIDDLEWARE =====

// logRequest provides HTTP request logging middleware
func logRequest(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sr := &statusRecorder{ResponseWriter: w, status: 200}
		next.ServeHTTP(sr, r)
		log.Info().
			Str("method", r.Method).
			Str("path", r.URL.Path).
			Int("status", sr.status).
			Dur("dur", time.Since(start)).
			Msg("http")
	})
}

// ===== STATIC FILE HANDLERS =====

// serveIndex serves the main index.html file
func (s *Server) serveIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	b, err := webFS.ReadFile("web/index.html")
	if err != nil {
		log.Error().Err(err).Msg("serve index")
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(b)
}

// ===== API HANDLERS =====

// handleSyncAll triggers synchronization for all enabled pairs
func (s *Server) handleSyncAll(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Get all pairs and trigger synchronization through PairManager
	s.CfgMu.Lock()
	pairs := make([]*cfg.Pair, len(s.Cfg.Pairs))
	copy(pairs, s.Cfg.Pairs)
	s.CfgMu.Unlock()

	totalFiles := 0
	var totalBytes int64

	for _, p := range pairs {
		if !p.Enabled {
			continue
		}

		// Use direct synchronization for syncAll operation
		copier := &core.Copier{}
		files, bytes, err := copier.CompareAndSync(s.ctx, p)
		if err != nil {
			log.Error().Str("pair", p.ID).Err(err).Msg("sync all failed for pair")
			continue
		}
		totalFiles += files
		totalBytes += bytes
	}

	writeJSON(w, map[string]any{
		"files": totalFiles,
		"bytes": totalBytes,
	})
}

// handlePairs manages the collection of sync pairs (GET, POST)
func (s *Server) handlePairs(w http.ResponseWriter, r *http.Request) {
	s.CfgMu.Lock()
	defer s.CfgMu.Unlock()

	switch r.Method {
	case http.MethodGet:
		s.handleGetPairs(w)
	case http.MethodPost:
		s.handleCreatePair(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleGetPairs returns all pairs with their current status
func (s *Server) handleGetPairs(w http.ResponseWriter) {
	pairs := make([]*PairWithStatus, len(s.Cfg.Pairs))
	for i, p := range s.Cfg.Pairs {
		status, _ := s.PairManager.GetPairStatus(p.ID)
		pairs[i] = &PairWithStatus{
			Pair:   *p,
			Status: status,
		}
	}
	writeJSON(w, pairs)
}

// handleCreatePair creates a new sync pair
func (s *Server) handleCreatePair(w http.ResponseWriter, r *http.Request) {
	var p cfg.Pair
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := core.ValidatePair(&p); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Check ID uniqueness
	for _, existing := range s.Cfg.Pairs {
		if existing.ID == p.ID {
			w.WriteHeader(http.StatusConflict)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"error": "id already exists",
			})
			return
		}
	}

	s.Cfg.Pairs = append(s.Cfg.Pairs, &p)
	_ = cfg.Save(s.Paths.ConfigFile, s.Cfg)
	log.Info().Str("pair", p.ID).Msg("pair created")

	// Auto-start if enabled
	if p.Enabled {
		if err := s.PairManager.StartPair(&p); err != nil {
			log.Error().Str("pair", p.ID).Err(err).Msg("failed to start pair")
		}
	}

	writeJSON(w, p)
}

// handlePairByID manages individual sync pairs and their actions
func (s *Server) handlePairByID(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/pairs/"), "/")
	id := parts[0]
	if id == "" {
		http.NotFound(w, r)
		return
	}

	if len(parts) == 1 {
		s.handlePairCRUD(w, r, id)
	} else {
		s.handlePairActions(w, r, id, parts[1])
	}
}

// handlePairCRUD handles CRUD operations for individual pairs
func (s *Server) handlePairCRUD(w http.ResponseWriter, r *http.Request, id string) {
	switch r.Method {
	case http.MethodPut:
		s.handleUpdatePair(w, r, id)
	case http.MethodDelete:
		s.handleDeletePair(w, id)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleUpdatePair updates an existing sync pair
func (s *Server) handleUpdatePair(w http.ResponseWriter, r *http.Request, id string) {
	var incoming cfg.Pair
	if err := json.NewDecoder(r.Body).Decode(&incoming); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := core.ValidatePair(&incoming); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Update under config lock
	s.CfgMu.Lock()
	updated := false
	var oldPtr *cfg.Pair
	for i := range s.Cfg.Pairs {
		if s.Cfg.Pairs[i].ID == id {
			oldPtr = s.Cfg.Pairs[i]
			s.Cfg.Pairs[i] = &incoming
			updated = true
			break
		}
	}

	if !updated {
		s.CfgMu.Unlock()
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	_ = cfg.Save(s.Paths.ConfigFile, s.Cfg)
	s.CfgMu.Unlock()

	// Update through PairManager
	if incoming.Enabled {
		if oldPtr == nil || !oldPtr.Enabled {
			// Pair was disabled, now enabling
			s.PairManager.StartPair(&incoming)
		} else {
			// Pair was enabled, updating configuration
			s.PairManager.UpdatePair(&incoming)
		}
	} else {
		// Disabling pair
		s.PairManager.StopPair(id)
	}

	writeJSON(w, incoming)
}

// handleDeletePair removes a sync pair
func (s *Server) handleDeletePair(w http.ResponseWriter, id string) {
	s.CfgMu.Lock()
	for i := range s.Cfg.Pairs {
		if s.Cfg.Pairs[i].ID == id {
			s.CfgMu.Unlock()

			// Stop pair through PairManager
			s.PairManager.StopPair(id)

			s.CfgMu.Lock()
			s.Cfg.Pairs = append(s.Cfg.Pairs[:i], s.Cfg.Pairs[i+1:]...)
			_ = cfg.Save(s.Paths.ConfigFile, s.Cfg)
			s.CfgMu.Unlock()

			writeJSON(w, map[string]string{"status": "deleted"})
			return
		}
	}
	s.CfgMu.Unlock()
	http.Error(w, "not found", http.StatusNotFound)
}

// handlePairActions handles action endpoints for pairs (start, stop, sync, etc.)
func (s *Server) handlePairActions(w http.ResponseWriter, r *http.Request, id, action string) {
	switch r.Method + " " + action {
	case http.MethodPost + " start":
		s.handleStartPair(w, id)
	case http.MethodPost + " stop":
		s.handleStopPair(w, id)
	case http.MethodPost + " sync":
		s.handleSyncPair(w, id)
	case http.MethodGet + " status":
		s.handleGetPairStatus(w, id)
	case http.MethodGet + " hook-status":
		s.handleGetHookStatus(w, id)
	case http.MethodPost + " test-hook":
		s.handleTestHook(w, id)
	default:
		http.Error(w, "not found", http.StatusNotFound)
	}
}

// handleStartPair starts a sync pair
func (s *Server) handleStartPair(w http.ResponseWriter, id string) {
	if err := s.SetEnabled(id, true); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]string{"status": "started"})
}

// handleStopPair stops a sync pair
func (s *Server) handleStopPair(w http.ResponseWriter, id string) {
	if err := s.SetEnabled(id, false); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]string{"status": "stopped"})
}

// handleSyncPair triggers immediate synchronization for a pair
func (s *Server) handleSyncPair(w http.ResponseWriter, id string) {
	if err := s.PairManager.SyncPairNow(id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]string{"status": "sync started"})
}

// handleGetPairStatus returns the current status of a sync pair
func (s *Server) handleGetPairStatus(w http.ResponseWriter, id string) {
	status, err := s.PairManager.GetPairStatus(id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	writeJSON(w, status)
}

// handleGetHookStatus returns the last hook execution status
func (s *Server) handleGetHookStatus(w http.ResponseWriter, id string) {
	if st, ok := core.GetLastHookStatus(id); ok {
		writeJSON(w, st)
	} else {
		http.Error(w, "no status", http.StatusNotFound)
	}
}

// handleTestHook executes hooks for testing purposes
func (s *Server) handleTestHook(w http.ResponseWriter, id string) {
	p := s.findPair(id)
	if p == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	// Run hooks with a test file name
	testFile := "test-file.jar"
	core.RunHooks(s.ctx, p, testFile)

	if st, ok := core.GetLastHookStatus(id); ok {
		writeJSON(w, map[string]any{
			"tested": true,
			"file":   testFile,
			"status": st,
		})
	} else {
		writeJSON(w, map[string]any{
			"tested": true,
			"file":   testFile,
			"status": "no hooks executed",
		})
	}
}

// handleScheduleExamples returns predefined schedule examples for the UI
func (s *Server) handleScheduleExamples(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	examples := []ScheduleExample{
		{
			Name:        "Watcher (File Changes)",
			Description: "Monitor file changes and sync immediately",
			Schedule:    scheduler.NewWatcherSchedule(),
		},
		{
			Name:        "Every 15 minutes",
			Description: "Sync every 15 minutes continuously",
			Schedule:    scheduler.NewIntervalSchedule("15m"),
		},
		{
			Name:        "Every 2 hours",
			Description: "Sync every 2 hours continuously",
			Schedule:    scheduler.NewIntervalSchedule("2h"),
		},
		{
			Name:        "Workdays 8-20 every 1.5h",
			Description: "Monday to Friday, 8 AM to 8 PM, every 1.5 hours",
			Schedule:    scheduler.NewWorkdaysSchedule("08:00", "20:00", "1h30m"),
		},
		{
			Name:        "Daily at 9 AM",
			Description: "Every day at 9:00 AM",
			Schedule:    scheduler.NewCronSchedule("0 0 9 * * *"),
		},
		{
			Name:        "Workdays at 9, 13, 17",
			Description: "Monday to Friday at 9:00, 13:00, and 17:00",
			Schedule:    scheduler.NewCronSchedule("0 0 9,13,17 * * 1-5"),
		},
		{
			Name:        "Every 30 min (8-20, workdays)",
			Description: "Every 30 minutes from 8 AM to 8 PM on workdays",
			Schedule:    scheduler.NewCronSchedule("0 */30 8-20 * * 1-5"),
		},
		{
			Name:        "Weekend maintenance",
			Description: "Saturday and Sunday at 2:00 AM",
			Schedule:    scheduler.NewCronSchedule("0 0 2 * * 6,0"),
		},
	}

	writeJSON(w, examples)
}

// ===== UTILITY FUNCTIONS =====

// findPair locates a sync pair by ID (thread-safe)
func (s *Server) findPair(id string) *cfg.Pair {
	s.CfgMu.Lock()
	defer s.CfgMu.Unlock()
	for _, p := range s.Cfg.Pairs {
		if p.ID == id {
			return p
		}
	}
	return nil
}

// SetEnabled updates a pair's enabled flag, starts/stops worker, and persists config
func (s *Server) SetEnabled(id string, enabled bool) error {
	s.CfgMu.Lock()
	var p *cfg.Pair
	for i := range s.Cfg.Pairs {
		if s.Cfg.Pairs[i].ID == id {
			p = s.Cfg.Pairs[i]
			break
		}
	}
	if p == nil {
		s.CfgMu.Unlock()
		return http.ErrMissingFile
	}

	prev := p.Enabled
	p.Enabled = enabled
	_ = cfg.Save(s.Paths.ConfigFile, s.Cfg)
	s.CfgMu.Unlock()

	if prev == enabled {
		return nil
	}

	if enabled {
		return s.PairManager.StartPair(p)
	} else {
		return s.PairManager.StopPair(id)
	}
}

// ListPairsSummary returns brief information about pairs for tray display
func (s *Server) ListPairsSummary() []tray.PairSummary {
	s.CfgMu.Lock()
	defer s.CfgMu.Unlock()
	res := make([]tray.PairSummary, 0, len(s.Cfg.Pairs))
	for _, p := range s.Cfg.Pairs {
		res = append(res, tray.PairSummary{
			ID:      p.ID,
			Enabled: p.Enabled,
		})
	}
	return res
}

// writeJSON writes a JSON response with proper headers and formatting
func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

// ===== CONFIGURATION COMPARISON UTILITIES =====
// Note: These functions are kept for backward compatibility but may be refactored later

// stringSlicesEqual compares two string slices for equality
func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// hooksEqual compares two hook configurations for equality
func hooksEqual(a, b []cfg.Hook) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !stringSlicesEqual(a[i].MatchExtensions, b[i].MatchExtensions) {
			return false
		}
		if !stringSlicesEqual(a[i].MatchGlobs, b[i].MatchGlobs) {
			return false
		}

		// Compare HTTP hooks
		ah, bh := a[i].HTTP, b[i].HTTP
		if (ah == nil) != (bh == nil) {
			return false
		}
		if ah != nil && bh != nil {
			if ah.Method != bh.Method || ah.URL != bh.URL || ah.BodyTemplate != bh.BodyTemplate {
				return false
			}
			if len(ah.Headers) != len(bh.Headers) {
				return false
			}
			for k, v := range ah.Headers {
				if bh.Headers[k] != v {
					return false
				}
			}
		}

		// Compare Command hooks
		ac, bc := a[i].Command, b[i].Command
		if (ac == nil) != (bc == nil) {
			return false
		}
		if ac != nil && bc != nil {
			if ac.Executable != bc.Executable || ac.WorkDir != bc.WorkDir {
				return false
			}
			if !stringSlicesEqual(ac.Args, bc.Args) {
				return false
			}
			if len(ac.EnvVars) != len(bc.EnvVars) {
				return false
			}
			for k, v := range ac.EnvVars {
				if bc.EnvVars[k] != v {
					return false
				}
			}
		}
	}
	return true
}

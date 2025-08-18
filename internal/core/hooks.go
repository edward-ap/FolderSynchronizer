// Package core provides hook execution functionality for the FolderSynchronizer application.
// It handles HTTP webhooks and command execution after file synchronization events.
package core

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"text/template"
	"time"

	cfg "FolderSynchronizer/internal/config"

	"github.com/cenkalti/backoff/v4"
	"github.com/rs/zerolog/log"
)

// ===== CONSTANTS AND CONFIGURATION =====

const (
	// HTTP client timeout for webhook requests
	HTTPTimeout = 20 * time.Second

	// Maximum size limits for response/output reading
	MaxSuccessResponseSize = 4096 // 4KB for successful HTTP responses
	MaxErrorResponseSize   = 8192 // 8KB for error HTTP responses
	MaxCommandOutputSize   = 4000 // 4KB for command output
	MaxDisplaySnippetSize  = 1000 // 1KB for display snippets
	MaxTruncationSize      = 2000 // 2KB before truncating error bodies

	// Backoff configuration for HTTP retries
	RetryInitialInterval = 300 * time.Millisecond
	RetryMaxElapsedTime  = 3 * time.Second
)

// Security: List of potentially dangerous commands to block
var dangerousCommands = []string{
	"rm", "rmdir", "del", "erase", "format", "mkfs",
	"shutdown", "reboot", "halt", "poweroff",
	"dd", "fdisk", "parted",
}

// Security: List of dangerous command patterns to detect
var dangerousPatterns = []string{
	" rm -rf", " rm -r ", " del /s", " :> ", " >/dev/sd",
	"--force", "--recursive", "/f /s", "sudo rm",
}

// ===== HOOK STATUS TRACKING =====

// HookStatus represents the execution status of a hook for UI display
type HookStatus struct {
	Timestamp time.Time `json:"timestamp"` // When the hook was executed
	File      string    `json:"file"`      // File that triggered the hook
	HookType  string    `json:"hookType"`  // Type of hook ("http" or "command")
	Success   bool      `json:"success"`   // Whether execution was successful
	Info      string    `json:"info"`      // Additional information or error details
}

// String returns a JSON representation of the hook status for debugging
func (s HookStatus) String() string {
	data, _ := json.Marshal(s)
	return string(data)
}

// Global hook status tracking (thread-safe)
var (
	hookStatusMutex sync.Mutex
	lastHookStatus  = make(map[string]HookStatus) // pairID -> latest hook status
)

// SetLastHookStatus records the latest hook execution status for a sync pair
func SetLastHookStatus(pairID string, status HookStatus) {
	hookStatusMutex.Lock()
	defer hookStatusMutex.Unlock()
	lastHookStatus[pairID] = status
}

// GetLastHookStatus retrieves the latest hook execution status for a sync pair
func GetLastHookStatus(pairID string) (HookStatus, bool) {
	hookStatusMutex.Lock()
	defer hookStatusMutex.Unlock()
	status, exists := lastHookStatus[pairID]
	return status, exists
}

// ===== TEMPLATE DATA STRUCTURES =====

// hookTemplateData contains variables available for hook templates
type hookTemplateData struct {
	RelPath    string // Relative path of the synchronized file
	Basename   string // Base filename without directory
	SourcePath string // Full path in source directory
	TargetPath string // Full path in target directory
	Timestamp  string // Current timestamp for the hook execution
}

// ===== SECURITY VALIDATION =====

// isCommandSafe performs security validation on command hooks to prevent
// execution of potentially dangerous operations
func isCommandSafe(executable string, args []string) bool {
	executableBase := strings.ToLower(filepath.Base(executable))

	// Check against known dangerous commands
	for _, dangerousCmd := range dangerousCommands {
		if executableBase == dangerousCmd || strings.HasPrefix(executableBase, dangerousCmd+".") {
			return false
		}
	}

	// Check full command line against dangerous patterns
	fullCommand := strings.ToLower(executable + " " + strings.Join(args, " "))
	for _, pattern := range dangerousPatterns {
		if strings.Contains(fullCommand, pattern) {
			return false
		}
	}

	return true
}

// ===== HTTP ERROR HANDLING =====

// httpStatusError represents an HTTP error with status code and response body
type httpStatusError struct {
	Code int    // HTTP status code
	Body string // Response body content
}

// Error returns a formatted error message for HTTP status errors
func (e *httpStatusError) Error() string {
	body := strings.TrimSpace(e.Body)
	if len(body) > MaxTruncationSize {
		body = body[:MaxTruncationSize] + "…"
	}

	if body != "" {
		return fmt.Sprintf("HTTP %d %s — %s", e.Code, http.StatusText(e.Code), body)
	}
	return fmt.Sprintf("HTTP %d %s", e.Code, http.StatusText(e.Code))
}

// ===== TEMPLATE PROCESSING =====

// executeTemplate processes a Go text template with the provided data
func executeTemplate(templateStr string, data hookTemplateData) (string, error) {
	if templateStr == "" {
		return "", nil
	}

	tmpl, err := template.New("hook").Parse(templateStr)
	if err != nil {
		return "", fmt.Errorf("template parse error: %w", err)
	}

	var buffer bytes.Buffer
	if err := tmpl.Execute(&buffer, data); err != nil {
		return "", fmt.Errorf("template execution error: %w", err)
	}

	return buffer.String(), nil
}

// ===== HOOK TYPE DETECTION =====

// detectHookType determines the type of hook based on its configuration
func detectHookType(hook *cfg.Hook) string {
	if hook == nil {
		return "unknown"
	}

	if hook.HTTP != nil && strings.TrimSpace(hook.HTTP.URL) != "" {
		return "http"
	}

	if hook.Command != nil && strings.TrimSpace(hook.Command.Executable) != "" {
		return "command"
	}

	return "unknown"
}

// ===== MAIN HOOK EXECUTION =====

// RunHooks executes all configured hooks for a sync pair when a file is synchronized.
// It filters hooks based on file extensions and glob patterns, then executes
// appropriate HTTP or command hooks with template substitution.
func RunHooks(ctx context.Context, pair *cfg.Pair, relPath string) {
	if len(pair.Hooks) == 0 {
		return
	}

	// Prepare template data for hook execution
	templateData := hookTemplateData{
		RelPath:    relPath,
		Basename:   filepath.Base(relPath),
		SourcePath: filepath.Join(pair.Source, relPath),
		TargetPath: filepath.Join(pair.Target, relPath),
		Timestamp:  time.Now().Format(time.RFC3339),
	}

	// Execute each configured hook
	for i := range pair.Hooks {
		hook := &pair.Hooks[i]

		// Apply hook filtering based on file extensions and globs
		if !shouldTriggerHook(hook, relPath) {
			continue
		}

		// Execute hook based on its type
		switch detectHookType(hook) {
		case "http":
			executeHTTPHook(ctx, pair.ID, hook, templateData)
		case "command":
			executeCommandHook(ctx, pair.ID, hook, templateData)
		default:
			log.Warn().
				Str("pair", pair.ID).
				Str("file", relPath).
				Msg("unknown hook type")

			SetLastHookStatus(pair.ID, HookStatus{
				Timestamp: time.Now(),
				File:      relPath,
				HookType:  "unknown",
				Success:   false,
				Info:      "unknown hook type",
			})
		}
	}
}

// shouldTriggerHook determines if a hook should be executed for the given file
func shouldTriggerHook(hook *cfg.Hook, filePath string) bool {
	// If no filters are specified, trigger for all files
	if len(hook.MatchExtensions) == 0 && len(hook.MatchGlobs) == 0 {
		return true
	}

	// Check extension match using the corrected function name
	if len(hook.MatchExtensions) > 0 && MatchesInclude(hook.MatchExtensions, filePath) {
		return true
	}

	// Check glob pattern match
	if len(hook.MatchGlobs) > 0 && MatchesAnyGlob(hook.MatchGlobs, filePath) {
		return true
	}

	return false
}

// ===== HTTP HOOK EXECUTION =====

// executeHTTPHook executes an HTTP webhook with retry logic and proper error handling
func executeHTTPHook(ctx context.Context, pairID string, hook *cfg.Hook, data hookTemplateData) {
	startTime := time.Now()

	// Validate and prepare HTTP method
	method := strings.ToUpper(strings.TrimSpace(hook.HTTP.Method))
	if method == "" {
		method = http.MethodPost
	}

	// Validate URL
	url := strings.TrimSpace(hook.HTTP.URL)
	if url == "" {
		setHookFailure(pairID, data, "http", "empty URL")
		return
	}

	// Process body template
	bodyText, err := executeTemplate(hook.HTTP.BodyTemplate, data)
	if err != nil {
		setHookFailure(pairID, data, "http", "template error: "+err.Error())
		return
	}

	// Prepare request body
	var bodyReader io.Reader
	if strings.EqualFold(method, http.MethodGet) || bodyText == "" {
		bodyReader = nil // No body for GET requests or empty body
	} else {
		bodyReader = strings.NewReader(bodyText)
	}

	// Create HTTP request
	request, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		setHookFailure(pairID, data, "http", "request creation error: "+err.Error())
		return
	}

	// Set headers
	setHTTPHeaders(request, hook.HTTP.Headers, bodyReader != nil)

	// Execute with retry logic
	client := &http.Client{Timeout: HTTPTimeout}

	operation := func() error {
		return executeHTTPRequest(client, request, pairID, data, startTime)
	}

	backoffStrategy := createBackoffStrategy(ctx)
	if err := backoff.Retry(operation, backoffStrategy); err != nil {
		setHookFailure(pairID, data, "http", err.Error())
		return
	}

	log.Info().
		Str("pair", pairID).
		Str("file", data.RelPath).
		Dur("duration", time.Since(startTime)).
		Msg("http hook success")
}

// setHTTPHeaders configures HTTP headers for webhook requests
func setHTTPHeaders(request *http.Request, headers map[string]string, hasBody bool) {
	// Set custom headers
	for key, value := range headers {
		if strings.EqualFold(key, "content-type") {
			// Only set Content-Type if there's actually a body
			if hasBody {
				request.Header.Set(key, value)
			}
		} else {
			request.Header.Set(key, value)
		}
	}

	// Set default Content-Type for requests with body
	if hasBody && request.Header.Get("Content-Type") == "" {
		request.Header.Set("Content-Type", "application/json")
	}

	// Set default Accept header
	if request.Header.Get("Accept") == "" {
		request.Header.Set("Accept", "application/json, */*;q=0.1")
	}
}

// executeHTTPRequest performs the actual HTTP request with proper response handling
func executeHTTPRequest(client *http.Client, request *http.Request, pairID string, data hookTemplateData, startTime time.Time) error {
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()

	// Handle successful responses (2xx status codes)
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		// Read limited response body for success info
		bodyBytes, _ := io.ReadAll(io.LimitReader(response.Body, MaxSuccessResponseSize))
		snippet := strings.TrimSpace(string(bodyBytes))
		if len(snippet) > MaxDisplaySnippetSize {
			snippet = snippet[:MaxDisplaySnippetSize] + "…"
		}

		info := fmt.Sprintf("HTTP %d %s in %s",
			response.StatusCode,
			http.StatusText(response.StatusCode),
			time.Since(startTime).Round(time.Millisecond))

		if snippet != "" {
			info += " — " + snippet
		}

		SetLastHookStatus(pairID, HookStatus{
			Timestamp: time.Now(),
			File:      data.RelPath,
			HookType:  "http",
			Success:   true,
			Info:      info,
		})

		return nil
	}

	// Handle error responses
	bodyBytes, _ := io.ReadAll(io.LimitReader(response.Body, MaxErrorResponseSize))
	return &httpStatusError{
		Code: response.StatusCode,
		Body: string(bodyBytes),
	}
}

// createBackoffStrategy creates an exponential backoff strategy for HTTP retries
func createBackoffStrategy(ctx context.Context) backoff.BackOffContext {
	exponentialBackoff := backoff.NewExponentialBackOff()
	exponentialBackoff.InitialInterval = RetryInitialInterval
	exponentialBackoff.MaxElapsedTime = RetryMaxElapsedTime
	return backoff.WithContext(exponentialBackoff, ctx)
}

// ===== COMMAND HOOK EXECUTION =====

// executeCommandHook executes a command hook with security validation and proper error handling
func executeCommandHook(ctx context.Context, pairID string, hook *cfg.Hook, data hookTemplateData) {
	startTime := time.Now()

	// Validate command configuration
	if hook.Command == nil || strings.TrimSpace(hook.Command.Executable) == "" {
		setHookFailure(pairID, data, "command", "empty command")
		return
	}

	// Process argument templates
	args, err := processCommandArguments(hook.Command.Args, data)
	if err != nil {
		setHookFailure(pairID, data, "command", "template error: "+err.Error())
		return
	}

	// Security validation
	if !isCommandSafe(hook.Command.Executable, args) {
		setHookFailure(pairID, data, "command", "command rejected by safety checks")
		return
	}

	// Create and configure command
	cmd := exec.CommandContext(ctx, hook.Command.Executable, args...)
	configureCommand(cmd, hook.Command)

	// Execute command
	output, err := cmd.CombinedOutput()
	outputStr := string(output)
	if len(outputStr) > MaxCommandOutputSize {
		outputStr = outputStr[:MaxCommandOutputSize] + "…"
	}

	if err != nil {
		log.Error().
			Str("pair", pairID).
			Str("file", data.RelPath).
			Str("type", "command").
			Dur("duration", time.Since(startTime)).
			Str("output", outputStr).
			Err(err).
			Msg("command hook failed")

		errorMsg := outputStr
		if errorMsg == "" {
			errorMsg = err.Error()
		}

		setHookFailure(pairID, data, "command", errorMsg)
		return
	}

	// Success
	log.Info().
		Str("pair", pairID).
		Str("file", data.RelPath).
		Str("type", "command").
		Dur("duration", time.Since(startTime)).
		Msg("command hook success")

	SetLastHookStatus(pairID, HookStatus{
		Timestamp: time.Now(),
		File:      data.RelPath,
		HookType:  "command",
		Success:   true,
		Info:      outputStr,
	})
}

// processCommandArguments processes template variables in command arguments
func processCommandArguments(args []string, data hookTemplateData) ([]string, error) {
	processedArgs := make([]string, 0, len(args))

	for _, arg := range args {
		processedArg, err := executeTemplate(arg, data)
		if err != nil {
			return nil, err
		}
		processedArgs = append(processedArgs, processedArg)
	}

	return processedArgs, nil
}

// configureCommand sets up working directory and environment variables for command execution
func configureCommand(cmd *exec.Cmd, config *cfg.CommandHook) {
	// Set working directory if specified
	if workDir := strings.TrimSpace(config.WorkDir); workDir != "" {
		cmd.Dir = workDir
	}

	// Set up environment variables
	cmd.Env = os.Environ()
	for key, value := range config.EnvVars {
		cmd.Env = append(cmd.Env, key+"="+value)
	}
}

// ===== UTILITY FUNCTIONS =====

// setHookFailure is a convenience function for recording hook failures
func setHookFailure(pairID string, data hookTemplateData, hookType, errorMsg string) {
	SetLastHookStatus(pairID, HookStatus{
		Timestamp: time.Now(),
		File:      data.RelPath,
		HookType:  hookType,
		Success:   false,
		Info:      errorMsg,
	})
}

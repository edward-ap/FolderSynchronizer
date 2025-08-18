// Package scheduler provides task scheduling functionality for the FolderSynchronizer application.
// It supports multiple schedule types including intervals, cron expressions, and custom schedules.
package scheduler

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
	"github.com/rs/zerolog/log"
)

// ===== SCHEDULE TYPE DEFINITIONS =====

// ScheduleType defines the type of schedule for task execution
type ScheduleType string

const (
	ScheduleTypeDisabled ScheduleType = "disabled" // Manual execution only
	ScheduleTypeWatcher  ScheduleType = "watcher"  // File change monitoring (current mode)
	ScheduleTypeInterval ScheduleType = "interval" // Fixed interval execution
	ScheduleTypeCron     ScheduleType = "cron"     // Cron expression scheduling
	ScheduleTypeCustom   ScheduleType = "custom"   // Custom schedule configuration
)

// WeekDay represents days of the week for scheduling
type WeekDay int

const (
	Sunday WeekDay = iota
	Monday
	Tuesday
	Wednesday
	Thursday
	Friday
	Saturday
)

// ===== SCHEDULE CONFIGURATION STRUCTURES =====

// Schedule contains complete schedule configuration for a task
type Schedule struct {
	Type ScheduleType `json:"type"` // Type of schedule

	// For interval type scheduling
	Interval string `json:"interval,omitempty"` // "5m", "1h30m", "2h"

	// For cron type scheduling
	CronExpr string `json:"cronExpr,omitempty"` // "0 8-20/90 * * 1-5"

	// For custom type scheduling - detailed configuration
	Custom *CustomSchedule `json:"custom,omitempty"`

	// Common schedule settings
	Timezone  string     `json:"timezone,omitempty"`  // "Europe/Moscow", "UTC"
	StartDate *time.Time `json:"startDate,omitempty"` // Schedule activation date
	EndDate   *time.Time `json:"endDate,omitempty"`   // Schedule expiration date
	MaxRuns   int        `json:"maxRuns,omitempty"`   // Maximum number of executions
}

// CustomSchedule provides detailed schedule configuration with time windows and weekdays
type CustomSchedule struct {
	WeekDays  []WeekDay `json:"weekDays"`  // [Monday, Tuesday, Wednesday, Thursday, Friday]
	StartTime string    `json:"startTime"` // "08:00"
	EndTime   string    `json:"endTime"`   // "20:00"
	Interval  string    `json:"interval"`  // "1h30m"

	// Additional scheduling options
	SkipHolidays bool `json:"skipHolidays,omitempty"` // Skip holidays (future feature)
	OnlyWorkDays bool `json:"onlyWorkDays,omitempty"` // Only work days (future feature)
}

// ===== TASK DEFINITIONS =====

// TaskFunc represents a function that can be executed by the scheduler
type TaskFunc func(ctx context.Context) error

// Task represents a scheduled task with execution statistics and configuration
type Task struct {
	ID       string   `json:"id"`       // Unique task identifier
	Name     string   `json:"name"`     // Human-readable task name
	Schedule Schedule `json:"schedule"` // Schedule configuration
	Enabled  bool     `json:"enabled"`  // Whether task is active

	// Execution statistics
	LastRun   *time.Time `json:"lastRun,omitempty"`   // Last execution timestamp
	NextRun   *time.Time `json:"nextRun,omitempty"`   // Next scheduled execution
	RunCount  int        `json:"runCount"`            // Total successful executions
	FailCount int        `json:"failCount"`           // Total failed executions
	LastError string     `json:"lastError,omitempty"` // Last error message

	// Internal fields (not serialized)
	fn        TaskFunc      // Task execution function
	cronEntry cron.EntryID  // Cron scheduler entry ID
	ticker    *time.Ticker  // Interval ticker
	stopChan  chan struct{} // Stop signal channel
}

// ===== SCHEDULER IMPLEMENTATION =====

// Scheduler manages task execution with multiple scheduling strategies
type Scheduler struct {
	mutex    sync.RWMutex       // Thread-safe access to tasks
	tasks    map[string]*Task   // Active tasks by ID
	cron     *cron.Cron         // Cron scheduler instance
	ctx      context.Context    // Scheduler context for shutdown
	cancel   context.CancelFunc // Cancel function for graceful shutdown
	timezone *time.Location     // Default timezone for scheduling
}

// ===== SCHEDULER LIFECYCLE =====

// NewScheduler creates a new scheduler instance with the specified timezone
func NewScheduler(timezone string) (*Scheduler, error) {
	var location *time.Location
	var err error

	if timezone == "" {
		location = time.Local
	} else {
		location, err = time.LoadLocation(timezone)
		if err != nil {
			return nil, fmt.Errorf("invalid timezone %s: %w", timezone, err)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())

	// Create cron scheduler with second precision and logging
	cronScheduler := cron.New(
		cron.WithLocation(location),
		cron.WithSeconds(),
		cron.WithLogger(cronLogger{}),
	)

	return &Scheduler{
		tasks:    make(map[string]*Task),
		cron:     cronScheduler,
		ctx:      ctx,
		cancel:   cancel,
		timezone: location,
	}, nil
}

// Start begins scheduler operation
func (s *Scheduler) Start() {
	s.cron.Start()
	log.Info().Msg("scheduler started")
}

// Stop gracefully shuts down the scheduler and all running tasks
func (s *Scheduler) Stop() {
	s.cancel()
	s.cron.Stop()

	// Stop all active tickers and close channels
	s.mutex.Lock()
	for _, task := range s.tasks {
		s.cleanupTask(task)
	}
	s.mutex.Unlock()

	log.Info().Msg("scheduler stopped")
}

// cleanupTask stops all task-related goroutines and resources
func (s *Scheduler) cleanupTask(task *Task) {
	if task.ticker != nil {
		task.ticker.Stop()
		task.ticker = nil
	}

	if task.stopChan != nil {
		select {
		case <-task.stopChan:
			// Channel already closed
		default:
			close(task.stopChan)
		}
	}
}

// ===== TASK MANAGEMENT =====

// AddTask adds a new task to the scheduler with the specified configuration
func (s *Scheduler) AddTask(id, name string, schedule Schedule, fn TaskFunc) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	if _, exists := s.tasks[id]; exists {
		return fmt.Errorf("task %s already exists", id)
	}

	task := &Task{
		ID:       id,
		Name:     name,
		Schedule: schedule,
		Enabled:  true,
		fn:       fn,
		stopChan: make(chan struct{}),
	}

	if err := s.scheduleTask(task); err != nil {
		return fmt.Errorf("failed to schedule task %s: %w", id, err)
	}

	s.tasks[id] = task
	log.Info().
		Str("task", id).
		Str("type", string(schedule.Type)).
		Msg("task added")

	return nil
}

// RemoveTask removes a task from the scheduler
func (s *Scheduler) RemoveTask(id string) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	task, exists := s.tasks[id]
	if !exists {
		return fmt.Errorf("task %s not found", id)
	}

	s.unscheduleTask(task)
	delete(s.tasks, id)

	log.Info().Str("task", id).Msg("task removed")
	return nil
}

// UpdateTask updates an existing task's schedule
func (s *Scheduler) UpdateTask(id string, schedule Schedule) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	task, exists := s.tasks[id]
	if !exists {
		return fmt.Errorf("task %s not found", id)
	}

	// Stop current schedule
	s.unscheduleTask(task)

	// Update schedule configuration
	task.Schedule = schedule

	// Start new schedule if task is enabled
	if task.Enabled {
		if err := s.scheduleTask(task); err != nil {
			return fmt.Errorf("failed to reschedule task %s: %w", id, err)
		}
	}

	log.Info().
		Str("task", id).
		Str("type", string(schedule.Type)).
		Msg("task updated")

	return nil
}

// EnableTask activates a task for execution
func (s *Scheduler) EnableTask(id string) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	task, exists := s.tasks[id]
	if !exists {
		return fmt.Errorf("task %s not found", id)
	}

	if task.Enabled {
		return nil // Already enabled
	}

	task.Enabled = true
	return s.scheduleTask(task)
}

// DisableTask deactivates a task from execution
func (s *Scheduler) DisableTask(id string) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	task, exists := s.tasks[id]
	if !exists {
		return fmt.Errorf("task %s not found", id)
	}

	if !task.Enabled {
		return nil // Already disabled
	}

	task.Enabled = false
	s.unscheduleTask(task)
	return nil
}

// RunTaskNow executes a task immediately, bypassing the schedule
func (s *Scheduler) RunTaskNow(id string) error {
	s.mutex.RLock()
	task, exists := s.tasks[id]
	s.mutex.RUnlock()

	if !exists {
		return fmt.Errorf("task %s not found", id)
	}

	go s.executeTask(task)
	return nil
}

// ===== TASK INFORMATION =====

// GetTask returns information about a specific task
func (s *Scheduler) GetTask(id string) (*Task, error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	task, exists := s.tasks[id]
	if !exists {
		return nil, fmt.Errorf("task %s not found", id)
	}

	// Return a copy without internal fields
	return s.copyTaskForAPI(task), nil
}

// ListTasks returns information about all tasks
func (s *Scheduler) ListTasks() []*Task {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	tasks := make([]*Task, 0, len(s.tasks))
	for _, task := range s.tasks {
		tasks = append(tasks, s.copyTaskForAPI(task))
	}

	return tasks
}

// copyTaskForAPI creates a copy of a task safe for API consumption
func (s *Scheduler) copyTaskForAPI(task *Task) *Task {
	taskCopy := *task
	taskCopy.fn = nil
	taskCopy.stopChan = nil
	return &taskCopy
}

// ===== SCHEDULE IMPLEMENTATION =====

// scheduleTask configures task execution based on its schedule type
func (s *Scheduler) scheduleTask(task *Task) error {
	switch task.Schedule.Type {
	case ScheduleTypeDisabled:
		// No scheduling needed for disabled tasks
		return nil

	case ScheduleTypeWatcher:
		// File watcher scheduling is handled externally by fsnotify
		return nil

	case ScheduleTypeInterval:
		return s.scheduleIntervalTask(task)

	case ScheduleTypeCron:
		return s.scheduleCronTask(task)

	case ScheduleTypeCustom:
		return s.scheduleCustomTask(task)

	default:
		return fmt.Errorf("unsupported schedule type: %s", task.Schedule.Type)
	}
}

// scheduleIntervalTask sets up interval-based task execution
func (s *Scheduler) scheduleIntervalTask(task *Task) error {
	interval, err := time.ParseDuration(task.Schedule.Interval)
	if err != nil {
		return fmt.Errorf("invalid interval %s: %w", task.Schedule.Interval, err)
	}

	task.ticker = time.NewTicker(interval)
	task.NextRun = timePtr(time.Now().Add(interval))

	go s.runIntervalTask(task, interval)
	return nil
}

// runIntervalTask handles the interval execution loop
func (s *Scheduler) runIntervalTask(task *Task, interval time.Duration) {
	for {
		select {
		case <-task.ticker.C:
			if s.shouldExecuteTask(task) {
				s.executeTask(task)
				task.NextRun = timePtr(time.Now().Add(interval))
			}
		case <-task.stopChan:
			return
		case <-s.ctx.Done():
			return
		}
	}
}

// scheduleCronTask sets up cron-based task execution
func (s *Scheduler) scheduleCronTask(task *Task) error {
	entryID, err := s.cron.AddFunc(task.Schedule.CronExpr, func() {
		if s.shouldExecuteTask(task) {
			s.executeTask(task)
		}
	})
	if err != nil {
		return fmt.Errorf("invalid cron expression %s: %w", task.Schedule.CronExpr, err)
	}

	task.cronEntry = entryID

	// Calculate next execution time
	if entry := s.cron.Entry(entryID); entry.ID != 0 {
		task.NextRun = &entry.Next
	}

	return nil
}

// scheduleCustomTask sets up custom schedule-based task execution
func (s *Scheduler) scheduleCustomTask(task *Task) error {
	custom := task.Schedule.Custom
	if custom == nil {
		return fmt.Errorf("custom schedule configuration is missing")
	}

	if err := s.validateCustomSchedule(custom); err != nil {
		return err
	}

	interval, _ := time.ParseDuration(custom.Interval)
	startTime, _ := time.Parse("15:04", custom.StartTime)
	endTime, _ := time.Parse("15:04", custom.EndTime)

	// Create ticker with appropriate check interval
	checkInterval := s.calculateCheckInterval(interval)
	task.ticker = time.NewTicker(checkInterval)
	task.NextRun = timePtr(s.calculateNextCustomExecution(task))

	go s.runCustomTask(task, startTime, endTime, interval)
	return nil
}

// validateCustomSchedule validates custom schedule configuration
func (s *Scheduler) validateCustomSchedule(custom *CustomSchedule) error {
	if _, err := time.ParseDuration(custom.Interval); err != nil {
		return fmt.Errorf("invalid custom interval %s: %w", custom.Interval, err)
	}

	if _, err := time.Parse("15:04", custom.StartTime); err != nil {
		return fmt.Errorf("invalid start time %s: %w", custom.StartTime, err)
	}

	if _, err := time.Parse("15:04", custom.EndTime); err != nil {
		return fmt.Errorf("invalid end time %s: %w", custom.EndTime, err)
	}

	return nil
}

// calculateCheckInterval determines appropriate check frequency for custom schedules
func (s *Scheduler) calculateCheckInterval(interval time.Duration) time.Duration {
	checkInterval := time.Minute
	if interval < checkInterval {
		checkInterval = interval
	}
	return checkInterval
}

// runCustomTask handles the custom schedule execution loop
func (s *Scheduler) runCustomTask(task *Task, startTime, endTime time.Time, interval time.Duration) {
	var lastExecution time.Time

	for {
		select {
		case now := <-task.ticker.C:
			if s.shouldExecuteCustomTask(task, now, startTime, endTime, interval, &lastExecution) {
				s.executeTask(task)
				lastExecution = now
				task.NextRun = timePtr(s.calculateNextCustomExecution(task))
			}
		case <-task.stopChan:
			return
		case <-s.ctx.Done():
			return
		}
	}
}

// shouldExecuteCustomTask determines if a custom scheduled task should run
func (s *Scheduler) shouldExecuteCustomTask(task *Task, now time.Time, startTime, endTime time.Time, interval time.Duration, lastExecution *time.Time) bool {
	// Check if task should run based on general conditions
	if !s.shouldExecuteTask(task) {
		return false
	}

	// Check weekday constraints
	if !s.isValidWeekDay(task.Schedule.Custom.WeekDays, now.Weekday()) {
		return false
	}

	// Check time window constraints
	if !s.isWithinTimeWindow(now, startTime, endTime) {
		return false
	}

	// Check interval constraints
	if !lastExecution.IsZero() && now.Sub(*lastExecution) < interval {
		return false
	}

	return true
}

// isValidWeekDay checks if the current weekday is allowed for execution
func (s *Scheduler) isValidWeekDay(allowedDays []WeekDay, currentDay time.Weekday) bool {
	if len(allowedDays) == 0 {
		return true // All days allowed if none specified
	}

	for _, allowedDay := range allowedDays {
		if WeekDay(currentDay) == allowedDay {
			return true
		}
	}
	return false
}

// isWithinTimeWindow checks if current time is within the allowed execution window
func (s *Scheduler) isWithinTimeWindow(now time.Time, startTime, endTime time.Time) bool {
	currentTime := time.Date(0, 1, 1, now.Hour(), now.Minute(), now.Second(), 0, time.UTC)
	start := time.Date(0, 1, 1, startTime.Hour(), startTime.Minute(), 0, 0, time.UTC)
	end := time.Date(0, 1, 1, endTime.Hour(), endTime.Minute(), 0, 0, time.UTC)

	return !currentTime.Before(start) && !currentTime.After(end)
}

// calculateNextCustomExecution calculates the next execution time for custom schedules
func (s *Scheduler) calculateNextCustomExecution(task *Task) time.Time {
	custom := task.Schedule.Custom
	now := time.Now()

	// Simple logic - next interval (could be enhanced with more sophisticated calculation)
	interval, _ := time.ParseDuration(custom.Interval)
	return now.Add(interval)
}

// unscheduleTask removes a task from all scheduling mechanisms
func (s *Scheduler) unscheduleTask(task *Task) {
	// Remove from cron scheduler
	if task.cronEntry != 0 {
		s.cron.Remove(task.cronEntry)
		task.cronEntry = 0
	}

	// Stop interval ticker
	if task.ticker != nil {
		task.ticker.Stop()
		task.ticker = nil
	}

	// Close stop channel and recreate for potential reuse
	if task.stopChan != nil {
		select {
		case <-task.stopChan:
			// Channel already closed
		default:
			close(task.stopChan)
		}
		task.stopChan = make(chan struct{})
	}

	task.NextRun = nil
}

// ===== TASK EXECUTION =====

// shouldExecuteTask checks general conditions for task execution
func (s *Scheduler) shouldExecuteTask(task *Task) bool {
	if !task.Enabled {
		return false
	}

	now := time.Now()

	// Check date range constraints
	if task.Schedule.StartDate != nil && now.Before(*task.Schedule.StartDate) {
		return false
	}

	if task.Schedule.EndDate != nil && now.After(*task.Schedule.EndDate) {
		return false
	}

	// Check maximum execution limit
	if task.Schedule.MaxRuns > 0 && task.RunCount >= task.Schedule.MaxRuns {
		return false
	}

	return true
}

// executeTask runs a task with error handling and statistics tracking
func (s *Scheduler) executeTask(task *Task) {
	defer func() {
		if r := recover(); r != nil {
			log.Error().
				Str("task", task.ID).
				Interface("panic", r).
				Msg("task panicked")

			task.FailCount++
			task.LastError = fmt.Sprintf("panic: %v", r)
		}
	}()

	log.Info().Str("task", task.ID).Msg("executing task")

	startTime := time.Now()
	task.LastRun = &startTime

	if err := task.fn(s.ctx); err != nil {
		log.Error().
			Str("task", task.ID).
			Err(err).
			Msg("task failed")

		task.FailCount++
		task.LastError = err.Error()
	} else {
		log.Info().
			Str("task", task.ID).
			Dur("duration", time.Since(startTime)).
			Msg("task completed")

		task.LastError = ""
	}

	task.RunCount++
}

// ===== UTILITY FUNCTIONS =====

// timePtr returns a pointer to the given time value
func timePtr(t time.Time) *time.Time {
	return &t
}

// ===== CRON LOGGER IMPLEMENTATION =====

// cronLogger implements the cron library's logging interface
type cronLogger struct{}

func (cronLogger) Info(msg string, keysAndValues ...interface{}) {
	log.Info().Interface("data", keysAndValues).Msg(msg)
}

func (cronLogger) Error(err error, msg string, keysAndValues ...interface{}) {
	log.Error().Err(err).Interface("data", keysAndValues).Msg(msg)
}

// ===== SCHEDULE BUILDER FUNCTIONS =====

// NewWatcherSchedule creates a file watcher schedule
func NewWatcherSchedule() Schedule {
	return Schedule{Type: ScheduleTypeWatcher}
}

// NewIntervalSchedule creates an interval-based schedule
func NewIntervalSchedule(interval string) Schedule {
	return Schedule{
		Type:     ScheduleTypeInterval,
		Interval: interval,
	}
}

// NewCronSchedule creates a cron expression-based schedule
func NewCronSchedule(cronExpr string) Schedule {
	return Schedule{
		Type:     ScheduleTypeCron,
		CronExpr: cronExpr,
	}
}

// NewWorkdaysSchedule creates a workday schedule (Monday-Friday)
// Example: Mon-Fri from 8:00 to 20:00 every 1.5 hours
func NewWorkdaysSchedule(startTime, endTime, interval string) Schedule {
	return Schedule{
		Type: ScheduleTypeCustom,
		Custom: &CustomSchedule{
			WeekDays:  []WeekDay{Monday, Tuesday, Wednesday, Thursday, Friday},
			StartTime: startTime,
			EndTime:   endTime,
			Interval:  interval,
		},
	}
}

// NewCustomSchedule creates a custom schedule with specified parameters
func NewCustomSchedule(weekDays []WeekDay, startTime, endTime, interval string) Schedule {
	return Schedule{
		Type: ScheduleTypeCustom,
		Custom: &CustomSchedule{
			WeekDays:  weekDays,
			StartTime: startTime,
			EndTime:   endTime,
			Interval:  interval,
		},
	}
}

// ===== CRON EXPRESSION EXAMPLES =====
//
// Common cron expression patterns:
//
// "0 */15 * * * *"     - Every 15 minutes
// "0 0 8-17 * * 1-5"   - Every hour from 8 AM to 5 PM on weekdays
// "0 0/30 8-20 * * 1-5" - Every 30 minutes from 8 AM to 8 PM on weekdays
// "0 0 9,13,17 * * 1-5" - At 9:00 AM, 1:00 PM, and 5:00 PM on weekdays
// "0 0 2 * * 6,0"      - At 2:00 AM on weekends
// "0 0 0 1 * *"        - At midnight on the first day of every month

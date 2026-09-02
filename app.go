package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
	_ "modernc.org/sqlite"
)

type App struct {
	ctx      context.Context
	db       *sql.DB
	deviceID string
	mu       sync.Mutex
}

func NewApp() *App { return &App{} }

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	if err := a.openDatabase(); err != nil {
		runtime.LogErrorf(ctx, "database startup failed: %v", err)
	}
}

func (a *App) shutdown(_ context.Context) {
	if a.db != nil {
		_ = a.db.Close()
	}
}

func (a *App) openDatabase() error {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return err
	}
	dataDir := filepath.Join(configDir, "Punchcard")
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return err
	}
	return a.openDatabaseAt(filepath.Join(dataDir, "punchcard.db"))
}

func (a *App) openDatabaseAt(path string) error {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return err
	}
	db.SetMaxOpenConns(1)
	if _, err = db.Exec(`
		PRAGMA journal_mode=WAL;
		PRAGMA foreign_keys=ON;
		CREATE TABLE IF NOT EXISTS tasks (
			id TEXT PRIMARY KEY,
			title TEXT NOT NULL,
			notes TEXT NOT NULL DEFAULT '',
			project TEXT NOT NULL DEFAULT '',
			estimate_minutes INTEGER NOT NULL DEFAULT 0,
			completed INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);
		CREATE TABLE IF NOT EXISTS time_entries (
			id TEXT PRIMARY KEY,
			task_id TEXT NOT NULL,
			started_at TEXT NOT NULL,
			ended_at TEXT NOT NULL,
			duration_seconds INTEGER NOT NULL,
			note TEXT NOT NULL DEFAULT '',
			device_id TEXT NOT NULL,
			created_at TEXT NOT NULL,
			FOREIGN KEY(task_id) REFERENCES tasks(id) ON DELETE CASCADE
		);
		CREATE TABLE IF NOT EXISTS active_timer (
			singleton INTEGER PRIMARY KEY CHECK (singleton = 1),
			task_id TEXT NOT NULL,
			started_at TEXT NOT NULL,
			device_id TEXT NOT NULL,
			FOREIGN KEY(task_id) REFERENCES tasks(id) ON DELETE CASCADE
		);
		CREATE TABLE IF NOT EXISTS settings (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL
		);
	`); err != nil {
		_ = db.Close()
		return err
	}
	a.db = db

	if err := db.QueryRow(`SELECT value FROM settings WHERE key = 'device_id'`).Scan(&a.deviceID); errors.Is(err, sql.ErrNoRows) {
		a.deviceID = newID()
		_, err = db.Exec(`INSERT INTO settings(key, value) VALUES('device_id', ?)`, a.deviceID)
	}
	return err
}

func (a *App) ready() error {
	if a.db == nil {
		return errors.New("the local database is unavailable")
	}
	return nil
}

func (a *App) GetState() (AppState, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.getState()
}

func (a *App) getState() (AppState, error) {
	state := AppState{
		Tasks:   []Task{},
		Entries: []TimeEntry{},
	}
	if err := a.ready(); err != nil {
		return state, err
	}

	rows, err := a.db.Query(`SELECT id, title, completed, created_at, updated_at FROM tasks ORDER BY completed, updated_at DESC`)
	if err != nil {
		return state, err
	}
	for rows.Next() {
		var task Task
		if err := rows.Scan(&task.ID, &task.Title, &task.Completed, &task.CreatedAt, &task.UpdatedAt); err != nil {
			_ = rows.Close()
			return state, err
		}
		state.Tasks = append(state.Tasks, task)
	}
	if err := rows.Close(); err != nil {
		return state, err
	}

	entryRows, err := a.db.Query(`
		SELECT e.id, e.task_id, e.started_at, e.ended_at, e.duration_seconds, e.note, e.device_id, e.created_at
		FROM time_entries e
		ORDER BY e.started_at DESC`)
	if err != nil {
		return state, err
	}
	for entryRows.Next() {
		var entry TimeEntry
		if err := entryRows.Scan(&entry.ID, &entry.TaskID, &entry.StartedAt, &entry.EndedAt, &entry.DurationSeconds, &entry.Note, &entry.DeviceID, &entry.CreatedAt); err != nil {
			_ = entryRows.Close()
			return state, err
		}
		state.Entries = append(state.Entries, entry)
	}
	if err := entryRows.Close(); err != nil {
		return state, err
	}

	var timer ActiveTimer
	err = a.db.QueryRow(`SELECT a.task_id, t.title, a.started_at, a.device_id FROM active_timer a JOIN tasks t ON t.id = a.task_id WHERE singleton = 1`).
		Scan(&timer.TaskID, &timer.TaskTitle, &timer.StartedAt, &timer.DeviceID)
	if err == nil {
		state.ActiveTimer = &timer
	} else if !errors.Is(err, sql.ErrNoRows) {
		return state, err
	}

	return state, nil
}

func (a *App) CreateTask(input CreateTaskInput) (Task, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.ready(); err != nil {
		return Task{}, err
	}
	input.Title = strings.TrimSpace(input.Title)
	if input.Title == "" {
		return Task{}, errors.New("task title is required")
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	task := Task{
		ID: newID(), Title: input.Title, CreatedAt: now, UpdatedAt: now,
	}
	_, err := a.db.Exec(`INSERT INTO tasks(id, title, completed, created_at, updated_at) VALUES(?, ?, 0, ?, ?)`,
		task.ID, task.Title, task.CreatedAt, task.UpdatedAt)
	return task, err
}

func (a *App) SetTaskCompleted(taskID string, completed bool) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.ready(); err != nil {
		return err
	}
	tx, err := a.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if completed {
		if err := a.stopTimerTx(tx, taskID); err != nil {
			return err
		}
	}
	result, err := tx.Exec(`UPDATE tasks SET completed = ?, updated_at = ? WHERE id = ?`, completed, time.Now().UTC().Format(time.RFC3339Nano), taskID)
	if err != nil {
		return err
	}
	count, _ := result.RowsAffected()
	if count == 0 {
		return errors.New("task not found")
	}
	return tx.Commit()
}

func (a *App) StartTimer(taskID string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.ready(); err != nil {
		return err
	}
	tx, err := a.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var completed bool
	if err := tx.QueryRow(`SELECT completed FROM tasks WHERE id = ?`, taskID).Scan(&completed); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return errors.New("task not found")
		}
		return err
	}
	if completed {
		return errors.New("completed tasks cannot be timed")
	}
	if err := a.stopTimerTx(tx, ""); err != nil {
		return err
	}
	_, err = tx.Exec(`INSERT INTO active_timer(singleton, task_id, started_at, device_id) VALUES(1, ?, ?, ?)`,
		taskID, time.Now().UTC().Format(time.RFC3339Nano), a.deviceID)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (a *App) StopTimer(note string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.ready(); err != nil {
		return err
	}
	tx, err := a.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := a.stopTimerTxWithNote(tx, "", strings.TrimSpace(note)); err != nil {
		return err
	}
	return tx.Commit()
}

// GetTaskTimeSummary returns rolling totals based on the exact persisted start
// and end timestamps. The windows are elapsed periods, not calendar buckets.
func (a *App) GetTaskTimeSummary(taskID string) (TaskTimeSummary, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	summary := TaskTimeSummary{TaskID: taskID}
	if err := a.ready(); err != nil {
		return summary, err
	}

	rows, err := a.db.Query(`SELECT started_at, ended_at, duration_seconds FROM time_entries WHERE task_id = ?`, taskID)
	if err != nil {
		return summary, err
	}
	defer rows.Close()

	now := time.Now().UTC()
	for rows.Next() {
		var startedAt, endedAt string
		var duration int64
		if err := rows.Scan(&startedAt, &endedAt, &duration); err != nil {
			return summary, err
		}
		started, startErr := time.Parse(time.RFC3339Nano, startedAt)
		ended, endErr := time.Parse(time.RFC3339Nano, endedAt)
		if startErr != nil || endErr != nil || !ended.After(started) {
			continue
		}
		summary.AllTimeSeconds += duration
		summary.LastDaySeconds += overlapSeconds(started, ended, now.Add(-24*time.Hour), now)
		summary.LastWeekSeconds += overlapSeconds(started, ended, now.Add(-7*24*time.Hour), now)
		summary.LastMonthSeconds += overlapSeconds(started, ended, now.Add(-30*24*time.Hour), now)
	}
	if err := rows.Err(); err != nil {
		return summary, err
	}

	var activeStartedAt string
	err = a.db.QueryRow(`SELECT started_at FROM active_timer WHERE singleton = 1 AND task_id = ?`, taskID).Scan(&activeStartedAt)
	if err == nil {
		if started, parseErr := time.Parse(time.RFC3339Nano, activeStartedAt); parseErr == nil && now.After(started) {
			summary.AllTimeSeconds += int64(now.Sub(started).Seconds())
			summary.LastDaySeconds += overlapSeconds(started, now, now.Add(-24*time.Hour), now)
			summary.LastWeekSeconds += overlapSeconds(started, now, now.Add(-7*24*time.Hour), now)
			summary.LastMonthSeconds += overlapSeconds(started, now, now.Add(-30*24*time.Hour), now)
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return summary, err
	}
	return summary, nil
}

func (a *App) stopTimerTx(tx *sql.Tx, onlyTaskID string) error {
	return a.stopTimerTxWithNote(tx, onlyTaskID, "")
}

func (a *App) stopTimerTxWithNote(tx *sql.Tx, onlyTaskID, note string) error {
	var taskID, startedAt, deviceID string
	err := tx.QueryRow(`SELECT task_id, started_at, device_id FROM active_timer WHERE singleton = 1`).Scan(&taskID, &startedAt, &deviceID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if onlyTaskID != "" && onlyTaskID != taskID {
		return nil
	}
	started, err := time.Parse(time.RFC3339Nano, startedAt)
	if err != nil {
		return err
	}
	ended := time.Now().UTC()
	duration := max(int64(1), int64(ended.Sub(started).Seconds()))
	nowText := ended.Format(time.RFC3339Nano)
	if _, err = tx.Exec(`INSERT INTO time_entries(id, task_id, started_at, ended_at, duration_seconds, note, device_id, created_at) VALUES(?, ?, ?, ?, ?, ?, ?, ?)`,
		newID(), taskID, startedAt, nowText, duration, note, deviceID, nowText); err != nil {
		return err
	}
	_, err = tx.Exec(`DELETE FROM active_timer WHERE singleton = 1`)
	return err
}

func (a *App) ExportBackup() (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	doc, err := a.buildSyncDocument()
	if err != nil {
		return "", err
	}
	path, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title:           "Export Punchcard Backup",
		DefaultFilename: fmt.Sprintf("punchcard-%s.json", time.Now().Format("2006-01-02")),
		Filters:         []runtime.FileFilter{{DisplayName: "JSON backup", Pattern: "*.json"}},
	})
	if err != nil || path == "" {
		return path, err
	}
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "", err
	}
	return path, nil
}

func (a *App) ImportBackup() (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.ready(); err != nil {
		return "", err
	}
	path, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title:   "Import Punchcard Backup",
		Filters: []runtime.FileFilter{{DisplayName: "JSON backup", Pattern: "*.json"}},
	})
	if err != nil || path == "" {
		return path, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	var doc SyncDocument
	if err := json.Unmarshal(data, &doc); err != nil {
		return "", fmt.Errorf("invalid Punchcard backup: %w", err)
	}
	if doc.SchemaVersion != 1 {
		return "", fmt.Errorf("unsupported backup schema version %d", doc.SchemaVersion)
	}
	tx, err := a.db.Begin()
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback() }()
	for _, task := range doc.Tasks {
		if strings.TrimSpace(task.ID) == "" || strings.TrimSpace(task.Title) == "" {
			continue
		}
		_, err = tx.Exec(`INSERT INTO tasks(id, title, completed, created_at, updated_at)
			VALUES(?, ?, ?, ?, ?)
			ON CONFLICT(id) DO UPDATE SET title=excluded.title, completed=excluded.completed, updated_at=excluded.updated_at
			WHERE excluded.updated_at > tasks.updated_at`, task.ID, task.Title, task.Completed, task.CreatedAt, task.UpdatedAt)
		if err != nil {
			return "", err
		}
	}
	for _, entry := range doc.Entries {
		_, err = tx.Exec(`INSERT OR IGNORE INTO time_entries(id, task_id, started_at, ended_at, duration_seconds, note, device_id, created_at) VALUES(?, ?, ?, ?, ?, ?, ?, ?)`,
			entry.ID, entry.TaskID, entry.StartedAt, entry.EndedAt, entry.DurationSeconds, entry.Note, entry.DeviceID, entry.CreatedAt)
		if err != nil {
			return "", err
		}
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return path, nil
}

func (a *App) buildSyncDocument() (SyncDocument, error) {
	state, err := a.getState()
	if err != nil {
		return SyncDocument{}, err
	}
	return SyncDocument{
		SchemaVersion: 1,
		ExportedAt:    time.Now().UTC().Format(time.RFC3339Nano),
		DeviceID:      a.deviceID,
		Tasks:         state.Tasks,
		Entries:       state.Entries,
	}, nil
}

func newID() string {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	value[6] = (value[6] & 0x0f) | 0x40
	value[8] = (value[8] & 0x3f) | 0x80
	text := hex.EncodeToString(value[:])
	return text[0:8] + "-" + text[8:12] + "-" + text[12:16] + "-" + text[16:20] + "-" + text[20:]
}

func overlapSeconds(start, end, windowStart, windowEnd time.Time) int64 {
	if start.Before(windowStart) {
		start = windowStart
	}
	if end.After(windowEnd) {
		end = windowEnd
	}
	if !end.After(start) {
		return 0
	}
	return int64(end.Sub(start).Seconds())
}

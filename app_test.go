package main

import (
	"path/filepath"
	"testing"
	"time"
)

func newTestApp(t *testing.T) *App {
	t.Helper()
	app := NewApp()
	if err := app.openDatabaseAt(filepath.Join(t.TempDir(), "punchcard-test.db")); err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(func() { _ = app.db.Close() })
	return app
}

func TestTaskAndTimerLifecycle(t *testing.T) {
	app := newTestApp(t)
	task, err := app.CreateTask(CreateTaskInput{Title: "Write release notes"})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	if task.Title != "Write release notes" || task.Completed {
		t.Fatalf("unexpected task: %#v", task)
	}

	if err := app.StartTimer(task.ID); err != nil {
		t.Fatalf("start timer: %v", err)
	}
	started := time.Now().UTC().Add(-65 * time.Minute).Format(time.RFC3339Nano)
	if _, err := app.db.Exec(`UPDATE active_timer SET started_at = ? WHERE singleton = 1`, started); err != nil {
		t.Fatalf("adjust timer: %v", err)
	}
	if err := app.StopTimer("focused session"); err != nil {
		t.Fatalf("stop timer: %v", err)
	}

	state, err := app.GetState()
	if err != nil {
		t.Fatalf("get state: %v", err)
	}
	if state.ActiveTimer != nil {
		t.Fatal("timer should no longer be active")
	}
	if len(state.Entries) != 1 {
		t.Fatalf("expected one time entry, got %d", len(state.Entries))
	}
	if state.Entries[0].DurationSeconds < 3899 || state.Entries[0].DurationSeconds > 3902 {
		t.Fatalf("unexpected duration: %d", state.Entries[0].DurationSeconds)
	}

	if err := app.SetTaskCompleted(task.ID, true); err != nil {
		t.Fatalf("complete task: %v", err)
	}
	state, err = app.GetState()
	if err != nil {
		t.Fatalf("get completed state: %v", err)
	}
	if len(state.Tasks) != 1 || !state.Tasks[0].Completed {
		t.Fatal("task was not completed")
	}
}

func TestStartingAnotherTaskPunchesOutCurrentTimer(t *testing.T) {
	app := newTestApp(t)
	first, _ := app.CreateTask(CreateTaskInput{Title: "First"})
	second, _ := app.CreateTask(CreateTaskInput{Title: "Second"})
	if err := app.StartTimer(first.ID); err != nil {
		t.Fatal(err)
	}
	if err := app.StartTimer(second.ID); err != nil {
		t.Fatal(err)
	}
	state, err := app.GetState()
	if err != nil {
		t.Fatal(err)
	}
	if state.ActiveTimer == nil || state.ActiveTimer.TaskID != second.ID {
		t.Fatal("second task should be active")
	}
	if len(state.Entries) != 1 || state.Entries[0].TaskID != first.ID {
		t.Fatal("first timer should have been saved as an entry")
	}
}

func TestSyncDocumentContainsLocalData(t *testing.T) {
	app := newTestApp(t)
	if _, err := app.CreateTask(CreateTaskInput{Title: "Pack a backup"}); err != nil {
		t.Fatal(err)
	}
	document, err := app.buildSyncDocument()
	if err != nil {
		t.Fatal(err)
	}
	if document.SchemaVersion != 1 || document.DeviceID == "" || len(document.Tasks) != 1 {
		t.Fatalf("unexpected sync document: %#v", document)
	}
}

func TestTaskTimeSummaryUsesRollingWindows(t *testing.T) {
	app := newTestApp(t)
	task, err := app.CreateTask(CreateTaskInput{Title: "Measure rolling time"})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	entries := []struct {
		start    time.Time
		end      time.Time
		duration int64
	}{
		{now.Add(-3 * time.Hour), now.Add(-1 * time.Hour), 2 * 60 * 60},
		{now.Add(-8*24*time.Hour - 10*time.Hour), now.Add(-8 * 24 * time.Hour), 10 * 60 * 60},
		{now.Add(-40*24*time.Hour - 5*time.Hour), now.Add(-40 * 24 * time.Hour), 5 * 60 * 60},
	}
	for _, entry := range entries {
		_, err := app.db.Exec(`INSERT INTO time_entries(id, task_id, started_at, ended_at, duration_seconds, note, device_id, created_at) VALUES(?, ?, ?, ?, ?, '', ?, ?)`,
			newID(), task.ID, entry.start.Format(time.RFC3339Nano), entry.end.Format(time.RFC3339Nano), entry.duration, app.deviceID, now.Format(time.RFC3339Nano))
		if err != nil {
			t.Fatal(err)
		}
	}

	summary, err := app.GetTaskTimeSummary(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if summary.LastDaySeconds != 2*60*60 || summary.LastWeekSeconds != 2*60*60 {
		t.Fatalf("unexpected recent totals: %#v", summary)
	}
	if summary.LastMonthSeconds != 12*60*60 || summary.AllTimeSeconds != 17*60*60 {
		t.Fatalf("unexpected long-range totals: %#v", summary)
	}
}

func TestDeleteTaskRequiresCompletionAndRemovesEntries(t *testing.T) {
	app := newTestApp(t)
	task, err := app.CreateTask(CreateTaskInput{Title: "Disposable"})
	if err != nil {
		t.Fatal(err)
	}
	if err := app.DeleteTask(task.ID); err == nil {
		t.Fatal("open task should not be deletable")
	}
	if err := app.StartTimer(task.ID); err != nil {
		t.Fatal(err)
	}
	if err := app.StopTimer(""); err != nil {
		t.Fatal(err)
	}
	if err := app.SetTaskCompleted(task.ID, true); err != nil {
		t.Fatal(err)
	}
	if err := app.DeleteTask(task.ID); err != nil {
		t.Fatalf("delete completed task: %v", err)
	}
	state, err := app.GetState()
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Tasks) != 0 || len(state.Entries) != 0 {
		t.Fatalf("task data was not deleted: %#v", state)
	}
}

func TestPauseResumePersistsSeparateSegments(t *testing.T) {
	app := newTestApp(t)
	task, err := app.CreateTask(CreateTaskInput{Title: "Pause safely"})
	if err != nil {
		t.Fatal(err)
	}
	if err := app.StartTimer(task.ID); err != nil {
		t.Fatal(err)
	}
	firstStart := time.Now().UTC().Add(-10 * time.Minute).Format(time.RFC3339Nano)
	if _, err := app.db.Exec(`UPDATE active_timer SET started_at = ? WHERE singleton = 1`, firstStart); err != nil {
		t.Fatal(err)
	}
	if err := app.PauseTimer(); err != nil {
		t.Fatal(err)
	}
	state, err := app.GetState()
	if err != nil {
		t.Fatal(err)
	}
	if state.ActiveTimer == nil || !state.ActiveTimer.Paused || state.ActiveTimer.SessionSeconds < 599 {
		t.Fatalf("timer was not persistently paused: %#v", state.ActiveTimer)
	}
	if len(state.Entries) != 1 {
		t.Fatalf("pause should save one segment, got %d", len(state.Entries))
	}
	if err := app.ResumeTimer(); err != nil {
		t.Fatal(err)
	}
	secondStart := time.Now().UTC().Add(-5 * time.Minute).Format(time.RFC3339Nano)
	if _, err := app.db.Exec(`UPDATE active_timer SET started_at = ? WHERE singleton = 1`, secondStart); err != nil {
		t.Fatal(err)
	}
	if err := app.StopTimer(""); err != nil {
		t.Fatal(err)
	}
	state, err = app.GetState()
	if err != nil {
		t.Fatal(err)
	}
	if state.ActiveTimer != nil || len(state.Entries) != 2 {
		t.Fatalf("punch out should close the resumed timer with two saved segments: %#v", state)
	}
}

package main

type Task struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Completed bool   `json:"completed"`
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
}

type TimeEntry struct {
	ID              string `json:"id"`
	TaskID          string `json:"taskId"`
	StartedAt       string `json:"startedAt"`
	EndedAt         string `json:"endedAt"`
	DurationSeconds int64  `json:"durationSeconds"`
	Note            string `json:"note"`
	DeviceID        string `json:"deviceId"`
	CreatedAt       string `json:"createdAt"`
}

type ActiveTimer struct {
	TaskID    string `json:"taskId"`
	TaskTitle string `json:"taskTitle"`
	StartedAt string `json:"startedAt"`
	DeviceID  string `json:"deviceId"`
}

type AppState struct {
	Tasks       []Task       `json:"tasks"`
	Entries     []TimeEntry  `json:"entries"`
	ActiveTimer *ActiveTimer `json:"activeTimer"`
}

type CreateTaskInput struct {
	Title string `json:"title"`
}

type TaskTimeSummary struct {
	TaskID           string `json:"taskId"`
	LastDaySeconds   int64  `json:"lastDaySeconds"`
	LastWeekSeconds  int64  `json:"lastWeekSeconds"`
	LastMonthSeconds int64  `json:"lastMonthSeconds"`
	AllTimeSeconds   int64  `json:"allTimeSeconds"`
}

type SyncDocument struct {
	SchemaVersion int         `json:"schemaVersion"`
	ExportedAt    string      `json:"exportedAt"`
	DeviceID      string      `json:"deviceId"`
	Tasks         []Task      `json:"tasks"`
	Entries       []TimeEntry `json:"entries"`
}

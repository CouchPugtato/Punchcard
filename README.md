# Punchcard

Punchcard is a local-first desktop timekeeper with a compact, classic Macintosh-inspired interface. It's built with Wails, Go, Svelte, TypeScript, and SQLite.

## What works

- Create, complete, and reopen tasks
- Run one durable timer at a time
- Automatically punch out the current task when another starts
- Recover an active timer after an app restart or system sleep
- Add manual start/end time ranges, including sessions crossing midnight
- Show accumulated time beside each task
- Right-click any task for rolling 24-hour, 7-day, 30-day, and all-time totals
- Export and merge versioned JSON backups
- Persist all data locally in SQLite
- Use keyboard shortcuts (`Ctrl/Cmd+N` for a new task and `Esc` to close the dialog)

The compact UI keeps the timer and its controls on the left and the complete task list on the right. It uses custom CSS rather than a platform theme, so the same retro design works on Windows, macOS, and Linux while retaining scalable text, focus indicators, and reduced-motion support.

## Development

Requirements:

- Go 1.23 or newer
- Node.js 20 or newer
- Wails CLI v2
- WebView2 on Windows

Install and run:

```powershell
cd frontend
npm install
cd ..
wails dev
```

Run the checks:

```powershell
go test ./...
cd frontend
npm run check
npm run build
```

Create a native package:

```powershell
wails build
```

The Windows build is written to `build/bin/Punchcard.exe`.

## Architecture

```text
Svelte + TypeScript UI
        │ generated Wails bindings
Go application service
        ├── task and timer operations
        ├── reports
        └── versioned sync document
        │
Local SQLite database
        │
JSON export/import ── future Google Drive provider
```

The database is created in the operating system's user configuration directory under `Punchcard/punchcard.db`. SQLite is the local source of truth; the app never attempts to synchronize the live database file.

The portable `SyncDocument` in `models.go` is the boundary for backup and cloud providers. Imports merge tasks by ID and update timestamp and deduplicate time entries by ID, making the existing JSON workflow suitable for validating the merge format before Drive is connected.

## Google Drive integration

Drive authentication is intentionally not hard-coded. A distributable build needs a Google Cloud desktop OAuth client owned by the publisher. Once its client ID is available, the next integration step is:

1. Add desktop OAuth using the system browser and loopback callback.
2. Request only the `drive.appdata` scope.
3. Store the refresh token in the operating system credential vault.
4. Upload/download the same versioned `SyncDocument` currently used by Export and Import.
5. Merge on launch, after local changes, on wake, and periodically while online.
6. Use Drive file versions/ETags and retry the download–merge–upload cycle on conflicts.

Do not commit an OAuth client secret or refresh token to this repository.

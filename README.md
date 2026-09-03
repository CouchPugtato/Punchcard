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
- Sync tasks and recorded time through Google Drive while remaining fully usable offline
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

Create the Windows installer (requires [NSIS](https://nsis.sourceforge.io/) to be installed):

```powershell
.\build-installer.ps1
```

The installer is written to `build/bin/Punchcard-amd64-installer.exe`. It installs per user under `%LOCALAPPDATA%\Programs\Punchcard`, creates Start Menu and Desktop shortcuts, registers an uninstaller, and does not remove the user's Punchcard database during uninstall.

## Architecture

```text
Svelte + TypeScript UI
        │ generated Wails bindings
Go application service
        ├── task and timer operations
        ├── reports
        ├── versioned sync/merge document
        └── Google OAuth + Drive app-data provider
        │
Local SQLite database
        ├── JSON export/import
        └── hidden Google Drive app-data file
```

The database is created in the operating system's user configuration directory under `Punchcard/punchcard.db`. SQLite is the local source of truth; the app never attempts to synchronize the live database file.

The portable `SyncDocument` in `models.go` is the boundary for backup and cloud providers. Sync merges tasks by ID and update timestamp, deduplicates immutable time entries by ID, and propagates deletions with timestamped tombstones. Active timers remain device-local; their saved segments sync after pause or punch-out.

## Google Drive integration

Drive authentication is intentionally not hard-coded. To connect a development build:

1. In Google Cloud, enable the Google Drive API and configure the OAuth consent screen.
2. Create an OAuth client with application type **Desktop app** and download its JSON file.
3. Open Punchcard's sync menu and choose **Connect Drive**.
4. Select the downloaded JSON when prompted, then finish sign-in in the system browser.

Punchcard copies the selected client configuration to the operating system's user configuration directory under `Punchcard/google_oauth_client.json`. On Windows, the refresh token is stored in Windows Credential Manager; other platforms use a user-only file in the same configuration directory. The app requests only the `drive.appdata` scope and stores `punchcard-sync-v1.json` in Drive's hidden application-data folder.

Sync runs at startup, two seconds after local changes settle, every five minutes, and when **Sync Now** is pressed. SQLite remains the local source of truth, so failed or unavailable sync never blocks time tracking. Concurrent changes trigger a fresh download–merge–upload attempt.

Publisher builds can inject credentials without the first-run JSON prompt by setting `PUNCHCARD_GOOGLE_CLIENT_ID` and optionally `PUNCHCARD_GOOGLE_CLIENT_SECRET`, or at link time with `-X main.googleClientID=...` and `-X main.googleClientSecret=...`.

Do not commit OAuth credential JSON or refresh tokens to this repository.

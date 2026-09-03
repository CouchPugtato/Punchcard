# Development

## Checks

```powershell
go test ./...
cd frontend
npm run check
npm run build
```

## Architecture

Punchcard uses a Svelte and TypeScript interface inside a Wails desktop window. Go handles task, timer, reporting, sync, and SQLite operations through generated Wails bindings.

The database is stored in the operating system's user configuration directory at `Punchcard/punchcard.db`. SQLite remains the local source of truth; the live database file is never uploaded.

Backups and cloud sync use the versioned `SyncDocument` model in `models.go`. Tasks merge by ID and update time, time entries are deduplicated by ID, and timestamped tombstones propagate deletions. Active timers stay on the device until paused or punched out.

## Windows installer

```powershell
.\build-installer.ps1
```

The installer is created at `build/bin/Punchcard-amd64-installer.exe`. It installs per user, creates Start Menu and Desktop shortcuts, and preserves the user's database when uninstalled.

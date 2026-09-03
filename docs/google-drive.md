# Google Drive sync

Punchcard works offline without Google Drive. Drive sync stores a versioned file in the signed-in user's hidden app-data folder.

## Development setup

1. Create a project in [Google Cloud Console](https://console.cloud.google.com/).
2. Enable the Google Drive API.
3. Configure the OAuth consent screen and add yourself as a test user.
4. Create an OAuth client with the **Desktop app** type and download its JSON file.
5. In Punchcard, open the sync menu, choose **Connect Drive**, and select that file.

Punchcard requests only the `drive.appdata` scope. On Windows, its refresh token is kept in Windows Credential Manager. Never commit OAuth JSON files or refresh tokens.

Sync runs when the app starts, shortly after local changes, every five minutes, and when **Sync Now** is selected. Failed syncs do not prevent local time tracking.

Publisher builds can provide credentials with `PUNCHCARD_GOOGLE_CLIENT_ID` and optionally `PUNCHCARD_GOOGLE_CLIENT_SECRET`, or through Go linker values named `main.googleClientID` and `main.googleClientSecret`.

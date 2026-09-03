package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/textproto"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

const (
	driveFileName = "punchcard-sync-v1.json"
	driveScope    = "https://www.googleapis.com/auth/drive.appdata"
	tokenTarget   = "Punchcard/GoogleDrive"
)

var (
	googleClientID     string // May be injected with -ldflags "-X main.googleClientID=...".
	googleClientSecret string
	errDriveConflict   = errors.New("the Drive copy changed during sync")
)

type oauthCredentials struct {
	ClientID     string   `json:"client_id"`
	ClientSecret string   `json:"client_secret"`
	AuthURI      string   `json:"auth_uri"`
	TokenURI     string   `json:"token_uri"`
	RedirectURIs []string `json:"redirect_uris"`
}

type oauthToken struct {
	AccessToken  string    `json:"accessToken"`
	RefreshToken string    `json:"refreshToken"`
	TokenType    string    `json:"tokenType"`
	ExpiresAt    time.Time `json:"expiresAt"`
}

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int64  `json:"expires_in"`
	Error        string `json:"error"`
	Description  string `json:"error_description"`
}

type driveFile struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	ModifiedTime string `json:"modifiedTime"`
	Version      string `json:"version"`
}

func (a *App) startDriveSync() {
	ctx, cancel := context.WithCancel(context.Background())
	a.syncCancel = cancel
	_, configuredErr := a.loadOAuthCredentials(false)
	_, tokenErr := loadSecureToken(a.dataDir)
	configured := configuredErr == nil
	status := DriveSyncStatus{Configured: configured, State: "disconnected", Message: "Saved locally"}
	if tokenErr == nil {
		status.Connected = true
		if configured {
			status.State = "idle"
			status.Message = "Waiting to sync"
		} else {
			status.State = "error"
			status.Message = "Google OAuth configuration is missing"
		}
	}
	a.setDriveSyncStatus(status)
	go a.driveSyncLoop(ctx)
	if status.Connected && status.Configured {
		a.scheduleDriveSync()
	}
}

func (a *App) driveSyncLoop(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	var debounce *time.Timer
	var debounceC <-chan time.Time
	for {
		select {
		case <-ctx.Done():
			if debounce != nil {
				debounce.Stop()
			}
			return
		case <-a.syncSignal:
			if debounce == nil {
				debounce = time.NewTimer(2 * time.Second)
			} else {
				if !debounce.Stop() {
					select {
					case <-debounce.C:
					default:
					}
				}
				debounce.Reset(2 * time.Second)
			}
			debounceC = debounce.C
		case <-debounceC:
			debounceC = nil
			a.runAutomaticDriveSync()
		case <-ticker.C:
			a.runAutomaticDriveSync()
		}
	}
}

func (a *App) runAutomaticDriveSync() {
	status := a.GetDriveSyncStatus()
	if !status.Connected || !status.Configured {
		return
	}
	if _, err := a.SyncNow(); err != nil && a.ctx != nil {
		runtime.LogWarningf(a.ctx, "Google Drive sync failed: %v", err)
	}
}

func (a *App) scheduleDriveSync() {
	status := a.GetDriveSyncStatus()
	if status.Connected && status.Configured && status.State != "syncing" {
		status.State = "pending"
		status.Message = "Local changes waiting to sync"
		a.setDriveSyncStatus(status)
	}
	select {
	case a.syncSignal <- struct{}{}:
	default:
	}
}

func (a *App) GetDriveSyncStatus() DriveSyncStatus {
	a.statusMu.RLock()
	defer a.statusMu.RUnlock()
	return a.syncStatus
}

func (a *App) setDriveSyncStatus(status DriveSyncStatus) {
	a.statusMu.Lock()
	if status.LastSyncedAt == "" {
		status.LastSyncedAt = a.syncStatus.LastSyncedAt
	}
	a.syncStatus = status
	a.statusMu.Unlock()
	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, "drive:status", status)
	}
}

func (a *App) ConnectGoogleDrive() (DriveSyncStatus, error) {
	a.syncMu.Lock()
	defer a.syncMu.Unlock()
	credentials, err := a.loadOAuthCredentials(true)
	if err != nil {
		status := DriveSyncStatus{Configured: false, State: "error", Message: err.Error()}
		a.setDriveSyncStatus(status)
		return status, err
	}
	a.setDriveSyncStatus(DriveSyncStatus{Configured: true, State: "syncing", Message: "Waiting for Google sign-in"})
	token, err := a.authorizeGoogle(credentials)
	if err != nil {
		status := DriveSyncStatus{Configured: true, State: "error", Message: err.Error()}
		a.setDriveSyncStatus(status)
		return status, err
	}
	if err := saveSecureToken(a.dataDir, token); err != nil {
		status := DriveSyncStatus{Configured: true, State: "error", Message: "Could not securely save Google access"}
		a.setDriveSyncStatus(status)
		return status, err
	}
	a.setDriveSyncStatus(DriveSyncStatus{Connected: true, Configured: true, State: "syncing", Message: "Preparing first sync"})
	return a.syncDrive(credentials, &token)
}

func (a *App) DisconnectGoogleDrive() (DriveSyncStatus, error) {
	a.syncMu.Lock()
	defer a.syncMu.Unlock()
	err := deleteSecureToken(a.dataDir)
	configured := true
	if _, configErr := a.loadOAuthCredentials(false); configErr != nil {
		configured = false
	}
	status := DriveSyncStatus{Configured: configured, State: "disconnected", Message: "Saved locally"}
	a.setDriveSyncStatus(status)
	return status, err
}

func (a *App) SyncNow() (DriveSyncStatus, error) {
	a.syncMu.Lock()
	defer a.syncMu.Unlock()
	credentials, err := a.loadOAuthCredentials(false)
	if err != nil {
		status := DriveSyncStatus{Connected: true, State: "error", Message: "Google OAuth configuration is missing"}
		a.setDriveSyncStatus(status)
		return status, err
	}
	token, err := loadSecureToken(a.dataDir)
	if err != nil {
		status := DriveSyncStatus{Configured: true, State: "disconnected", Message: "Connect Google Drive to sync"}
		a.setDriveSyncStatus(status)
		return status, errors.New("Google Drive is not connected")
	}
	a.setDriveSyncStatus(DriveSyncStatus{Connected: true, Configured: true, State: "syncing", Message: "Syncing with Google Drive"})
	return a.syncDrive(credentials, &token)
}

func (a *App) syncDrive(credentials oauthCredentials, token *oauthToken) (DriveSyncStatus, error) {
	if err := a.refreshTokenIfNeeded(credentials, token, false); err != nil {
		return a.failDriveSync(err)
	}
	for attempt := 0; attempt < 3; attempt++ {
		files, err := a.findDriveFiles(token)
		if err != nil {
			return a.failDriveSync(err)
		}
		if len(files) == 0 {
			a.mu.Lock()
			doc, buildErr := a.buildSyncDocument()
			a.mu.Unlock()
			if buildErr != nil {
				return a.failDriveSync(buildErr)
			}
			if err := a.createDriveFile(token, doc); err != nil {
				return a.failDriveSync(err)
			}
			// Recheck shortly in case another new device created the same named
			// app-data file concurrently. The next pass merges both copies.
			a.scheduleDriveSync()
			return a.finishDriveSync()
		}

		remoteDocuments := make([]SyncDocument, 0, len(files))
		canonicalETag := ""
		for index, file := range files {
			remote, etag, downloadErr := a.downloadDriveDocument(token, file.ID)
			if downloadErr != nil {
				return a.failDriveSync(downloadErr)
			}
			remoteDocuments = append(remoteDocuments, remote)
			if index == 0 {
				canonicalETag = etag
			}
		}
		a.mu.Lock()
		for _, remote := range remoteDocuments {
			if err = a.mergeSyncDocument(remote); err != nil {
				break
			}
		}
		var merged SyncDocument
		if err == nil {
			merged, err = a.buildSyncDocument()
		}
		a.mu.Unlock()
		if err != nil {
			return a.failDriveSync(err)
		}
		if err = a.updateDriveFile(token, files[0].ID, canonicalETag, merged); errors.Is(err, errDriveConflict) {
			continue
		} else if err != nil {
			return a.failDriveSync(err)
		}
		if a.ctx != nil {
			runtime.EventsEmit(a.ctx, "drive:data-changed")
		}
		return a.finishDriveSync()
	}
	return a.failDriveSync(errors.New("Drive kept changing; try Sync Now again"))
}

func (a *App) finishDriveSync() (DriveSyncStatus, error) {
	status := DriveSyncStatus{
		Connected: true, Configured: true, State: "idle", Message: "Synced with Google Drive",
		LastSyncedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}
	a.setDriveSyncStatus(status)
	return status, nil
}

func (a *App) failDriveSync(err error) (DriveSyncStatus, error) {
	status := DriveSyncStatus{Connected: true, Configured: true, State: "error", Message: cleanDriveError(err)}
	a.setDriveSyncStatus(status)
	return status, err
}

func cleanDriveError(err error) string {
	text := strings.TrimSpace(err.Error())
	if len(text) > 110 {
		text = text[:107] + "..."
	}
	return text
}

func (a *App) loadOAuthCredentials(choose bool) (oauthCredentials, error) {
	credentials := oauthCredentials{
		ClientID:     strings.TrimSpace(firstNonEmpty(os.Getenv("PUNCHCARD_GOOGLE_CLIENT_ID"), googleClientID)),
		ClientSecret: strings.TrimSpace(firstNonEmpty(os.Getenv("PUNCHCARD_GOOGLE_CLIENT_SECRET"), googleClientSecret)),
		AuthURI:      "https://accounts.google.com/o/oauth2/v2/auth",
		TokenURI:     "https://oauth2.googleapis.com/token",
	}
	if credentials.ClientID != "" {
		return credentials, nil
	}
	path := filepath.Join(a.dataDir, "google_oauth_client.json")
	data, err := os.ReadFile(path)
	if err != nil && choose {
		path, err = runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
			Title:   "Choose Google Desktop OAuth credentials",
			Filters: []runtime.FileFilter{{DisplayName: "Google OAuth JSON", Pattern: "*.json"}},
		})
		if err == nil && path == "" {
			err = errors.New("Google Drive connection was cancelled")
		}
		if err == nil {
			data, err = os.ReadFile(path)
		}
	}
	if err != nil {
		return oauthCredentials{}, errors.New("choose a Google Desktop OAuth JSON file to connect")
	}
	parsed, err := parseOAuthCredentials(data)
	if err != nil {
		return oauthCredentials{}, err
	}
	if choose {
		if writeErr := os.WriteFile(filepath.Join(a.dataDir, "google_oauth_client.json"), data, 0o600); writeErr != nil {
			return oauthCredentials{}, writeErr
		}
	}
	return parsed, nil
}

func parseOAuthCredentials(data []byte) (oauthCredentials, error) {
	var envelope struct {
		Installed oauthCredentials `json:"installed"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return oauthCredentials{}, errors.New("invalid Google OAuth JSON file")
	}
	credentials := envelope.Installed
	if strings.TrimSpace(credentials.ClientID) == "" {
		return oauthCredentials{}, errors.New("the JSON must contain Desktop app OAuth credentials")
	}
	if credentials.AuthURI == "" {
		credentials.AuthURI = "https://accounts.google.com/o/oauth2/v2/auth"
	}
	if credentials.TokenURI == "" {
		credentials.TokenURI = "https://oauth2.googleapis.com/token"
	}
	return credentials, nil
}

func (a *App) authorizeGoogle(credentials oauthCredentials) (oauthToken, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return oauthToken{}, err
	}
	defer listener.Close()
	redirectURI := "http://" + listener.Addr().String() + "/oauth/callback"
	verifier, err := randomURLText(32)
	if err != nil {
		return oauthToken{}, err
	}
	state, err := randomURLText(24)
	if err != nil {
		return oauthToken{}, err
	}
	challengeBytes := sha256.Sum256([]byte(verifier))
	params := url.Values{
		"client_id": {credentials.ClientID}, "redirect_uri": {redirectURI}, "response_type": {"code"},
		"scope": {driveScope}, "access_type": {"offline"}, "prompt": {"consent"}, "state": {state},
		"code_challenge": {base64.RawURLEncoding.EncodeToString(challengeBytes[:])}, "code_challenge_method": {"S256"},
	}
	type authResult struct {
		code string
		err  error
	}
	result := make(chan authResult, 1)
	mux := http.NewServeMux()
	mux.HandleFunc("/oauth/callback", func(writer http.ResponseWriter, request *http.Request) {
		query := request.URL.Query()
		if query.Get("state") != state {
			result <- authResult{err: errors.New("Google sign-in returned an invalid state")}
			http.Error(writer, "Punchcard could not verify this sign-in.", http.StatusBadRequest)
			return
		}
		if message := query.Get("error"); message != "" {
			result <- authResult{err: fmt.Errorf("Google sign-in failed: %s", message)}
			http.Error(writer, "Google sign-in was not completed.", http.StatusBadRequest)
			return
		}
		code := query.Get("code")
		if code == "" {
			result <- authResult{err: errors.New("Google sign-in returned no authorization code")}
			http.Error(writer, "Google sign-in returned no code.", http.StatusBadRequest)
			return
		}
		writer.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(writer, `<!doctype html><meta charset="utf-8"><title>Punchcard connected</title><body style="font-family:monospace;background:#f2ead0;color:#493f30;padding:3rem"><h2>Punchcard is connected.</h2><p>You can close this window and return to the app.</p><script>setTimeout(()=>window.close(),1200)</script></body>`)
		result <- authResult{code: code}
	})
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = server.Serve(listener) }()
	runtime.BrowserOpenURL(a.ctx, credentials.AuthURI+"?"+params.Encode())

	var auth authResult
	select {
	case auth = <-result:
	case <-time.After(3 * time.Minute):
		auth.err = errors.New("Google sign-in timed out")
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_ = server.Shutdown(shutdownCtx)
	if auth.err != nil {
		return oauthToken{}, auth.err
	}
	return exchangeAuthorizationCode(credentials, auth.code, verifier, redirectURI)
}

func exchangeAuthorizationCode(credentials oauthCredentials, code, verifier, redirectURI string) (oauthToken, error) {
	form := url.Values{
		"client_id": {credentials.ClientID}, "code": {code}, "code_verifier": {verifier},
		"redirect_uri": {redirectURI}, "grant_type": {"authorization_code"},
	}
	if credentials.ClientSecret != "" {
		form.Set("client_secret", credentials.ClientSecret)
	}
	response, err := (&http.Client{Timeout: 30 * time.Second}).PostForm(credentials.TokenURI, form)
	if err != nil {
		return oauthToken{}, err
	}
	defer response.Body.Close()
	var payload tokenResponse
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&payload); err != nil {
		return oauthToken{}, err
	}
	if response.StatusCode >= 300 || payload.AccessToken == "" {
		return oauthToken{}, fmt.Errorf("Google token exchange failed: %s", firstNonEmpty(payload.Description, payload.Error, response.Status))
	}
	return oauthToken{AccessToken: payload.AccessToken, RefreshToken: payload.RefreshToken, TokenType: payload.TokenType, ExpiresAt: time.Now().Add(time.Duration(payload.ExpiresIn) * time.Second)}, nil
}

func (a *App) refreshTokenIfNeeded(credentials oauthCredentials, token *oauthToken, force bool) error {
	if !force && token.AccessToken != "" && time.Now().Before(token.ExpiresAt.Add(-time.Minute)) {
		return nil
	}
	if token.RefreshToken == "" {
		return errors.New("Google access expired; reconnect Drive")
	}
	form := url.Values{"client_id": {credentials.ClientID}, "refresh_token": {token.RefreshToken}, "grant_type": {"refresh_token"}}
	if credentials.ClientSecret != "" {
		form.Set("client_secret", credentials.ClientSecret)
	}
	response, err := (&http.Client{Timeout: 30 * time.Second}).PostForm(credentials.TokenURI, form)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	var payload tokenResponse
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&payload); err != nil {
		return err
	}
	if response.StatusCode >= 300 || payload.AccessToken == "" {
		return fmt.Errorf("Google token refresh failed: %s", firstNonEmpty(payload.Description, payload.Error, response.Status))
	}
	token.AccessToken = payload.AccessToken
	token.TokenType = payload.TokenType
	token.ExpiresAt = time.Now().Add(time.Duration(payload.ExpiresIn) * time.Second)
	if payload.RefreshToken != "" {
		token.RefreshToken = payload.RefreshToken
	}
	return saveSecureToken(a.dataDir, *token)
}

func (a *App) findDriveFiles(token *oauthToken) ([]driveFile, error) {
	query := url.Values{
		"spaces":   {"appDataFolder"},
		"q":        {fmt.Sprintf("name = '%s' and trashed = false", driveFileName)},
		"orderBy":  {"modifiedTime desc"},
		"pageSize": {"100"},
		"fields":   {"files(id,name,modifiedTime,version)"},
	}
	request, _ := http.NewRequest(http.MethodGet, "https://www.googleapis.com/drive/v3/files?"+query.Encode(), nil)
	response, err := doDriveRequest(token, request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if err := driveResponseError(response); err != nil {
		return nil, err
	}
	var payload struct {
		Files []driveFile `json:"files"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&payload); err != nil {
		return nil, err
	}
	return payload.Files, nil
}

func (a *App) downloadDriveDocument(token *oauthToken, fileID string) (SyncDocument, string, error) {
	request, _ := http.NewRequest(http.MethodGet, "https://www.googleapis.com/drive/v3/files/"+url.PathEscape(fileID)+"?alt=media", nil)
	response, err := doDriveRequest(token, request)
	if err != nil {
		return SyncDocument{}, "", err
	}
	defer response.Body.Close()
	if err := driveResponseError(response); err != nil {
		return SyncDocument{}, "", err
	}
	var doc SyncDocument
	if err := json.NewDecoder(io.LimitReader(response.Body, 32<<20)).Decode(&doc); err != nil {
		return SyncDocument{}, "", fmt.Errorf("invalid Punchcard data in Drive: %w", err)
	}
	return doc, response.Header.Get("ETag"), nil
}

func (a *App) createDriveFile(token *oauthToken, doc SyncDocument) error {
	data, err := json.Marshal(doc)
	if err != nil {
		return err
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	boundary := writer.Boundary()
	metadataHeader := makeTextHeader("application/json; charset=UTF-8")
	metadataPart, _ := writer.CreatePart(metadataHeader)
	_, _ = metadataPart.Write([]byte(`{"name":"` + driveFileName + `","parents":["appDataFolder"],"mimeType":"application/json"}`))
	mediaPart, _ := writer.CreatePart(makeTextHeader("application/json"))
	_, _ = mediaPart.Write(data)
	_ = writer.Close()
	request, _ := http.NewRequest(http.MethodPost, "https://www.googleapis.com/upload/drive/v3/files?uploadType=multipart&fields=id", &body)
	request.Header.Set("Content-Type", "multipart/related; boundary="+boundary)
	response, err := doDriveRequest(token, request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	return driveResponseError(response)
}

func makeTextHeader(contentType string) textproto.MIMEHeader {
	return textproto.MIMEHeader{"Content-Type": {contentType}}
}

func (a *App) updateDriveFile(token *oauthToken, fileID, etag string, doc SyncDocument) error {
	data, err := json.Marshal(doc)
	if err != nil {
		return err
	}
	request, _ := http.NewRequest(http.MethodPatch, "https://www.googleapis.com/upload/drive/v3/files/"+url.PathEscape(fileID)+"?uploadType=media&fields=id,modifiedTime,version", bytes.NewReader(data))
	request.Header.Set("Content-Type", "application/json")
	if etag != "" {
		request.Header.Set("If-Match", etag)
	}
	response, err := doDriveRequest(token, request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusPreconditionFailed {
		return errDriveConflict
	}
	return driveResponseError(response)
}

func doDriveRequest(token *oauthToken, request *http.Request) (*http.Response, error) {
	request.Header.Set("Authorization", "Bearer "+token.AccessToken)
	return (&http.Client{Timeout: 45 * time.Second}).Do(request)
}

func driveResponseError(response *http.Response) error {
	if response.StatusCode < 300 {
		return nil
	}
	data, _ := io.ReadAll(io.LimitReader(response.Body, 8<<10))
	var payload struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	_ = json.Unmarshal(data, &payload)
	return fmt.Errorf("Google Drive request failed: %s", firstNonEmpty(payload.Error.Message, response.Status))
}

func randomURLText(size int) (string, error) {
	data := make([]byte, size)
	if _, err := rand.Read(data); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

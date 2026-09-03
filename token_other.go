//go:build !windows

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
)

func tokenPath(dataDir string) string { return filepath.Join(dataDir, "google_drive_token.json") }

func saveSecureToken(dataDir string, token oauthToken) error {
	data, err := json.Marshal(token)
	if err != nil {
		return err
	}
	return os.WriteFile(tokenPath(dataDir), data, 0o600)
}

func loadSecureToken(dataDir string) (oauthToken, error) {
	data, err := os.ReadFile(tokenPath(dataDir))
	if err != nil {
		return oauthToken{}, err
	}
	var token oauthToken
	if err := json.Unmarshal(data, &token); err != nil {
		return oauthToken{}, err
	}
	return token, nil
}

func deleteSecureToken(dataDir string) error {
	err := os.Remove(tokenPath(dataDir))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

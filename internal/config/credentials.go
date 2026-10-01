package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// ErrCredentialsNotFound is returned by LoadCredentials when the
// credentials file does not exist. It is not necessarily fatal to a
// caller: v1 runs that don't yet need ESI auth (ticket #16 is the first
// that does) can proceed without credentials.
var ErrCredentialsNotFound = errors.New("credentials file not found")

// Credentials are the OAuth PKCE client's identity and refresh state (spec
// §12, §13), read from credentials.json (mode 600) at the config directory.
// There is deliberately no CLI flag for any of these fields — the
// credentials file is the only source (spec §13).
type Credentials struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	RedirectURI  string `json:"redirect_uri"`
	RefreshToken string `json:"refresh_token"`
}

// LoadCredentials reads and parses the credentials file at path. It
// enforces mode 600 (spec §13) and returns ErrCredentialsNotFound if the
// file does not exist; both wrong permissions and malformed JSON are
// returned as clear, path-naming errors.
func LoadCredentials(path string) (Credentials, error) {
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return Credentials{}, ErrCredentialsNotFound
	}
	if err != nil {
		return Credentials{}, fmt.Errorf("reading credentials %s: %w", path, err)
	}

	if perm := info.Mode().Perm(); perm != 0o600 {
		return Credentials{}, fmt.Errorf("credentials %s must be mode 600, got %#o", path, perm)
	}

	body, err := os.ReadFile(path)
	if err != nil {
		return Credentials{}, fmt.Errorf("reading credentials %s: %w", path, err)
	}

	var creds Credentials
	if err := json.Unmarshal(body, &creds); err != nil {
		return Credentials{}, fmt.Errorf("parsing credentials %s: %w", path, err)
	}
	return creds, nil
}

// SaveCredentials writes creds to path as mode-600 JSON, creating or
// replacing the file atomically (write to a temp file in the same
// directory, then rename). Ticket #16 uses this to persist a rotated
// refresh token (spec §12: "the refresh token ... rotates — persist the
// returned one every time") without ever leaving a partially-written or
// wrong-permission credentials.json on disk. The config directory itself
// is created (mode 700, matching the 600 file it will hold) if it doesn't
// exist yet: `eve-trader login` is the first command that ever writes
// here, so on a fresh install ~/.config/eve-trader has never been created
// by anything else (diagnosed: CreateTemp below failed with "no such file
// or directory" the first time a pilot logged in on a clean machine).
func SaveCredentials(path string, creds Credentials) error {
	body, err := json.MarshalIndent(creds, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding credentials %s: %w", path, err)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("creating config directory for %s: %w", path, err)
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("writing credentials %s: %w", path, err)
	}
	defer os.Remove(tmp.Name())

	if _, err := tmp.Write(body); err != nil {
		tmp.Close()
		return fmt.Errorf("writing credentials %s: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("writing credentials %s: %w", path, err)
	}
	if err := os.Chmod(tmp.Name(), 0o600); err != nil {
		return fmt.Errorf("writing credentials %s: %w", path, err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("writing credentials %s: %w", path, err)
	}
	return nil
}

package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
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

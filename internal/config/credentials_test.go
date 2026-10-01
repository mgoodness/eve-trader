package config_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/mgoodness/eve-trader/internal/config"
)

const validCredentialsJSON = `{
  "client_id": "abc123",
  "client_secret": "shh",
  "redirect_uri": "http://127.0.0.1:8000/callback",
  "refresh_token": "refresh-abc"
}`

func writeCredentials(t *testing.T, body string, perm os.FileMode) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "credentials.json")
	if err := os.WriteFile(path, []byte(body), perm); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return path
}

func TestLoadCredentialsParsesAMode600File(t *testing.T) {
	path := writeCredentials(t, validCredentialsJSON, 0o600)

	got, err := config.LoadCredentials(path)
	if err != nil {
		t.Fatalf("LoadCredentials: %v", err)
	}

	want := config.Credentials{
		ClientID:     "abc123",
		ClientSecret: "shh",
		RedirectURI:  "http://127.0.0.1:8000/callback",
		RefreshToken: "refresh-abc",
	}
	if got != want {
		t.Errorf("LoadCredentials() = %+v, want %+v", got, want)
	}
}

func TestLoadCredentialsRejectsAFileThatIsNotMode600(t *testing.T) {
	path := writeCredentials(t, validCredentialsJSON, 0o644)

	_, err := config.LoadCredentials(path)
	if err == nil {
		t.Fatal("LoadCredentials(mode 644) returned no error, want one")
	}
}

func TestLoadCredentialsReturnsErrCredentialsNotFoundWhenMissing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials.json")

	_, err := config.LoadCredentials(path)
	if !errors.Is(err, config.ErrCredentialsNotFound) {
		t.Errorf("LoadCredentials(missing) error = %v, want ErrCredentialsNotFound", err)
	}
}

func TestLoadCredentialsReturnsAClearErrorForMalformedJSON(t *testing.T) {
	path := writeCredentials(t, "not valid json", 0o600)

	_, err := config.LoadCredentials(path)
	if err == nil {
		t.Fatal("LoadCredentials(malformed) returned no error, want one")
	}
}

func TestSaveCredentialsRoundTripsWithLoadCredentials(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials.json")
	want := config.Credentials{
		ClientID:     "abc123",
		ClientSecret: "shh",
		RedirectURI:  "http://127.0.0.1:8000/callback",
		RefreshToken: "rotated-refresh-token",
	}

	if err := config.SaveCredentials(path, want); err != nil {
		t.Fatalf("SaveCredentials: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("got mode %#o, want 0600", perm)
	}

	got, err := config.LoadCredentials(path)
	if err != nil {
		t.Fatalf("LoadCredentials: %v", err)
	}
	if got != want {
		t.Errorf("LoadCredentials() = %+v, want %+v", got, want)
	}
}

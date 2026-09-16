package tokencrypt_test

import (
	"bytes"
	"testing"

	"github.com/mgoodness/eve-trader/internal/tokencrypt"
)

func TestEncryptDecryptRoundTrip(t *testing.T) {
	want := "refresh-token-value"

	ciphertext, err := tokencrypt.Encrypt("correct-secret", want)
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}

	got, err := tokencrypt.Decrypt("correct-secret", ciphertext)
	if err != nil {
		t.Fatalf("Decrypt() error = %v", err)
	}
	if got != want {
		t.Errorf("Decrypt() = %q, want %q", got, want)
	}
}

func TestEncryptDoesNotStorePlaintext(t *testing.T) {
	plaintext := "super-secret-refresh-token"

	ciphertext, err := tokencrypt.Encrypt("secret", plaintext)
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}
	if bytes.Contains(ciphertext, []byte(plaintext)) {
		t.Errorf("Encrypt() ciphertext contains plaintext, want opaque bytes")
	}
}

func TestEncryptProducesDistinctCiphertextsForSamePlaintext(t *testing.T) {
	a, err := tokencrypt.Encrypt("secret", "same-plaintext")
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}
	b, err := tokencrypt.Encrypt("secret", "same-plaintext")
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}
	if bytes.Equal(a, b) {
		t.Errorf("Encrypt() produced identical ciphertexts for two calls, want distinct nonces")
	}
}

func TestDecryptWithWrongSecretFails(t *testing.T) {
	ciphertext, err := tokencrypt.Encrypt("right-secret", "plaintext")
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}

	if _, err := tokencrypt.Decrypt("wrong-secret", ciphertext); err == nil {
		t.Error("Decrypt() with wrong secret error = nil, want error")
	}
}

func TestDecryptTruncatedCiphertextFails(t *testing.T) {
	if _, err := tokencrypt.Decrypt("secret", []byte("short")); err == nil {
		t.Error("Decrypt() with truncated ciphertext error = nil, want error")
	}
}

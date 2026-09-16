// Package tokencrypt provides symmetric AES-256-GCM encryption for
// secrets that must be persisted at rest -- currently, the ESI refresh
// token stored in esi_token (see docs/spec/v1.md §6). The key is derived
// by SHA-256-hashing an arbitrary-length secret string, so operators can
// supply any passphrase via an environment variable rather than having to
// generate and encode a raw 32-byte key.
package tokencrypt

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"io"
)

// Encrypt encrypts plaintext with a key derived from secret, returning
// nonce||ciphertext||tag. Each call uses a fresh random nonce, so
// encrypting the same plaintext twice yields different ciphertexts.
func Encrypt(secret, plaintext string) ([]byte, error) {
	gcm, err := newGCM(secret)
	if err != nil {
		return nil, err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("generating nonce: %w", err)
	}

	return gcm.Seal(nonce, nonce, []byte(plaintext), nil), nil
}

// Decrypt reverses Encrypt. It returns an error if secret doesn't match
// the one Encrypt used, or ciphertext has been truncated or tampered
// with.
func Decrypt(secret string, ciphertext []byte) (string, error) {
	gcm, err := newGCM(secret)
	if err != nil {
		return "", err
	}

	if len(ciphertext) < gcm.NonceSize() {
		return "", fmt.Errorf("ciphertext too short: %d bytes", len(ciphertext))
	}
	nonce, ct := ciphertext[:gcm.NonceSize()], ciphertext[gcm.NonceSize():]

	plaintext, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return "", fmt.Errorf("decrypting: %w", err)
	}

	return string(plaintext), nil
}

// newGCM builds an AES-256-GCM AEAD from secret, hashed to a fixed-size
// key via SHA-256.
func newGCM(secret string) (cipher.AEAD, error) {
	key := sha256.Sum256([]byte(secret))

	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, fmt.Errorf("creating cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("creating GCM: %w", err)
	}

	return gcm, nil
}

// Package cache is a disk cache keyed by an arbitrary string (an ESI request
// URL, in practice), with a per-entry TTL. It exists so the CLI adapter can
// honor ESI's cache windows (region orders 300s, history until 11:05 daily,
// jumps 86,400s, skills 60s, standings 3,600s — spec §6) without refetching
// on every run. A stale entry still returns its ETag (GetWithETag), so a
// caller can conditionally revalidate it with If-None-Match instead of
// refetching the body outright (spec §6 "caching & politeness").
package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"
)

// Store is a disk cache rooted at a directory, one file per key.
type Store struct {
	dir string
}

// Open returns a Store rooted at dir, creating dir if it does not exist.
func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	return &Store{dir: dir}, nil
}

type entry struct {
	ExpiresAt time.Time `json:"expires_at"`
	ETag      string    `json:"etag,omitempty"`
	Body      []byte    `json:"body"`
}

// Get returns the value stored under key, and whether it is still fresh
// (within its TTL). A missing key returns a nil body, fresh=false, and no
// error. A stale (expired) key still returns its body, so a caller can
// choose to use stale data if a refetch fails.
func (s *Store) Get(key string) (body []byte, fresh bool, err error) {
	body, _, fresh, err = s.GetWithETag(key)
	return body, fresh, err
}

// Set stores value under key with the given TTL, replacing any prior value.
func (s *Store) Set(key string, value []byte, ttl time.Duration) error {
	return s.SetWithETag(key, value, "", ttl)
}

// GetWithETag behaves like Get, but also returns the entry's stored ETag
// (if any) regardless of freshness, so a caller can send a conditional
// If-None-Match request to revalidate a stale entry instead of refetching
// its body outright. A missing key returns a nil body, empty etag,
// fresh=false, and no error.
func (s *Store) GetWithETag(key string) (body []byte, etag string, fresh bool, err error) {
	raw, err := os.ReadFile(s.path(key))
	if errors.Is(err, os.ErrNotExist) {
		return nil, "", false, nil
	}
	if err != nil {
		return nil, "", false, err
	}

	var e entry
	if err := json.Unmarshal(raw, &e); err != nil {
		return nil, "", false, err
	}
	return e.Body, e.ETag, time.Now().Before(e.ExpiresAt), nil
}

// SetWithETag behaves like Set, but also stores the value's ETag, so a
// later GetWithETag can revalidate it once it goes stale.
func (s *Store) SetWithETag(key string, value []byte, etag string, ttl time.Duration) error {
	e := entry{ExpiresAt: time.Now().Add(ttl), ETag: etag, Body: value}
	raw, err := json.Marshal(e)
	if err != nil {
		return err
	}
	return os.WriteFile(s.path(key), raw, 0o600)
}

func (s *Store) path(key string) string {
	sum := sha256.Sum256([]byte(key))
	return filepath.Join(s.dir, hex.EncodeToString(sum[:])+".json")
}

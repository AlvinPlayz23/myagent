// Package sessionstate persists per-session settings that must outlive the
// process but must not leak into other conversations.
//
// It mirrors internal/config's conventions (same directory, atomic
// temp+rename writes, 0600 permissions, tolerant loads) so it reads as a
// native part of the config layout rather than a bolt-on. It is deliberately
// general: any session-scoped key/value can be stored here, so future
// session-only settings do not each need their own sidecar file.
//
// State is keyed by session ID and records the session file's path so a stored
// entry can be traced back to the conversation it belongs to.
package sessionstate

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/AlvinPlayz23/myagent/internal/filelock"
)

// currentVersion is the schema version written by this package.
const currentVersion = 1

// Session holds the stored values for one session.
type Session struct {
	// ID is the session id this entry belongs to (the key in Store.Sessions).
	ID string `json:"-"`
	// Path is the session .jsonl file the values belong to, recorded when the
	// entry is written so a stored value can be traced back to its transcript.
	Path string `json:"path,omitempty"`
	// UpdatedAt is the ISO 8601 timestamp of the last Set.
	UpdatedAt string `json:"updatedAt,omitempty"`
	// Values maps a setting key to its stored payload.
	Values map[string]json.RawMessage `json:"values,omitempty"`
}

// Store is the decoded contents of session-state.json.
type Store struct {
	// Version is the schema version of this file.
	Version int `json:"version"`
	// Sessions maps session ID to its stored settings.
	Sessions map[string]*Session `json:"sessions,omitempty"`
}

// newStore returns an initialized, empty store.
func newStore() *Store {
	return &Store{Version: currentVersion, Sessions: map[string]*Session{}}
}

// Get returns the raw stored value for a session key. The bool reports
// whether the session and key both exist.
func (s *Store) Get(id, key string) (json.RawMessage, bool) {
	if s == nil || s.Sessions == nil {
		return nil, false
	}
	entry, ok := s.Sessions[id]
	if !ok || entry == nil || entry.Values == nil {
		return nil, false
	}
	raw, ok := entry.Values[key]
	return raw, ok
}

// GetStringSlice decodes a stored []string value. It reports false when the
// key is absent or the payload is not a string slice, so a hand-edited or
// future-format file degrades to "unset" rather than failing.
func (s *Store) GetStringSlice(id, key string) ([]string, bool) {
	raw, ok := s.Get(id, key)
	if !ok {
		return nil, false
	}
	var out []string
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, false
	}
	return out, true
}

// Set stores value under a session key, creating the session entry if needed.
// path locates the session file; it may be empty. Each write refreshes
// UpdatedAt.
func (s *Store) Set(id, key string, value any, path string) error {
	if id == "" || key == "" {
		return fmt.Errorf("sessionstate: id and key are required")
	}
	if s.Sessions == nil {
		s.Sessions = map[string]*Session{}
	}
	entry, ok := s.Sessions[id]
	if !ok || entry == nil {
		entry = &Session{ID: id}
		s.Sessions[id] = entry
	}
	if entry.Values == nil {
		entry.Values = map[string]json.RawMessage{}
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("sessionstate: encode %q: %w", key, err)
	}
	entry.Values[key] = raw
	// Record the path only when we know one, so an earlier known path is not
	// erased by a later write from a context that lacks it.
	if path != "" {
		entry.Path = path
	}
	entry.UpdatedAt = time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	return nil
}

// Delete removes one key from a session. When it was the last key, the whole
// session entry is dropped so the file does not accumulate empty shells.
func (s *Store) Delete(id, key string) {
	if s == nil || s.Sessions == nil {
		return
	}
	entry, ok := s.Sessions[id]
	if !ok || entry == nil {
		return
	}
	delete(entry.Values, key)
	if len(entry.Values) == 0 {
		delete(s.Sessions, id)
	}
}

// Prune drops entries whose session id is not in live. It reports how many
// entries were removed. Sessions are never deleted by this codebase, so
// without pruning the file would grow one entry per session ever started.
func (s *Store) Prune(live map[string]bool) int {
	if s == nil || s.Sessions == nil {
		return 0
	}
	removed := 0
	for id := range s.Sessions {
		if !live[id] {
			delete(s.Sessions, id)
			removed++
		}
	}
	return removed
}

// Dir returns the directory holding session-state.json (~/.myagent), honoring
// MYAGENT_DIR like internal/config does.
func Dir() (string, error) {
	if d := os.Getenv("MYAGENT_DIR"); d != "" {
		return d, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".myagent"), nil
}

// Path returns the path to session-state.json within Dir().
func Path() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "session-state.json"), nil
}

// Load reads session-state.json. A missing, empty, or corrupt file is not an
// error: it yields an empty store, so a hand-edited or truncated file degrades
// to "no session state" instead of blocking startup.
func Load() (*Store, error) {
	s := newStore()
	path, err := Path()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	switch {
	case err != nil:
		if os.IsNotExist(err) {
			return s, nil
		}
		return nil, err
	case len(data) == 0:
		return s, nil
	}
	var loaded Store
	if err := json.Unmarshal(data, &loaded); err != nil {
		return s, nil // tolerate a corrupt file
	}
	if loaded.Sessions == nil {
		loaded.Sessions = map[string]*Session{}
	}
	for id, entry := range loaded.Sessions {
		if entry == nil {
			delete(loaded.Sessions, id)
			continue
		}
		entry.ID = id
	}
	if loaded.Version == 0 {
		loaded.Version = currentVersion
	}
	return &loaded, nil
}

// Save writes the store atomically with 0600 permissions, mirroring
// config.Save: a concurrent reader either sees the old file or the complete
// new one, never a partial write.
func Save(s *Store) error {
	if s == nil {
		s = newStore()
	}
	dir, err := Dir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	path, err := Path()
	if err != nil {
		return err
	}
	s.Version = currentVersion
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp, err := os.CreateTemp(dir, ".session-state-*.json.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
	}()
	if err := tmp.Chmod(0o600); err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

// File reloads before each write and locks the full read-modify-write operation
// across handles and processes so unrelated session settings are preserved.
type File struct {
	mu sync.Mutex
}

// NewFile returns a ready store handle.
func NewFile() *File { return &File{} }

// Set stores one key and rewrites the current file under a cross-process lock.
func (f *File) Set(id, key string, value any, path string) (err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	lock, err := acquireLock()
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, lock.Close()) }()
	s, err := Load()
	if err != nil {
		return err
	}
	if err := s.Set(id, key, value, path); err != nil {
		return err
	}
	return Save(s)
}

// GetStringSlice reads one key, reporting false when it is absent or the
// payload is not a string slice.
func (f *File) GetStringSlice(id, key string) ([]string, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, err := Load()
	if err != nil {
		return nil, false
	}
	return s.GetStringSlice(id, key)
}

// Prune drops entries not in live and reports how many were removed.
func (f *File) Prune(live map[string]bool) (removed int, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	lock, err := acquireLock()
	if err != nil {
		return 0, err
	}
	defer func() { err = errors.Join(err, lock.Close()) }()
	s, err := Load()
	if err != nil {
		return 0, err
	}
	removed = s.Prune(live)
	if removed == 0 {
		return 0, nil
	}
	if err := Save(s); err != nil {
		return 0, err
	}
	return removed, nil
}

func acquireLock() (*filelock.Lock, error) {
	path, err := Path()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	return filelock.Acquire(path)
}

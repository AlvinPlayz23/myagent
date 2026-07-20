// Package auth persists credentials for built-in catalog providers separately
// from user-created custom provider configuration.
package auth

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

type Credentials struct {
	APIKey  string `json:"apiKey"`
	BaseURL string `json:"baseUrl,omitempty"`
}

type Store struct {
	Providers map[string]Credentials `json:"providers"`
	path      string
}

func Load(root string) (*Store, error) {
	store := &Store{Providers: make(map[string]Credentials), path: filepath.Join(root, "auth", "providers.json")}
	data, err := os.ReadFile(store.path)
	switch {
	case err == nil:
		if err := json.Unmarshal(data, store); err != nil {
			return nil, err
		}
		if store.Providers == nil {
			store.Providers = make(map[string]Credentials)
		}
		store.path = filepath.Join(root, "auth", "providers.json")
	case errors.Is(err, fs.ErrNotExist):
	default:
		return nil, err
	}
	return store, nil
}

func (s *Store) Get(provider string) (Credentials, bool) {
	credentials, ok := s.Providers[provider]
	return credentials, ok && credentials.APIKey != ""
}

func (s *Store) Set(provider string, credentials Credentials) error {
	if s.Providers == nil {
		s.Providers = make(map[string]Credentials)
	}
	s.Providers[provider] = credentials
	return s.Save()
}

func (s *Store) Save() error {
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp, err := os.CreateTemp(dir, ".providers-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(name, s.path); err != nil {
		return err
	}
	return os.Chmod(s.path, 0o600)
}

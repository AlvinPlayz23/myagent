package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/AlvinPlayz23/myagent/internal/filelock"
)

func TestUpdatePreservesOtherWriters(t *testing.T) {
	useTempDir(t)
	if err := Save(&Config{DefaultModel: "openai/model-a", WelcomeStyle: "rain"}); err != nil {
		t.Fatal(err)
	}
	// Two long-lived frontends retain startup snapshots, but their save
	// closures must mutate a freshly loaded config rather than those snapshots.
	saveTools := func() error {
		return Update(func(cfg *Config) error {
			cfg.DisabledTools = []string{"bash"}
			return nil
		})
	}
	saveStyle := func() error {
		return Update(func(cfg *Config) error {
			cfg.WelcomeStyle = "default"
			return nil
		})
	}
	if err := saveTools(); err != nil {
		t.Fatal(err)
	}
	if err := saveStyle(); err != nil {
		t.Fatal(err)
	}
	got, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if got.WelcomeStyle != "default" || !reflect.DeepEqual(got.DisabledTools, []string{"bash"}) || got.DefaultModel != "openai/model-a" {
		t.Fatal("update lost an unrelated setting")
	}
}

func TestUpdateCallbackErrorDoesNotWrite(t *testing.T) {
	useTempDir(t)
	if err := Save(&Config{WelcomeStyle: "rain"}); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(configPath(t))
	if err != nil {
		t.Fatal(err)
	}
	wantErr := errors.New("mutation failed")
	err = Update(func(cfg *Config) error {
		cfg.WelcomeStyle = "default"
		return wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("Update error = %v, want callback error", err)
	}
	after, err := os.ReadFile(configPath(t))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("callback error changed the file")
	}
}

func TestUpdateLoadErrorDoesNotRunCallback(t *testing.T) {
	useTempDir(t)
	writeFile(t, configPath(t), "{invalid")
	called := false
	if err := Update(func(*Config) error { called = true; return nil }); err == nil {
		t.Fatal("Update accepted invalid JSON")
	}
	if called {
		t.Fatal("callback ran after a load error")
	}
}

func TestUpdateConcurrentWriters(t *testing.T) {
	useTempDir(t)
	const writers = 24
	start := make(chan struct{})
	errors := make(chan error, writers)
	var wg sync.WaitGroup
	for i := range writers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			errors <- Update(func(cfg *Config) error {
				if cfg.Providers == nil {
					cfg.Providers = make(map[string]ProviderConfig)
				}
				cfg.Providers[fmt.Sprintf("provider-%d", i)] = ProviderConfig{Model: "model"}
				time.Sleep(time.Millisecond)
				return nil
			})
		}()
	}
	close(start)
	wg.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	got, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Providers) != writers {
		t.Fatalf("providers = %d, want %d", len(got.Providers), writers)
	}
}

func TestUpdateReleasesLockOnError(t *testing.T) {
	for _, failure := range []string{"callback", "load", "save"} {
		t.Run(failure, func(t *testing.T) {
			useTempDir(t)
			path := configPath(t)
			if failure == "load" {
				writeFile(t, path, "{invalid")
			}
			err := Update(func(*Config) error {
				switch failure {
				case "callback":
					return errors.New("callback error")
				case "save":
					// A directory at the target prevents the atomic rename.
					return os.Mkdir(path, 0o700)
				}
				return nil
			})
			if err == nil {
				t.Fatal("Update should fail")
			}
			lock, err := filelock.Acquire(path)
			if err != nil {
				t.Fatalf("lock retained after error: %v", err)
			}
			if err := lock.Close(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestUpdateCreatesDirectory(t *testing.T) {
	t.Setenv("MYAGENT_DIR", filepath.Join(t.TempDir(), "new", "config"))
	if err := Update(func(cfg *Config) error {
		cfg.WelcomeStyle = "rain"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	got, err := Load()
	if err != nil || got.WelcomeStyle != "rain" {
		t.Fatalf("first update not persisted: %v", err)
	}
}

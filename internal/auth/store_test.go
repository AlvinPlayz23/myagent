package auth

import (
	"os"
	"runtime"
	"testing"
)

func TestStoreRoundTrip(t *testing.T) {
	dir := t.TempDir()
	store, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Set("nvidia", Credentials{APIKey: "secret", BaseURL: "https://example.test/v1"}); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := loaded.Get("nvidia")
	if !ok || got.APIKey != "secret" || got.BaseURL != "https://example.test/v1" {
		t.Fatalf("credentials = %#v, %v", got, ok)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(loaded.path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("mode = %o", info.Mode().Perm())
		}
	}
}

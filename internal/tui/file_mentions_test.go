package tui

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoverFilesPrioritizesRootFiles(t *testing.T) {
	cwd := t.TempDir()
	for _, path := range []string{"main.go", "README.md", "desktop/app.go", "internal/agent/run.go"} {
		fullPath := filepath.Join(cwd, path)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(fullPath, []byte("test"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	files := discoverFiles(cwd)
	want := []string{"README.md", "main.go", "desktop/app.go", "internal/agent/run.go"}
	if len(files) != len(want) {
		t.Fatalf("files = %v, want %v", files, want)
	}
	for i := range want {
		if files[i] != want[i] {
			t.Fatalf("files = %v, want %v", files, want)
		}
	}
}

func TestDiscoverFilesHonorsGitignore(t *testing.T) {
	cwd := t.TempDir()
	if err := os.WriteFile(filepath.Join(cwd, ".gitignore"), []byte("desktop/\n*.exe\nprivate.txt\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"main.go", "private.txt", "myagent.exe", "desktop/app.go", "internal/app.go"} {
		fullPath := filepath.Join(cwd, path)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(fullPath, []byte("test"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	files := discoverFiles(cwd)
	want := []string{".gitignore", "main.go", "internal/app.go"}
	if len(files) != len(want) {
		t.Fatalf("files = %v, want %v", files, want)
	}
	for i := range want {
		if files[i] != want[i] {
			t.Fatalf("files = %v, want %v", files, want)
		}
	}
}

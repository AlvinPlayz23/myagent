package tui

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/AlvinPlayz23/myagent/internal/types"
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

func TestExpandFileMentionsRejectsSymlinkOutsideWorkingDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("creating symlinks requires elevated privileges or Developer Mode on Windows")
	}

	cwd := t.TempDir()
	outside := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(outside, []byte("do not expose"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(cwd, "linked-secret")); err != nil {
		t.Fatal(err)
	}

	_, err := expandFileMentions("review @linked-secret", cwd)
	if err == nil || !strings.Contains(err.Error(), "outside the working directory") {
		t.Fatalf("expandFileMentions error = %v, want outside-working-directory error", err)
	}
}

func TestExpandFileMentionsAllowsSymlinkWithinWorkingDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("creating symlinks requires elevated privileges or Developer Mode on Windows")
	}

	cwd := t.TempDir()
	if err := os.WriteFile(filepath.Join(cwd, "actual.txt"), []byte("safe content"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("actual.txt", filepath.Join(cwd, "linked.txt")); err != nil {
		t.Fatal(err)
	}

	got, err := expandFileMentions("review @linked.txt", cwd)
	if err != nil {
		t.Fatalf("expandFileMentions: %v", err)
	}
	if !strings.Contains(got, "safe content") {
		t.Fatalf("expanded prompt = %q, want linked file content", got)
	}
}

func TestExpandPromptContentAttachesMentionedImage(t *testing.T) {
	cwd := t.TempDir()
	png := append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 32)...)
	if err := os.WriteFile(filepath.Join(cwd, "screen.png"), png, 0o600); err != nil {
		t.Fatal(err)
	}

	content, err := expandPromptContent("explain @screen.png", cwd)
	if err != nil {
		t.Fatalf("expandPromptContent: %v", err)
	}
	if len(content) != 2 || content[0].Type != types.ContentText || content[0].Text != "explain @screen.png" {
		t.Fatalf("content = %#v", content)
	}
	if content[1].Type != types.ContentImage || content[1].MimeType != "image/png" || content[1].Data == "" {
		t.Fatalf("image block = %#v", content[1])
	}
}

func TestExpandPromptContentRecognizesJFIFAsJPEG(t *testing.T) {
	cwd := t.TempDir()
	jpeg := append([]byte("\xff\xd8\xff\xe0\x00\x10JFIF\x00"), make([]byte, 32)...)
	if err := os.WriteFile(filepath.Join(cwd, "photo.jfif"), jpeg, 0o600); err != nil {
		t.Fatal(err)
	}

	content, err := expandPromptContent("explain @photo.jfif", cwd)
	if err != nil {
		t.Fatalf("expandPromptContent: %v", err)
	}
	if len(content) != 2 || content[1].Type != types.ContentImage || content[1].MimeType != "image/jpeg" {
		t.Fatalf("content = %#v", content)
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

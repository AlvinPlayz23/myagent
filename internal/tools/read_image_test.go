package tools

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/AlvinPlayz23/myagent/internal/types"
)

func TestReadToolReturnsImageAttachment(t *testing.T) {
	cwd := t.TempDir()
	png := append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 32)...)
	if err := os.WriteFile(filepath.Join(cwd, "image.png"), png, 0o600); err != nil {
		t.Fatal(err)
	}

	result, err := (&ReadTool{Cwd: cwd}).Execute(context.Background(), "call-1", map[string]any{"path": "image.png"})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Content) != 1 || result.Content[0].Type != types.ContentImage {
		t.Fatalf("content = %#v, want one image", result.Content)
	}
	if result.Content[0].MimeType != "image/png" || result.Content[0].Data == "" {
		t.Fatalf("image = %#v", result.Content[0])
	}
}

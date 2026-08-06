package images

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/AlvinPlayz23/myagent/internal/types"
)

func TestValidateContentNormalizesValidImage(t *testing.T) {
	png := append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 32)...)
	content, err := ValidateContent([]types.ContentBlock{
		types.TextBlock("look"),
		types.ImageBlock(base64.StdEncoding.EncodeToString(png), "image/png"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(content) != 2 || content[1].MimeType != "image/png" {
		t.Fatalf("content = %#v", content)
	}
}

func TestValidateContentRejectsMIMETypeMismatch(t *testing.T) {
	png := append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 32)...)
	_, err := ValidateContent([]types.ContentBlock{
		types.ImageBlock(base64.StdEncoding.EncodeToString(png), "image/jpeg"),
	})
	if err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("error = %v, want MIME mismatch", err)
	}
}

func TestValidateContentRejectsNonUserBlock(t *testing.T) {
	_, err := ValidateContent([]types.ContentBlock{{Type: types.ContentToolCall}})
	if err == nil || !strings.Contains(err.Error(), "not allowed") {
		t.Fatalf("error = %v, want block rejection", err)
	}
}

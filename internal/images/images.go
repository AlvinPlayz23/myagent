// Package images validates and encodes image content shared by CLI and server inputs.
package images

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/AlvinPlayz23/myagent/internal/types"
)

const (
	MaxImages     = 6
	MaxImageBytes = 8 << 20
	MaxTotalBytes = 20 << 20
)

var supported = map[string]struct{}{
	"image/gif":  {},
	"image/jpeg": {},
	"image/png":  {},
	"image/webp": {},
}

// Load reads and validates an image file and returns a provider-ready block.
func Load(path string) (types.ContentBlock, error) {
	info, err := os.Stat(path)
	if err != nil {
		return types.ContentBlock{}, err
	}
	if !info.Mode().IsRegular() {
		return types.ContentBlock{}, fmt.Errorf("image path is not a regular file")
	}
	if info.Size() > MaxImageBytes {
		return types.ContentBlock{}, fmt.Errorf("image exceeds the %d MiB limit", MaxImageBytes>>20)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return types.ContentBlock{}, err
	}
	return FromBytes(data)
}

// FromBytes validates and encodes raw image data into a content block.
func FromBytes(data []byte) (types.ContentBlock, error) {
	if len(data) > MaxImageBytes {
		return types.ContentBlock{}, fmt.Errorf("image exceeds the %d MiB limit", MaxImageBytes>>20)
	}
	mimeType, ok := Detect(data)
	if !ok {
		return types.ContentBlock{}, fmt.Errorf("unsupported image type")
	}
	return types.ImageBlock(base64.StdEncoding.EncodeToString(data), mimeType), nil
}

// Detect returns a supported image MIME type for data.
func Detect(data []byte) (string, bool) {
	mimeType := http.DetectContentType(data)
	_, ok := supported[mimeType]
	return mimeType, ok
}

// ValidateContent validates client-supplied user content and returns a copy
// with normalized image MIME types. Only text and image blocks are accepted.
func ValidateContent(content []types.ContentBlock) ([]types.ContentBlock, error) {
	if len(content) == 0 {
		return nil, fmt.Errorf("content is required")
	}
	out := make([]types.ContentBlock, 0, len(content))
	imageCount, totalBytes := 0, 0
	hasContent := false
	for _, block := range content {
		switch block.Type {
		case types.ContentText:
			if strings.TrimSpace(block.Text) != "" {
				hasContent = true
			}
			out = append(out, types.TextBlock(block.Text))
		case types.ContentImage:
			imageCount++
			if imageCount > MaxImages {
				return nil, fmt.Errorf("at most %d images are allowed per message", MaxImages)
			}
			decoded, err := base64.StdEncoding.DecodeString(block.Data)
			if err != nil {
				return nil, fmt.Errorf("image %d contains invalid base64 data", imageCount)
			}
			if len(decoded) > MaxImageBytes {
				return nil, fmt.Errorf("image %d exceeds the %d MiB limit", imageCount, MaxImageBytes>>20)
			}
			totalBytes += len(decoded)
			if totalBytes > MaxTotalBytes {
				return nil, fmt.Errorf("images exceed the %d MiB total limit", MaxTotalBytes>>20)
			}
			mimeType, ok := Detect(decoded)
			if !ok {
				return nil, fmt.Errorf("image %d has an unsupported image type", imageCount)
			}
			declared := strings.ToLower(strings.TrimSpace(block.MimeType))
			if declared == "image/jpg" {
				declared = "image/jpeg"
			}
			if declared != "" && declared != mimeType {
				return nil, fmt.Errorf("image %d MIME type %q does not match %q", imageCount, block.MimeType, mimeType)
			}
			out = append(out, types.ImageBlock(block.Data, mimeType))
			hasContent = true
		default:
			return nil, fmt.Errorf("content block type %q is not allowed", block.Type)
		}
	}
	if !hasContent {
		return nil, fmt.Errorf("message must contain text or an image")
	}
	return out, nil
}

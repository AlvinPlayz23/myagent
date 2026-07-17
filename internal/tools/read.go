package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/myagent/myagent/internal/types"
)

// ReadTool reads file contents. Ported from pi read.ts. Description and schema
// are kept verbatim (models are trained on them).
type ReadTool struct {
	Cwd string
}

func (t *ReadTool) Name() string { return "read" }

func (t *ReadTool) Description() string {
	// Verbatim from pi read.ts createReadToolDefinition.
	return fmt.Sprintf(
		"Read the contents of a file. Supports text files and images (jpg, png, gif, webp, bmp). "+
			"Images are sent as attachments. For text files, output is truncated to %d lines or %dKB "+
			"(whichever is hit first). Use offset/limit for large files. When you need the full file, "+
			"continue with offset until complete.",
		DefaultMaxLines, DefaultMaxBytes/1024,
	)
}

func (t *ReadTool) Parameters() map[string]any {
	// Ported verbatim from pi read.ts readSchema.
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path":   map[string]any{"type": "string", "description": "Path to the file to read (relative or absolute)"},
			"offset": map[string]any{"type": "number", "description": "Line number to start reading from (1-indexed)"},
			"limit":  map[string]any{"type": "number", "description": "Maximum number of lines to read"},
		},
		"required": []string{"path"},
	}
}

func (t *ReadTool) Execute(ctx context.Context, _ string, args map[string]any) (*types.ToolResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("Operation aborted")
	}
	path, ok := argString(args, "path")
	if !ok || path == "" {
		return nil, fmt.Errorf("read: missing required 'path' argument")
	}
	offset, hasOffset := argInt(args, "offset")
	limit, hasLimit := argInt(args, "limit")

	abs := resolveToCwd(path, t.Cwd)
	data, err := os.ReadFile(abs)
	if err != nil {
		return nil, err
	}
	text := string(data)
	allLines := strings.Split(text, "\n")

	startLine := 0
	if hasOffset && offset > 0 {
		startLine = offset - 1
	}
	if startLine >= len(allLines) {
		return nil, fmt.Errorf("Offset %d is beyond end of file (%d lines total)", offset, len(allLines))
	}
	startDisplay := startLine + 1

	var selected string
	userLimited := -1
	if hasLimit {
		end := startLine + limit
		if end > len(allLines) {
			end = len(allLines)
		}
		selected = strings.Join(allLines[startLine:end], "\n")
		userLimited = end - startLine
	} else {
		selected = strings.Join(allLines[startLine:], "\n")
	}

	tr := TruncateHead(selected, 0, 0)
	var outText string
	switch {
	case tr.FirstLineExceedsLimit:
		firstLineSize := FormatSize(len(allLines[startLine]))
		outText = fmt.Sprintf("[Line %d is %s, exceeds %s limit. Use bash: sed -n '%dp' %s | head -c %d]",
			startDisplay, firstLineSize, FormatSize(DefaultMaxBytes), startDisplay, path, DefaultMaxBytes)
	case tr.Truncated:
		endDisplay := startDisplay + tr.OutputLines - 1
		nextOffset := endDisplay + 1
		outText = tr.Content
		if tr.TruncatedBy == "lines" {
			outText += fmt.Sprintf("\n\n[Showing lines %d-%d of %d. Use offset=%d to continue.]",
				startDisplay, endDisplay, len(allLines), nextOffset)
		} else {
			outText += fmt.Sprintf("\n\n[Showing lines %d-%d of %d (%s limit). Use offset=%d to continue.]",
				startDisplay, endDisplay, len(allLines), FormatSize(DefaultMaxBytes), nextOffset)
		}
	case userLimited >= 0 && startLine+userLimited < len(allLines):
		remaining := len(allLines) - (startLine + userLimited)
		nextOffset := startLine + userLimited + 1
		outText = fmt.Sprintf("%s\n\n[%d more lines in file. Use offset=%d to continue.]",
			tr.Content, remaining, nextOffset)
	default:
		outText = tr.Content
	}

	return types.TextResult(outText, nil), nil
}

// resolveToCwd resolves path against cwd if it is relative. Ported from pi
// path-utils resolveToCwd.
func resolveToCwd(path, cwd string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(cwd, path)
}

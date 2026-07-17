package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/myagent/myagent/internal/types"
)

// WriteTool creates or overwrites a file, creating parent directories.
// Ported from pi write.ts.
type WriteTool struct {
	Cwd string
}

func (t *WriteTool) Name() string { return "write" }

func (t *WriteTool) Description() string {
	// Verbatim from pi write.ts.
	return "Write content to a file. Creates the file if it doesn't exist, overwrites if it does. " +
		"Automatically creates parent directories."
}

func (t *WriteTool) Parameters() map[string]any {
	// Ported verbatim from pi write.ts writeSchema.
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path":    map[string]any{"type": "string", "description": "Path to the file to write (relative or absolute)"},
			"content": map[string]any{"type": "string", "description": "Content to write to the file"},
		},
		"required": []string{"path", "content"},
	}
}

func (t *WriteTool) Execute(ctx context.Context, _ string, args map[string]any) (*types.ToolResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("Operation aborted")
	}
	path, ok := argString(args, "path")
	if !ok || path == "" {
		return nil, fmt.Errorf("write: missing required 'path' argument")
	}
	content, ok := argString(args, "content")
	if !ok {
		return nil, fmt.Errorf("write: missing required 'content' argument")
	}

	abs := resolveToCwd(path, t.Cwd)
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("Operation aborted")
	}
	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		return nil, err
	}

	return types.TextResult(fmt.Sprintf("Successfully wrote %d bytes to %s", len(content), path), nil), nil
}

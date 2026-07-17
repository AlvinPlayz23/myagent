package tools

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/myagent/myagent/internal/types"
)

// EditTool performs exact-match text replacements in a single file.
// Ported from pi edit.ts. Each edit's oldText must match a unique region.
type EditTool struct {
	Cwd string
}

func (t *EditTool) Name() string { return "edit" }

func (t *EditTool) Description() string {
	// Verbatim from pi edit.ts createEditToolDefinition.
	return "Edit a single file using exact text replacement. Every edits[].oldText must match a unique, " +
		"non-overlapping region of the original file. If two changes affect the same block or nearby lines, " +
		"merge them into one edit instead of emitting overlapping edits. Do not include large unchanged regions " +
		"just to connect distant changes."
}

func (t *EditTool) Parameters() map[string]any {
	// Ported verbatim from pi edit.ts editSchema.
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{"type": "string", "description": "Path to the file to edit (relative or absolute)"},
			"edits": map[string]any{
				"type": "array",
				"description": "One or more targeted replacements. Each edit is matched against the original file, " +
					"not incrementally. Do not include overlapping or nested edits. If two changes touch the same " +
					"block or nearby lines, merge them into one edit instead.",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"oldText": map[string]any{"type": "string", "description": "Exact text for one targeted replacement. It must be unique in the original file and must not overlap with any other edits[].oldText in the same call."},
						"newText": map[string]any{"type": "string", "description": "Replacement text for this targeted edit."},
					},
					"required": []string{"oldText", "newText"},
				},
			},
		},
		"required": []string{"path", "edits"},
	}
}

type editEntry struct {
	oldText string
	newText string
}

func (t *EditTool) Execute(ctx context.Context, _ string, args map[string]any) (*types.ToolResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("Operation aborted")
	}
	path, ok := argString(args, "path")
	if !ok || path == "" {
		return nil, fmt.Errorf("edit: missing required 'path' argument")
	}
	edits, err := parseEdits(args["edits"])
	if err != nil {
		return nil, err
	}
	if len(edits) == 0 {
		return nil, fmt.Errorf("Edit tool input is invalid. edits must contain at least one replacement.")
	}

	abs := resolveToCwd(path, t.Cwd)
	data, err := os.ReadFile(abs)
	if err != nil {
		return nil, fmt.Errorf("Could not edit file: %s. %v.", path, err)
	}
	content := string(data)

	// Apply each edit against the ORIGINAL content, requiring a unique match.
	// Ported from pi applyEditsToNormalizedContent semantics (exact match,
	// non-unique fails).
	newContent := content
	for i, e := range edits {
		count := strings.Count(content, e.oldText)
		if count == 0 {
			return nil, fmt.Errorf("edits[%d].oldText not found in %s", i, path)
		}
		if count > 1 {
			return nil, fmt.Errorf("edits[%d].oldText is not unique in %s (%d matches). Provide a larger, unique oldText.", i, path, count)
		}
		newContent = strings.Replace(newContent, e.oldText, e.newText, 1)
	}

	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("Operation aborted")
	}
	if err := os.WriteFile(abs, []byte(newContent), 0o644); err != nil {
		return nil, err
	}

	return types.TextResult(fmt.Sprintf("Successfully replaced %d block(s) in %s.", len(edits), path), nil), nil
}

func parseEdits(v any) ([]editEntry, error) {
	arr, ok := v.([]any)
	if !ok {
		return nil, fmt.Errorf("edit: 'edits' must be an array")
	}
	var out []editEntry
	for _, item := range arr {
		m, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("edit: each edit must be an object with oldText/newText")
		}
		oldText, ok1 := m["oldText"].(string)
		newText, ok2 := m["newText"].(string)
		if !ok1 || !ok2 {
			return nil, fmt.Errorf("edit: each edit requires string oldText and newText")
		}
		out = append(out, editEntry{oldText: oldText, newText: newText})
	}
	return out, nil
}

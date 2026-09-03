package tui

import "strings"

// proposalDiff builds the before/after preview for edit/write tool calls.
func proposalDiff(name string, args map[string]any) []diffLine {
	path := toolArg(args, "path")
	if path == "" {
		path = toolArg(args, "file_path")
	}
	if path == "" {
		return nil
	}
	newPath := "b/" + strings.TrimPrefix(strings.ReplaceAll(path, "\\", "/"), "./")

	switch name {
	case "write":
		content, ok := args["content"].(string)
		if !ok {
			return nil
		}
		lines := []diffLine{{text: "--- /dev/null"}, {text: "+++ " + newPath}, {text: "@@"}}
		return append(lines, prefixedDiffLines('+', content)...)
	case "edit":
		rawEdits, ok := args["edits"].([]any)
		if !ok {
			return nil
		}
		lines := []diffLine{{text: "--- a/" + strings.TrimPrefix(strings.ReplaceAll(path, "\\", "/"), "./")}, {text: "+++ " + newPath}}
		for _, raw := range rawEdits {
			edit, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			oldText, oldOK := edit["oldText"].(string)
			newText, newOK := edit["newText"].(string)
			if !oldOK || !newOK {
				continue
			}
			lines = append(lines, prefixedDiffLines('-', oldText)...)
			lines = append(lines, prefixedDiffLines('+', newText)...)
		}
		if len(lines) == 2 {
			return nil
		}
		return lines
	default:
		return nil
	}
}

func toolArg(args map[string]any, key string) string {
	if args == nil {
		return ""
	}
	s, _ := args[key].(string)
	return s
}

func prefixedDiffLines(prefix byte, text string) []diffLine {
	// strings.Split intentionally retains a final empty line: an added or
	// removed trailing newline is meaningful in this compact preview.
	parts := strings.Split(text, "\n")
	lines := make([]diffLine, len(parts))
	for i, part := range parts {
		lines[i] = diffLine{prefix: prefix, text: part}
	}
	return lines
}

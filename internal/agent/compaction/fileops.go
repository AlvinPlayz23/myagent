package compaction

import (
	"sort"
	"strings"

	"github.com/myagent/myagent/internal/types"
)

// FileOperations tracks file paths touched in the history being summarized.
// Ported from pi FileOperations.
type FileOperations struct {
	Read    map[string]struct{}
	Written map[string]struct{}
	Edited  map[string]struct{}
}

// NewFileOps returns an empty file-operation accumulator.
func NewFileOps() FileOperations {
	return FileOperations{
		Read:    map[string]struct{}{},
		Written: map[string]struct{}{},
		Edited:  map[string]struct{}{},
	}
}

// ExtractFileOpsFromMessage adds file operations from an assistant message's
// tool calls to the accumulator. Ported from pi extractFileOpsFromMessage.
// Non-assistant messages and tool calls without a path argument are ignored.
func ExtractFileOpsFromMessage(m types.Message, ops FileOperations) {
	if m.Role != types.RoleAssistant {
		return
	}
	for _, b := range m.Content {
		if b.Type != types.ContentToolCall {
			continue
		}
		path, _ := b.Arguments["path"].(string)
		if path == "" {
			continue
		}
		switch b.Name {
		case "read":
			ops.Read[path] = struct{}{}
		case "write":
			ops.Written[path] = struct{}{}
		case "edit":
			ops.Edited[path] = struct{}{}
		}
	}
}

// MergeFileOps merges src into dst in place. Used to carry file operations
// forward from a previous compaction's details.
func MergeFileOps(dst, src FileOperations) {
	for k := range src.Read {
		dst.Read[k] = struct{}{}
	}
	for k := range src.Written {
		dst.Written[k] = struct{}{}
	}
	for k := range src.Edited {
		dst.Edited[k] = struct{}{}
	}
}

// FileLists is the sorted read-only and modified file lists derived from a
// FileOperations set. Ported from pi computeFileLists.
type FileLists struct {
	ReadFiles     []string
	ModifiedFiles []string
}

// ComputeFileLists derives sorted file lists from accumulated operations.
// A file is "modified" if it was written or edited; "read" if read-only.
func ComputeFileLists(ops FileOperations) FileLists {
	modified := map[string]struct{}{}
	for k := range ops.Edited {
		modified[k] = struct{}{}
	}
	for k := range ops.Written {
		modified[k] = struct{}{}
	}
	var readOnly []string
	for k := range ops.Read {
		if _, isMod := modified[k]; !isMod {
			readOnly = append(readOnly, k)
		}
	}
	var modifiedFiles []string
	for k := range modified {
		modifiedFiles = append(modifiedFiles, k)
	}
	sort.Strings(readOnly)
	sort.Strings(modifiedFiles)
	return FileLists{ReadFiles: readOnly, ModifiedFiles: modifiedFiles}
}

// FormatFileOperations renders file lists as summary metadata tags. Ported
// from pi formatFileOperations. Returns "" when there are no files, so the
// caller can unconditionally append the result.
func FormatFileOperations(files FileLists) string {
	var sections []string
	if len(files.ReadFiles) > 0 {
		sections = append(sections, "<read-files>\n"+strings.Join(files.ReadFiles, "\n")+"\n</read-files>")
	}
	if len(files.ModifiedFiles) > 0 {
		sections = append(sections, "<modified-files>\n"+strings.Join(files.ModifiedFiles, "\n")+"\n</modified-files>")
	}
	if len(sections) == 0 {
		return ""
	}
	return "\n\n" + strings.Join(sections, "\n\n")
}

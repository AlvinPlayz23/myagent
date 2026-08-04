package tui

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	filePickerMaxVisible = 5
	maxMentionFiles      = 10
	maxMentionFileBytes  = 1 << 20
	maxFileCandidates    = 2_000
)

type filePicker struct {
	items         []string
	matched       []int
	sel           int
	start, end    int // byte offsets of the @query in the textarea value
	active        bool
	dismissedText string
	loaded        bool
}

// sync opens the picker for the unfinished @path immediately before the input
// cursor. File discovery is deliberately confined to the TUI working directory.
func (p *filePicker) sync(text, cwd string) {
	start, end, query, ok := activeMention(text)
	if !ok || text == p.dismissedText {
		if !ok {
			p.dismissedText = ""
		}
		p.close()
		return
	}
	if !p.loaded {
		p.items = discoverFiles(cwd)
		p.loaded = true
	}
	p.dismissedText = ""
	p.start, p.end = start, end
	p.matched = p.matched[:0]
	query = strings.ToLower(filepath.ToSlash(query))
	for i, path := range p.items {
		if strings.Contains(strings.ToLower(path), query) {
			p.matched = append(p.matched, i)
		}
	}
	p.active = len(p.matched) > 0
	if p.sel >= len(p.matched) {
		p.sel = 0
	}
}

func (p *filePicker) close() {
	p.active = false
	p.matched = p.matched[:0]
	p.sel = 0
}

func (p *filePicker) dismiss(text string) {
	p.dismissedText = text
	p.close()
}

func (p *filePicker) move(delta int) {
	if p.active && len(p.matched) > 0 {
		p.sel = (p.sel + delta + len(p.matched)) % len(p.matched)
	}
}

func (p *filePicker) selected() (string, bool) {
	if !p.active || p.sel < 0 || p.sel >= len(p.matched) {
		return "", false
	}
	return p.items[p.matched[p.sel]], true
}

func (p *filePicker) height() int {
	if !p.active {
		return 0
	}
	return min(filePickerMaxVisible, len(p.matched))
}

func (p *filePicker) visibleRange(count int) (int, int) {
	count = min(count, p.height())
	start := max(0, p.sel-count+1)
	if maxStart := len(p.matched) - count; start > maxStart {
		start = maxStart
	}
	return start, start + count
}

func discoverFiles(cwd string) []string {
	ignored := loadGitignore(cwd)
	var files []string
	add := func(path string, d fs.DirEntry) {
		if len(files) >= maxFileCandidates || !d.Type().IsRegular() {
			return
		}
		info, err := d.Info()
		if err != nil || info.Size() > maxMentionFileBytes {
			return
		}
		rel, err := filepath.Rel(cwd, path)
		if err == nil && !ignored(filepath.ToSlash(rel), false) {
			files = append(files, filepath.ToSlash(rel))
		}
	}

	// Read root files first. WalkDir otherwise descends into alphabetically
	// earlier directories and may reach the candidate limit before root files.
	if entries, err := os.ReadDir(cwd); err == nil {
		for _, entry := range entries {
			if !entry.IsDir() {
				add(filepath.Join(cwd, entry.Name()), entry)
			}
		}
	}
	_ = filepath.WalkDir(cwd, func(path string, d fs.DirEntry, err error) error {
		if err != nil || len(files) >= maxFileCandidates {
			if d != nil && d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if path == cwd {
			return nil
		}
		rel, err := filepath.Rel(cwd, path)
		if err != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", "vendor", ".idea", ".vscode":
				return filepath.SkipDir
			}
			if ignored(rel, true) {
				return filepath.SkipDir
			}
			return nil
		}
		// Root files were already collected above.
		if !strings.Contains(rel, "/") {
			return nil
		}
		add(path, d)
		return nil
	})
	// Keep top-level files visible before nested matches. A plain lexical sort
	// otherwise places directory contents ahead of root Go source files.
	sort.Slice(files, func(i, j int) bool {
		iDepth := strings.Count(files[i], "/")
		jDepth := strings.Count(files[j], "/")
		if iDepth != jDepth {
			return iDepth < jDepth
		}
		return files[i] < files[j]
	})
	return files
}

// loadGitignore implements the common project-local .gitignore patterns used
// for picker discovery. Ignored directories are skipped before they are walked.
func loadGitignore(cwd string) func(path string, isDir bool) bool {
	contents, err := os.ReadFile(filepath.Join(cwd, ".gitignore"))
	if err != nil {
		return func(string, bool) bool { return false }
	}
	var patterns []string
	for _, line := range strings.Split(string(contents), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "!") {
			continue
		}
		patterns = append(patterns, filepath.ToSlash(strings.TrimPrefix(line, "/")))
	}
	return func(path string, isDir bool) bool {
		for _, pattern := range patterns {
			directory := strings.HasSuffix(pattern, "/")
			pattern = strings.TrimSuffix(pattern, "/")
			if directory && !isDir {
				continue
			}
			if ok, _ := filepath.Match(pattern, path); ok {
				return true
			}
			if !strings.Contains(pattern, "/") {
				if ok, _ := filepath.Match(pattern, filepath.Base(path)); ok {
					return true
				}
			}
		}
		return false
	}
}

// activeMention identifies an unquoted @path fragment at the end of text.
// A mention begins at whitespace or the beginning of a prompt, which avoids
// interpreting email addresses as file mentions.
func activeMention(text string) (start, end int, query string, ok bool) {
	at := strings.LastIndex(text, "@")
	if at < 0 || (at > 0 && !isMentionBoundary(text[at-1])) {
		return 0, 0, "", false
	}
	fragment := text[at+1:]
	if strings.ContainsAny(fragment, "\r\n\t ") {
		return 0, 0, "", false
	}
	return at, len(text), fragment, true
}

func isMentionBoundary(b byte) bool {
	return b == ' ' || b == '\t' || b == '\r' || b == '\n'
}

// expandFileMentions appends the contents of every @path token to the message
// delivered to the CLI agent. The visible prompt is intentionally unchanged.
func expandFileMentions(text, cwd string) (string, error) {
	mentions := mentionedPaths(text)
	if len(mentions) == 0 {
		return text, nil
	}
	if len(mentions) > maxMentionFiles {
		return "", fmt.Errorf("at most %d file mentions are allowed per message", maxMentionFiles)
	}

	// Abs alone is insufficient here: an in-project symlink can point outside
	// cwd. Canonicalize cwd and every mentioned file before checking containment
	// so @mentions cannot exfiltrate arbitrary readable files into the prompt.
	cleanCwd, err := filepath.Abs(cwd)
	if err != nil {
		return "", err
	}
	cleanCwd, err = filepath.EvalSymlinks(cleanCwd)
	if err != nil {
		return "", fmt.Errorf("resolve working directory: %w", err)
	}

	var context strings.Builder
	for _, mention := range mentions {
		path := filepath.Join(cwd, filepath.FromSlash(mention))
		cleanPath, err := filepath.Abs(path)
		if err != nil {
			return "", fmt.Errorf("file mention %q is outside the working directory", mention)
		}
		cleanPath, err = filepath.EvalSymlinks(cleanPath)
		if err != nil || !pathWithin(cleanCwd, cleanPath) {
			return "", fmt.Errorf("file mention %q is outside the working directory", mention)
		}
		info, err := os.Stat(cleanPath)
		if err != nil {
			return "", fmt.Errorf("read mentioned file %q: %w", mention, err)
		}
		if !info.Mode().IsRegular() {
			return "", fmt.Errorf("mentioned path %q is not a regular file", mention)
		}
		if info.Size() > maxMentionFileBytes {
			return "", fmt.Errorf("mentioned file %q exceeds the %d KB limit", mention, maxMentionFileBytes/1024)
		}
		contents, err := os.ReadFile(cleanPath)
		if err != nil {
			return "", fmt.Errorf("read mentioned file %q: %w", mention, err)
		}
		fmt.Fprintf(&context, "\n\n<file path=%q>\n%s\n</file>", mention, contents)
	}
	return text + context.String(), nil
}

// pathWithin reports whether path is cwd itself or a descendant of cwd. Both
// paths must already be absolute and have had symlinks resolved.
func pathWithin(cwd, path string) bool {
	rel, err := filepath.Rel(cwd, path)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

func mentionedPaths(text string) []string {
	var paths []string
	for i := 0; i < len(text); {
		at := strings.IndexByte(text[i:], '@')
		if at < 0 {
			break
		}
		at += i
		if at > 0 && !isMentionBoundary(text[at-1]) {
			i = at + 1
			continue
		}
		j := at + 1
		for j < len(text) && !isMentionBoundary(text[j]) {
			j++
		}
		if j > at+1 {
			paths = append(paths, text[at+1:j])
		}
		i = j
	}
	return paths
}

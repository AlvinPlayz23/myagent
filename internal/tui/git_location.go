package tui

import (
	"os"
	"path/filepath"
	"strings"
)

type gitLocation struct {
	branch   string
	worktree bool
}

// inspectGitLocation reads the small amount of repository metadata needed by
// the status line without spawning git during rendering.
func inspectGitLocation(path string) gitLocation {
	dir, err := filepath.Abs(path)
	if err != nil {
		dir = path
	}
	if info, statErr := os.Stat(dir); statErr == nil && !info.IsDir() {
		dir = filepath.Dir(dir)
	}

	for {
		entry := filepath.Join(dir, ".git")
		info, statErr := os.Stat(entry)
		if statErr == nil {
			gitDir := entry
			worktree := false
			if !info.IsDir() {
				data, readErr := os.ReadFile(entry)
				if readErr != nil {
					return gitLocation{}
				}
				line := strings.TrimSpace(string(data))
				const prefix = "gitdir:"
				if !strings.HasPrefix(strings.ToLower(line), prefix) {
					return gitLocation{}
				}
				gitDir = strings.TrimSpace(line[len(prefix):])
				if !filepath.IsAbs(gitDir) {
					gitDir = filepath.Join(dir, gitDir)
				}
				worktree = true
			}

			head, readErr := os.ReadFile(filepath.Join(gitDir, "HEAD"))
			if readErr != nil {
				return gitLocation{worktree: worktree}
			}
			ref := strings.TrimSpace(string(head))
			if strings.HasPrefix(ref, "ref: refs/heads/") {
				return gitLocation{
					branch:   strings.TrimPrefix(ref, "ref: refs/heads/"),
					worktree: worktree,
				}
			}
			if len(ref) > 8 {
				ref = ref[:8]
			}
			return gitLocation{branch: ref, worktree: worktree}
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return gitLocation{}
		}
		dir = parent
	}
}

func formatLocationLine(cwd string) string {
	location := inspectGitLocation(cwd)
	parts := make([]string, 0, 3)
	if location.branch != "" {
		parts = append(parts, " "+location.branch)
	}
	if location.worktree {
		parts = append(parts, "worktree")
	}
	if display := collapseHome(cwd); display != "" {
		parts = append(parts, display)
	}
	return strings.Join(parts, " ")
}

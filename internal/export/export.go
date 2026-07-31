// Package export renders persisted sessions as portable documents.
package export

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/AlvinPlayz23/myagent/internal/session"
	"github.com/AlvinPlayz23/myagent/internal/types"
)

type Format string

const (
	Markdown Format = "markdown"
	HTML     Format = "html"
)

var ErrFileExists = errors.New("export file already exists")

type Document struct {
	Title, SessionID, Cwd string
	Messages              []types.Message
}

func Extension(format Format) string {
	if format == HTML {
		return ".html"
	}
	return ".md"
}

func Label(format Format) string {
	if format == HTML {
		return "HTML (.html)"
	}
	return "Markdown (.md)"
}

// Filename validates a user supplied base name and appends the format extension.
func Filename(name string, format Format) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("file name cannot be empty")
	}
	if filepath.Base(name) != name || strings.ContainsAny(name, `/\\`) || name == "." || name == ".." {
		return "", fmt.Errorf("file name must not contain a path")
	}
	for _, r := range name {
		if r < 32 || strings.ContainsRune(`<>:"|?*`, r) {
			return "", fmt.Errorf("file name contains invalid characters")
		}
	}
	ext := Extension(format)
	if !strings.EqualFold(filepath.Ext(name), ext) {
		name += ext
	}
	return name, nil
}

func DefaultFilename(title string) string {
	title = strings.TrimSpace(title)
	if title == "" || title == "new" {
		return "session"
	}
	var b strings.Builder
	lastDash := false
	for _, r := range strings.ToLower(title) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			lastDash = false
		} else if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	out := strings.Trim(b.String(), "-.")
	if out == "" {
		return "session"
	}
	if len([]rune(out)) > 80 {
		out = string([]rune(out)[:80])
	}
	return out
}

func Write(sess *session.Session, format Format, name string, overwrite bool) (string, error) {
	if sess == nil {
		return "", fmt.Errorf("no active session")
	}
	filename, err := Filename(name, format)
	if err != nil {
		return "", err
	}
	path := filepath.Join(sess.Cwd(), filename)
	doc := Document{Title: sess.Title(), SessionID: sess.ID(), Cwd: sess.Cwd(), Messages: sess.Messages()}
	var data []byte
	if format == HTML {
		data, err = RenderHTML(doc)
	} else {
		data, err = RenderMarkdown(doc)
	}
	if err != nil {
		return "", err
	}
	if !overwrite {
		if _, err := os.Stat(path); err == nil {
			return "", ErrFileExists
		} else if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", err
	}
	return path, nil
}

package tui

import (
	"os/exec"
	"runtime"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/clipperhouse/displaywidth"

	"github.com/AlvinPlayz23/myagent/internal/tui/urldetect"
)

// urlAtDisplayColumn returns the URL covering the given display column on a
// (possibly ANSI-styled) line, or "" if none.
func urlAtDisplayColumn(line string, col int) string {
	if col < 0 {
		return ""
	}

	plain := ansi.Strip(line)
	for _, loc := range urldetect.Pattern().FindAllStringIndex(plain, -1) {
		raw := plain[loc[0]:loc[1]]
		url, lead := urldetect.Trim(raw)
		if url == "" {
			continue
		}
		startCol := displaywidth.String(plain[:loc[0]+lead])
		endCol := startCol + displaywidth.String(url)
		if col >= startCol && col < endCol {
			return url
		}
	}
	return ""
}

// openURLCmd opens a URL in the default browser; a var so tests can stub it.
var openURLCmd = func(url string) *exec.Cmd {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", url)
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		return exec.Command("xdg-open", url)
	}
}

func openURL(url string) error {
	cmd := openURLCmd(url)
	if cmd == nil {
		return nil
	}
	return cmd.Start()
}

// blockLineAt maps a transcript content row to its owning block and rendered
// line text, mirroring transcript.render's layout (one blank row between
// blocks, hidden thinking blocks omitted). It reports false for rows outside
// every block.
func (t *transcript) blockLineAt(row, width int) (*block, string, bool) {
	line := 0
	first := true
	for _, b := range t.blocks {
		if b.kind == blockThinking && !t.showThinking {
			continue
		}
		if !first {
			line++ // blank separator row between blocks
		}
		first = false
		rendered := t.renderBlock(b, width)
		lines := strings.Split(rendered, "\n")
		if row >= line && row < line+len(lines) {
			return b, lines[row-line], true
		}
		line += len(lines)
	}
	return nil, "", false
}

// ctrlClickURL handles a Ctrl+click on the transcript: if the clicked cell
// lies on a bare URL inside an assistant block, the URL is opened in the
// default browser. Assistant-only by design — user, tool, and notice blocks
// never open. It reports whether the click was consumed.
func (m *model) ctrlClickURL(point textPoint) bool {
	b, line, ok := m.transcript.blockLineAt(point.row, m.width)
	if !ok || b.kind != blockAssistant {
		return false
	}
	url := urlAtDisplayColumn(line, point.col)
	if url == "" {
		return false
	}
	if err := openURL(url); err != nil {
		m.statusMsg = "Could not open URL: " + err.Error()
	} else {
		m.statusMsg = "Opened " + url
	}
	return true
}

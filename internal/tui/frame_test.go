package tui

import (
	"strings"
	"testing"

	"github.com/AlvinPlayz23/myagent/internal/agent"
	"github.com/AlvinPlayz23/myagent/internal/tui/engine"
)

// cfgForTest returns an agent config backed by the stub provider.
func cfgForTest() agent.Config {
	return agent.Config{Provider: stubProvider{}}
}

// frameText renders the app's current frame into a screen and returns the
// plain text rows, for assertions without a terminal.
func frameText(a *app) string {
	a.render()
	var b strings.Builder
	for y := 0; y < a.screen.H; y++ {
		for x := 0; x < a.screen.W; x++ {
			c := a.screen.CellAt(x, y)
			if c.Ch == 0 {
				b.WriteByte(' ')
			} else {
				b.WriteRune(c.Ch)
			}
		}
		b.WriteByte('\n')
	}
	return b.String()
}

func TestWelcomeFrameRendersLogoAndMenu(t *testing.T) {
	a := newTestApp(t, cfgForTest(), nil)
	a.w, a.h = 100, 30
	a.screen = engine.NewScreen(100, 30)
	a.term = nil

	frame := frameText(a)
	if !strings.Contains(frame, "myagent") {
		t.Fatalf("welcome frame missing logo title:\n%s", frame)
	}
	if !strings.Contains(frame, "Type a message") {
		t.Fatalf("welcome frame missing menu:\n%s", frame)
	}
	if !strings.Contains(frame, "Resume a session") {
		t.Fatalf("welcome frame missing resume row:\n%s", frame)
	}
}

func TestAgentFrameRendersComposerAndFooter(t *testing.T) {
	a := newTestApp(t, cfgForTest(), nil)
	a.w, a.h = 100, 30
	a.screen = engine.NewScreen(100, 30)
	a.term = nil
	a.startAgent()
	a.sb.addUser("hello world")
	a.prompt.setValue("fix the bug")

	frame := frameText(a)
	if !strings.Contains(frame, "hello world") {
		t.Fatalf("frame missing user entry:\n%s", frame)
	}
	if !strings.Contains(frame, "fix the bug") {
		t.Fatalf("frame missing composer text:\n%s", frame)
	}
	if !strings.Contains(frame, "model") {
		t.Fatalf("frame missing footer model:\n%s", frame)
	}
	if !strings.Contains(frame, "╭") || !strings.Contains(frame, "╰") {
		t.Fatalf("frame missing composer border:\n%s", frame)
	}
}

func TestAssistantMarkdownRenders(t *testing.T) {
	a := newTestApp(t, cfgForTest(), nil)
	a.w, a.h = 100, 30
	a.screen = engine.NewScreen(100, 30)
	a.term = nil
	a.startAgent()
	a.sb.beginAssistant()
	a.sb.appendAssistantDelta("# Heading\n\nA paragraph with `code` and **bold**.")
	a.sb.endAssistant()

	frame := frameText(a)
	if !strings.Contains(frame, "Heading") {
		t.Fatalf("frame missing heading:\n%s", frame)
	}
	if !strings.Contains(frame, "code") || !strings.Contains(frame, "bold") {
		t.Fatalf("frame missing inline styles:\n%s", frame)
	}
}

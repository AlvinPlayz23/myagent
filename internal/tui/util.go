package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/AlvinPlayz23/myagent/internal/tui/engine"
	"github.com/AlvinPlayz23/myagent/internal/types"
)

// spinnerFrames is the working-state spinner.
var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// queuedMessage pairs a queued prompt with its display text.
type queuedMessage struct {
	display string
	message types.Message
}

// submissionMode distinguishes queued follow-ups from steering input.
type submissionMode int

const (
	submitFollowUp submissionMode = iota
	submitSteering
)

const promptHistoryLimit = 100

// promptStyle selects the composer chrome drawn around the editor.
type promptStyle string

const (
	promptDefault promptStyle = "default"
	promptRuled   promptStyle = "ruled"
)

type promptChoice struct {
	style       promptStyle
	label       string
	description string
}

var promptChoices = []promptChoice{
	{style: promptDefault, label: "Default", description: "(default) bordered box like the pager"},
	{style: promptRuled, label: "Ruled", description: "one line framed by rules above and below"},
}

func normalizePromptStyle(style string) promptStyle {
	for _, choice := range promptChoices {
		if choice.style == promptStyle(style) {
			return choice.style
		}
	}
	return promptDefault
}

// customizeSection identifies which setting a /customize row belongs to.
type customizeSection int

const (
	sectionStartup customizeSection = iota
	sectionComposer
)

// customizeRow is one line of the /customize panel. Header rows title a group
// and carry no value, so navigation skips over them.
type customizeRow struct {
	section     customizeSection
	header      bool
	label       string
	description string
	welcome     welcomeStyle
	prompt      promptStyle
}

// customizeRows flattens the grouped settings into display order.
var customizeRows = buildCustomizeRows()

func buildCustomizeRows() []customizeRow {
	rows := []customizeRow{{section: sectionStartup, header: true, label: "1. Startup Style", description: "empty-session welcome"}}
	for _, choice := range welcomeChoices {
		rows = append(rows, customizeRow{
			section:     sectionStartup,
			label:       choice.label,
			description: choice.description,
			welcome:     choice.style,
		})
	}
	rows = append(rows, customizeRow{section: sectionComposer, header: true, label: "2. Composer (Prompt Box)", description: "where you type"})
	for _, choice := range promptChoices {
		rows = append(rows, customizeRow{
			section:     sectionComposer,
			label:       choice.label,
			description: choice.description,
			prompt:      choice.style,
		})
	}
	return rows
}

// compact formats a token count like the old footer: 1234 -> "1.2k".
func compact(n int) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	case n >= 1_000:
		return fmt.Sprintf("%.1fk", float64(n)/1_000)
	default:
		return fmt.Sprintf("%d", n)
	}
}

// collapseHome replaces the user's home directory prefix with "~".
func collapseHome(p string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return p
	}
	if p == home {
		return "~"
	}
	if strings.HasPrefix(p, home+string(filepath.Separator)) {
		return "~" + p[len(home):]
	}
	return p
}

// sameUserMessage compares two user messages by content.
func sameUserMessage(a, b types.Message) bool {
	if a.Role != b.Role || len(a.Content) != len(b.Content) {
		return false
	}
	for i := range a.Content {
		if a.Content[i].Text != b.Content[i].Text || a.Content[i].Type != b.Content[i].Type {
			return false
		}
	}
	return true
}

// messageIndex finds a message in a slice by content.
func messageIndex(messages []types.Message, target types.Message) int {
	for i, m := range messages {
		if sameUserMessage(m, target) {
			return i
		}
	}
	return -1
}

// queuedMessageIndex finds a queued message by content.
func queuedMessageIndex(messages []queuedMessage, target types.Message) int {
	for i, m := range messages {
		if sameUserMessage(m.message, target) {
			return i
		}
	}
	return -1
}

// userMessage builds a user text message.
func userMessage(text string) types.Message {
	return userMessageContent([]types.ContentBlock{types.TextBlock(text)})
}

// userMessageContent builds a user message from content blocks.
func userMessageContent(content []types.ContentBlock) types.Message {
	return types.Message{
		Role:      types.RoleUser,
		Content:   append([]types.ContentBlock(nil), content...),
		Timestamp: time.Now().UnixMilli(),
	}
}

// termSize reads the terminal size from the tty fd.
func termSize(t *engine.Terminal) (int, int) {
	if t == nil {
		return 80, 24
	}
	return engine.TermSize(t.Input())
}

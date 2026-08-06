package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/AlvinPlayz23/myagent/internal/images"
	"github.com/AlvinPlayz23/myagent/internal/types"
	clipboard "golang.design/x/clipboard"
)

type clipboardPayload struct {
	image []byte
	text  string
}

type clipboardResultMsg struct {
	payload clipboardPayload
	err     error
}

type imageAttachment struct {
	block types.ContentBlock
	bytes int
}

// imageAttachments is the composer's pending image component. Clipboard
// images stay in memory and become first-class content blocks on submission.
type imageAttachments struct {
	items []imageAttachment
}

func readNativeClipboard() (clipboardPayload, error) {
	if err := clipboard.Init(); err != nil {
		return clipboardPayload{}, err
	}
	if data := clipboard.Read(clipboard.FmtImage); len(data) > 0 {
		return clipboardPayload{image: data}, nil
	}
	return clipboardPayload{text: string(clipboard.Read(clipboard.FmtText))}, nil
}

func readClipboardCmd(read func() (clipboardPayload, error)) tea.Cmd {
	return func() tea.Msg {
		payload, err := read()
		return clipboardResultMsg{payload: payload, err: err}
	}
}

func (a *imageAttachments) add(data []byte) error {
	block, err := images.FromBytes(data)
	if err != nil {
		return err
	}
	content := []types.ContentBlock{types.TextBlock("")}
	for _, item := range a.items {
		content = append(content, item.block)
	}
	content = append(content, block)
	if _, err := images.ValidateContent(content); err != nil {
		return err
	}
	a.items = append(a.items, imageAttachment{block: block, bytes: len(data)})
	return nil
}

func (a *imageAttachments) removeLast() bool {
	if len(a.items) == 0 {
		return false
	}
	a.items = a.items[:len(a.items)-1]
	return true
}

func (a *imageAttachments) appendTo(content []types.ContentBlock) ([]types.ContentBlock, error) {
	for _, item := range a.items {
		content = append(content, item.block)
	}
	return images.ValidateContent(content)
}

func (a *imageAttachments) clear() { a.items = nil }

func (a *imageAttachments) len() int { return len(a.items) }

func (a *imageAttachments) render(th *theme, width int) string {
	if len(a.items) == 0 {
		return ""
	}
	total := 0
	for _, item := range a.items {
		total += item.bytes
	}
	label := fmt.Sprintf("[image] %d attached (%s)", len(a.items), formatBytes(total))
	hint := "  backspace on an empty prompt removes the last"
	line := th.accent.Render(label) + th.muted.Render(hint)
	if width > 0 && lipgloss.Width(line) > width {
		line = th.accent.Render(label)
	}
	return line
}

func formatBytes(n int) string {
	if n >= 1<<20 {
		return fmt.Sprintf("%.1f MiB", float64(n)/(1<<20))
	}
	if n >= 1<<10 {
		return fmt.Sprintf("%.1f KiB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d B", n)
}

func attachmentDisplay(text string, count int) string {
	label := fmt.Sprintf("[%d image attached]", count)
	if count != 1 {
		label = fmt.Sprintf("[%d images attached]", count)
	}
	if strings.TrimSpace(text) == "" {
		return label
	}
	return text + "\n" + label
}

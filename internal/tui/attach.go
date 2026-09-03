package tui

import (
	"fmt"
	"strings"

	"github.com/AlvinPlayz23/myagent/internal/images"
	"github.com/AlvinPlayz23/myagent/internal/types"
	clipboard "golang.design/x/clipboard"
)

type clipboardPayload struct {
	image []byte
	text  string
}

type clipboardResult struct {
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

func newImageAttachments() *imageAttachments { return &imageAttachments{} }

func readNativeClipboard() (clipboardPayload, error) {
	if err := clipboard.Init(); err != nil {
		return clipboardPayload{}, err
	}
	if data := clipboard.Read(clipboard.FmtImage); len(data) > 0 {
		return clipboardPayload{image: data}, nil
	}
	if data := clipboard.Read(clipboard.FmtText); len(data) > 0 {
		return clipboardPayload{text: string(data)}, nil
	}
	return clipboardPayload{}, fmt.Errorf("clipboard is empty")
}

func (a *imageAttachments) len() int { return len(a.items) }

func (a *imageAttachments) clear() { a.items = nil }

// removeLast drops the most recent attachment, reporting whether one existed.
func (a *imageAttachments) removeLast() bool {
	if len(a.items) == 0 {
		return false
	}
	a.items = a.items[:len(a.items)-1]
	return true
}

// add registers a clipboard image as a pending attachment.
func (a *imageAttachments) add(data []byte) error {
	block, err := images.FromBytes(data)
	if err != nil {
		return err
	}
	a.items = append(a.items, imageAttachment{block: block, bytes: len(data)})
	return nil
}

// appendTo merges pending attachments into the outgoing content blocks.
func (a *imageAttachments) appendTo(content []types.ContentBlock) ([]types.ContentBlock, error) {
	if len(a.items) == 0 {
		return content, nil
	}
	out := append([]types.ContentBlock(nil), content...)
	for _, item := range a.items {
		out = append(out, item.block)
	}
	return out, nil
}

// summary renders the one-line attachment strip shown above the composer.
func (a *imageAttachments) summary(width int) string {
	parts := make([]string, 0, len(a.items))
	total := 0
	for _, item := range a.items {
		total += item.bytes
		parts = append(parts, imageLabel(item.block))
	}
	line := fmt.Sprintf("%d image%s (%s) — backspace removes the last",
		len(a.items), plural(len(a.items)), humanBytes(total))
	_ = parts
	if width > 0 && len(line) > width {
		line = line[:width]
	}
	return line
}

func imageLabel(block types.ContentBlock) string {
	if block.Type == types.ContentImage {
		return "[image]"
	}
	return "[file]"
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func humanBytes(n int) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1fMiB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1fKiB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%dB", n)
	}
}

// attachmentDisplay renders the transcript echo for a prompt that carried
// attachments.
func attachmentDisplay(text string, count int) string {
	var b strings.Builder
	b.WriteString(text)
	if text != "" {
		b.WriteString("\n\n")
	}
	fmt.Fprintf(&b, "[%d attached image%s]", count, plural(count))
	return b.String()
}

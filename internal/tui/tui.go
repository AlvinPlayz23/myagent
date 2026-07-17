package tui

import (
	"context"

	tea "charm.land/bubbletea/v2"

	"github.com/myagent/myagent/internal/agent"
	"github.com/myagent/myagent/internal/session"
	"github.com/myagent/myagent/internal/types"
)

// Run starts the interactive TUI. It drives the agent loop over the given
// config and prior history, persisting every produced message to sess as it
// completes. Blocks until the user quits.
func Run(ctx context.Context, cfg agent.Config, sess *session.Session, history []types.Message, modelID, cwd string) error {
	queue := newMsgQueue()
	r := newRunner(cfg, queue, history)

	th := newTheme()
	md := newMDRenderer()
	m := newModel(ctx, r, queue, th, md, modelID, cwd)

	// Seed the transcript with prior conversation so resumed sessions show
	// their history.
	seedTranscript(m.transcript, history)

	// Persist produced messages by wrapping the runner's history growth: after
	// each run completes we diff and append. Simpler: persist via a hook on the
	// runner. We attach a session persister to the runner.
	r.persist = func(msgs []types.Message) {
		if sess == nil {
			return
		}
		for _, msg := range msgs {
			_ = sess.AppendMessage(msg)
		}
	}

	p := tea.NewProgram(m, tea.WithContext(ctx))
	_, err := p.Run()
	return err
}

// seedTranscript renders prior history into the transcript on resume.
func seedTranscript(t *transcript, history []types.Message) {
	for _, msg := range history {
		switch msg.Role {
		case types.RoleUser:
			t.addUser(textOf(msg))
		case types.RoleAssistant:
			if txt := textOf(msg); txt != "" {
				t.beginAssistant()
				t.appendAssistantDelta(txt)
				t.endAssistant()
			}
			for _, tc := range msg.ToolCalls() {
				t.startTool(tc.ID, tc.Name, tc.Arguments)
			}
		case types.RoleToolResult:
			t.endTool(msg.ToolCallID, &types.ToolResult{Content: msg.Content}, msg.IsError)
		}
	}
}

func textOf(m types.Message) string {
	var parts []string
	for _, c := range m.Content {
		if c.Type == types.ContentText && c.Text != "" {
			parts = append(parts, c.Text)
		}
	}
	if len(parts) == 0 {
		return ""
	}
	out := parts[0]
	for _, p := range parts[1:] {
		out += "\n" + p
	}
	return out
}

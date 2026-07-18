package tui

import (
	"context"
	"strings"
	"testing"

	"github.com/myagent/myagent/internal/agent"
	"github.com/myagent/myagent/internal/types"
)

func TestParseSlashCommand(t *testing.T) {
	tests := []struct {
		input string
		kind  commandKind
		arg   string
		want  string
	}{
		{input: "/help", kind: commandHelp},
		{input: "/clear", kind: commandClear},
		{input: "/new", kind: commandNew},
		{input: "/compact", kind: commandCompact},
		{input: "/model-id test-model", kind: commandModelID, arg: "test-model"},
		{input: "/model-id", want: "usage: /model-id <id>"},
		{input: "/unknown", want: "unknown command: /unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := parseSlashCommand(tt.input)
			if tt.want != "" {
				if err == nil || !strings.Contains(err.Error(), tt.want) {
					t.Fatalf("error = %v, want %q", err, tt.want)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got.kind != tt.kind || got.arg != tt.arg {
				t.Fatalf("command = %#v, want kind %d arg %q", got, tt.kind, tt.arg)
			}
		})
	}
}

func TestLocalCommandsDoNotBecomeMessages(t *testing.T) {
	q := newMsgQueue()
	r := newRunner(agent.Config{}, q, []types.Message{userMessage("prior")})
	m := newModel(context.Background(), r, q, newTheme(), newMDRenderer(), "old-model", "")

	m.input.SetValue("/model-id new-model")
	m.submit(false)
	if r.cfg.Model.ID != "new-model" || m.modelID != "new-model" {
		t.Fatalf("model ids = %q/%q, want new-model", r.cfg.Model.ID, m.modelID)
	}
	if len(r.history) != 1 {
		t.Fatalf("history length = %d, want unchanged history", len(r.history))
	}

	m.input.SetValue("/clear")
	m.submit(false)
	if len(m.transcript.blocks) != 0 {
		t.Fatalf("clear left %d transcript blocks", len(m.transcript.blocks))
	}
	if len(r.history) != 1 {
		t.Fatalf("clear changed history length to %d", len(r.history))
	}
}

func TestNewCommandResetsConversation(t *testing.T) {
	q := newMsgQueue()
	r := newRunner(agent.Config{}, q, []types.Message{userMessage("prior")})
	created := false
	m := newModel(context.Background(), r, q, newTheme(), newMDRenderer(), "model", "", func() error {
		created = true
		r.reset()
		return nil
	})
	m.transcript.addUser("prior")
	m.usage.Input = 10
	m.input.SetValue("/new")
	m.submit(false)

	if !created {
		t.Fatal("new-session callback was not called")
	}
	if len(r.history) != 0 || len(m.transcript.blocks) != 0 || m.usage.Input != 0 {
		t.Fatalf("/new did not reset conversation: history=%d blocks=%d input=%d", len(r.history), len(m.transcript.blocks), m.usage.Input)
	}
}

package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/myagent/myagent/internal/agent"
	"github.com/myagent/myagent/internal/llm"
	modelcatalog "github.com/myagent/myagent/internal/models"
	"github.com/myagent/myagent/internal/session"
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
		{input: "/resume", kind: commandResume},
		{input: "/model", kind: commandModel},
		{input: "/model openrouter/openai/gpt-4.1", kind: commandModel, arg: "openrouter/openai/gpt-4.1"},
		{input: "/providers", kind: commandProviders},
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

func TestProvidersCommandEditsConfiguredProviderAndSavesNewKey(t *testing.T) {
	q := newMsgQueue()
	r := newRunner(agent.Config{}, q, nil)
	m := newModel(context.Background(), r, q, newTheme(), newMDRenderer(), "model", "")
	providers := []modelcatalog.Provider{
		{ID: "openrouter", Name: "OpenRouter", BaseURL: "https://openrouter.ai/api/v1"},
		{ID: "zenmux", Name: "ZenMux", BaseURL: "https://zenmux.ai/api/v1"},
	}
	m.availableProviders = func() []modelcatalog.Provider { return providers }
	m.providerConfigured = func(id string) bool { return id == "openrouter" }
	m.providerAPIKey = func(id string) string {
		if id == "openrouter" {
			return "old-key"
		}
		return ""
	}
	var savedProvider modelcatalog.Provider
	var savedKey string
	m.configureProvider = func(provider modelcatalog.Provider, key string) error {
		savedProvider, savedKey = provider, key
		return nil
	}

	m.runCommand("/providers")
	if !m.providers.active || len(m.providers.items) != 2 {
		t.Fatalf("provider picker = active %v items %d", m.providers.active, len(m.providers.items))
	}
	m.openProviderKeyEntry()
	if m.keyFor.ID != "openrouter" || m.keyInput.Value() != "old-key" {
		t.Fatal("configured provider should open an editor seeded with its stored key")
	}
	m.keyInput.SetValue("replacement-key")
	m.saveProviderKey()
	if savedProvider.ID != "openrouter" || savedKey != "replacement-key" {
		t.Fatalf("edited provider/key = %#v/%q", savedProvider, savedKey)
	}
	m.providers.move(1)
	m.openProviderKeyEntry()
	if m.keyFor.ID != "zenmux" {
		t.Fatalf("key entry provider = %q, want zenmux", m.keyFor.ID)
	}
	m.keyInput.SetValue("key-value")
	m.saveProviderKey()
	if savedProvider.ID != "zenmux" || savedKey != "key-value" {
		t.Fatalf("saved provider/key = %#v/%q", savedProvider, savedKey)
	}
	if m.keyFor.ID != "" || !m.providers.active {
		t.Fatal("picker should return after a successful key save")
	}
}

func TestProvidersCommandDoesNotEditCustomProviderCollision(t *testing.T) {
	q := newMsgQueue()
	r := newRunner(agent.Config{}, q, nil)
	m := newModel(context.Background(), r, q, newTheme(), newMDRenderer(), "model", "")
	_, _ = m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m.providers.open([]modelcatalog.Provider{{ID: "openrouter", Name: "OpenRouter", BaseURL: "https://openrouter.ai/api/v1"}})
	m.providerIsCustom = func(id string) bool { return id == "openrouter" }
	m.providerConfigured = func(string) bool { return false }
	m.providerAPIKey = func(string) string { return "" }

	_, cmd := m.openProviderKeyEntry()
	if cmd != nil {
		t.Fatal("custom collision should not focus the built-in key editor")
	}
	if m.keyFor.ID != "" || !m.providers.active {
		t.Fatal("custom collision opened the built-in key editor")
	}
	if !strings.Contains(m.statusMsg, "managed as a custom provider") {
		t.Fatalf("status = %q", m.statusMsg)
	}
	if !strings.Contains(m.renderProviderPicker(), "managed as custom") {
		t.Fatalf("collision is not marked in provider picker: %q", m.renderProviderPicker())
	}
}

func TestProviderKeyPasteStaysInMaskedInput(t *testing.T) {
	q := newMsgQueue()
	r := newRunner(agent.Config{}, q, nil)
	m := newModel(context.Background(), r, q, newTheme(), newMDRenderer(), "model", "")
	m.providers.open([]modelcatalog.Provider{{ID: "zenmux", Name: "ZenMux"}})
	m.providerConfigured = func(string) bool { return false }
	m.providerAPIKey = func(string) string { return "" }
	_, focus := m.openProviderKeyEntry()
	if focus != nil {
		_ = focus()
	}
	m.input.SetValue("keep this out of the composer")

	updated, _ := m.Update(tea.PasteMsg{Content: "pasted-api-key"})
	got := updated.(*model)
	if got.keyInput.Value() != "pasted-api-key" {
		t.Fatalf("key input = %q, want pasted API key", got.keyInput.Value())
	}
	if got.input.Value() != "keep this out of the composer" {
		t.Fatalf("composer changed to %q", got.input.Value())
	}
}

func TestCommandPickerFilteringAndSelection(t *testing.T) {
	p := newCommandPicker()
	p.sync("/")
	if !p.active || len(p.matched) != len(commandItems) {
		t.Fatalf("picker = active %v, matches %d; want all commands", p.active, len(p.matched))
	}
	p.sync("/cl")
	item, ok := p.selected()
	if !ok || item.name != "/clear" {
		t.Fatalf("selected = %#v, %v; want /clear", item, ok)
	}

	p.sync("/")
	p.move(1)
	item, _ = p.selected()
	if item.name != "/model" {
		t.Fatalf("selected after down = %q, want /model", item.name)
	}
	p.move(-1)
	item, _ = p.selected()
	if item.name != "/help" {
		t.Fatalf("selected after up = %q, want /help", item.name)
	}
}

func TestCommandPickerDismissesUntilInputChanges(t *testing.T) {
	p := newCommandPicker()
	p.sync("/h")
	p.dismiss("/h")
	p.sync("/h")
	if p.active {
		t.Fatal("picker reopened without an input change")
	}
	p.sync("/")
	if !p.active {
		t.Fatal("picker did not reopen after input changed")
	}
	p.sync("")
	if p.active {
		t.Fatal("picker remained active after slash was deleted")
	}
}

func TestCommandPickerAcceptsArgumentCommandWithoutSubmitting(t *testing.T) {
	q := newMsgQueue()
	r := newRunner(agent.Config{}, q, nil)
	m := newModel(context.Background(), r, q, newTheme(), newMDRenderer(), "model", "")
	m.input.SetValue("/m")
	m.picker.sync(m.input.Value())

	_, cmd := m.acceptCommandPicker(true)
	if cmd != nil {
		t.Fatal("argument command should not submit")
	}
	if got := m.input.Value(); got != "/model " {
		t.Fatalf("input = %q, want %q", got, "/model ")
	}
	if m.picker.active {
		t.Fatal("picker remained open after accepting a command")
	}
}

func TestLocalCommandsDoNotBecomeMessages(t *testing.T) {
	q := newMsgQueue()
	r := newRunner(agent.Config{}, q, []types.Message{userMessage("prior")})
	m := newModel(context.Background(), r, q, newTheme(), newMDRenderer(), "old-model", "")

	m.availableModels = func() []modelcatalog.Model { return []modelcatalog.Model{{Provider: "local", ID: "new-model"}} }
	m.selectModel = func(provider, id string) (llm.Provider, llm.Model, error) {
		return r.cfg.Provider, llm.Model{Provider: provider, ID: id}, nil
	}
	m.input.SetValue("/model local/new-model")
	m.submit(false)
	if r.cfg.Model.ID != "new-model" || m.modelID != "local/new-model" {
		t.Fatalf("model ids = %q/%q, want local/new-model", r.cfg.Model.ID, m.modelID)
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

func TestResumeCommandSelectsAndLoadsSession(t *testing.T) {
	q := newMsgQueue()
	r := newRunner(agent.Config{}, q, []types.Message{userMessage("current")})
	m := newModel(context.Background(), r, q, newTheme(), newMDRenderer(), "model", "")
	info := session.Info{ID: "session-2", Modified: time.Now(), Preview: "resumed prompt"}
	m.listSessions = func() ([]session.Info, error) {
		return []session.Info{info}, nil
	}
	resumedHistory := []types.Message{userMessage("resumed prompt")}
	var resumedID string
	m.resumeSession = func(id string) ([]types.Message, error) {
		resumedID = id
		return resumedHistory, nil
	}

	m.runCommand("/resume")
	if !m.sessions.active || len(m.sessions.items) != 1 {
		t.Fatalf("session picker = active %v, items %d", m.sessions.active, len(m.sessions.items))
	}
	m.resumeSelectedSession()
	if resumedID != info.ID {
		t.Fatalf("resumed id = %q, want %q", resumedID, info.ID)
	}
	if m.sessions.active {
		t.Fatal("session picker remained open")
	}
	if len(r.history) != 1 || textOf(r.history[0]) != "resumed prompt" {
		t.Fatalf("runner history = %#v, want resumed history", r.history)
	}
	if len(m.transcript.blocks) != 1 || m.transcript.blocks[0].text != "resumed prompt" {
		t.Fatalf("transcript was not replaced with resumed history: %#v", m.transcript.blocks)
	}
}

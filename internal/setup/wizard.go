// Package setup implements myagent's first-run interactive wizard.
//
// When no config.json exists (or it is blank), main invokes RunWizard before
// starting an interactive session. The wizard collects the required
// OpenAI-compatible credentials, writes them to config.json via config.Save,
// and returns the resolved config so the caller can continue into the TUI
// without re-loading.
//
// The wizard is a self-contained bubbletea v2 program using bubbles/textinput
// for each field. It honors MYAGENT_DIR and uses OPENAI_BASE_URL /
// MYAGENT_MODEL as visible defaults; config.Load reapplies every OPENAI_* env
// override after the config has been saved.
package setup

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/myagent/myagent/internal/config"
)

// Result is what RunWizard returns: the in-memory config the wizard produced
// (mirrors config.Load()'s resolution, i.e. env overrides + defaults applied)
// so the caller can use it directly without re-reading the file.
type Result struct {
	Config *config.Config
}

// field is one wizard input. The API-key field is masked (EchoPassword),
// the others behave as normal single-line text inputs.
type field struct {
	label string
	help  string
	input textinput.Model
}

// wizardModel is the wizard's Bubble Tea model.
type wizardModel struct {
	fields  []*field
	current int

	width, height int
	ready         bool

	err  string
	done bool
	quit bool

	result *config.Config
}

// RunWizard drives the interactive setup wizard. It blocks until the user
// either completes setup (returns the resolved config) or cancels with
// Ctrl+C / Esc (returns ErrCancelled). If no interactive terminal is
// attached, it falls back to ErrNoTty and the caller is expected to surface a
// non-interactive error instead.
func RunWizard(ctx context.Context) (*config.Config, error) {
	if !isInteractive() {
		return nil, ErrNoTty
	}

	m := newWizardModel()
	p := tea.NewProgram(m, tea.WithContext(ctx))
	out, err := p.Run()
	if err != nil {
		return nil, err
	}
	wm, ok := out.(*wizardModel)
	if !ok {
		return nil, fmt.Errorf("setup: unexpected model type %T", out)
	}
	if wm.quit {
		return nil, ErrCancelled
	}
	if wm.result == nil {
		return nil, ErrCancelled
	}
	return wm.result, nil
}

// newWizardModel builds the wizard state and each field's textinput with
// sensible defaults pulled from the environment and config defaults.
func newWizardModel() *wizardModel {
	keyPH := "sk-..."
	basePH := config.DefaultBaseURL
	if v := os.Getenv(config.EnvBaseURL); v != "" {
		basePH = v
	}
	modelPH := config.DefaultModel
	if v := os.Getenv(config.EnvModel); v != "" {
		modelPH = v
	}

	mk := func(masked bool, label, help, ph string) *field {
		ti := textinput.New()
		ti.Placeholder = ph
		ti.CharLimit = 0
		if masked {
			ti.EchoMode = textinput.EchoPassword
			ti.EchoCharacter = '*'
		}
		return &field{label: label, help: help, input: ti}
	}

	m := &wizardModel{
		fields: []*field{
			mk(true, "API key", "OpenAI-compatible API key. Required.", keyPH),
			mk(false, "Base URL", "OpenAI-compatible endpoint.", basePH),
			mk(false, "Model", "Model id (e.g. gpt-4o).", modelPH),
		},
	}
	// Store defaults as real values so accepting each field persists a complete
	// config rather than relying on the runtime defaults after setup.
	m.fields[1].input.SetValue(basePH)
	m.fields[2].input.SetValue(modelPH)
	_ = m.fields[0].input.Focus()
	return m
}

// Init starts nothing special; the first field is already focused.
func (m *wizardModel) Init() tea.Cmd { return nil }

// Update routes messages: resize, key presses, and textinput updates.
func (m *wizardModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.ready = true
		m.resizeInputs()
		return m, nil

	case tea.KeyPressMsg:
		return m.onKey(msg)
	}

	if m.done || m.quit {
		return m, nil
	}
	f := m.fields[m.current]
	var cmd tea.Cmd
	f.input, cmd = f.input.Update(msg)
	return m, cmd
}

// onKey routes key presses for the current field and navigation.
func (m *wizardModel) onKey(k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	ks := k.Keystroke()
	switch ks {
	case "ctrl+c":
		m.quit = true
		return m, tea.Quit
	case "esc":
		if m.current == 0 && m.fields[0].input.Value() == "" {
			m.quit = true
			return m, tea.Quit
		}
		// Esc otherwise clears the current field, like a typical dialog.
		m.fields[m.current].input.Reset()
		m.err = ""
		return m, nil
	case "enter":
		return m.submitField()
	case "tab", "ctrl+n":
		return m.nextField(1)
	case "shift+tab", "ctrl+p":
		return m.nextField(-1)
	}

	if m.done || m.quit {
		return m, nil
	}
	f := m.fields[m.current]
	var cmd tea.Cmd
	f.input, cmd = f.input.Update(k)
	return m, cmd
}

// submitField validates the current field and either advances or finishes.
func (m *wizardModel) submitField() (tea.Model, tea.Cmd) {
	f := m.fields[m.current]
	m.err = ""
	if f.label == "API key" && strings.TrimSpace(f.input.Value()) == "" {
		m.err = "API key is required. Press Esc to cancel."
		return m, nil
	}
	return m.nextField(1)
}

// nextField moves focus forward/back; when advancing past the last field it
// finalizes and writes the config.
func (m *wizardModel) nextField(dir int) (tea.Model, tea.Cmd) {
	m.err = ""
	next := m.current + dir
	if next >= len(m.fields) {
		return m.finalize()
	}
	if next < 0 {
		next = 0
	}
	m.fields[m.current].input.Blur()
	m.current = next
	if cmd := m.fields[next].input.Focus(); cmd != nil {
		return m, cmd
	}
	return m, nil
}

// finalize collects field values, fills defaults, writes config.json, and
// resolves the final config (env overrides + defaults) to hand back.
func (m *wizardModel) finalize() (tea.Model, tea.Cmd) {
	apiKey := strings.TrimSpace(m.fields[0].input.Value())
	baseURL := strings.TrimSpace(m.fields[1].input.Value())
	model := strings.TrimSpace(m.fields[2].input.Value())

	toSave := &config.Config{
		APIKey:  apiKey,
		BaseURL: baseURL,
		Model:   model,
	}
	if err := config.Save(toSave); err != nil {
		m.err = "Failed to write config: " + err.Error()
		return m, nil
	}

	// Re-resolve so env overrides and defaults apply exactly as on subsequent
	// runs, mirroring config.Load semantics.
	cfg, err := config.Load()
	if err != nil {
		m.err = "Config saved but could not be re-read: " + err.Error()
		return m, nil
	}
	if cfg.APIKey == "" {
		m.err = "Config saved but no API key is available."
		return m, nil
	}

	m.result = cfg
	m.done = true
	return m, tea.Quit
}

// resizeInputs gives each textinput the full width minus the label indent.
func (m *wizardModel) resizeInputs() {
	if !m.ready || m.width <= 0 {
		return
	}
	w := m.width - labelWidth(m.width) - 2
	if w < 20 {
		w = 20
	}
	for _, f := range m.fields {
		f.input.SetWidth(w)
	}
}

// View renders the wizard screen.
func (m *wizardModel) View() tea.View {
	if !m.ready {
		return tea.NewView("")
	}
	var sb strings.Builder
	sb.WriteString(titleStyle.Render("myagent setup"))
	sb.WriteString("\n\n")
	sb.WriteString(mutedStyle.Render("First run detected: no config yet. Let's fix that."))
	sb.WriteString("\n\n")

	for i, f := range m.fields {
		marker := "  "
		if i == m.current {
			marker = accentStyle.Render(">")
		}
		label := labelStyle.Render(f.label)
		line := fmt.Sprintf("%s %s %s", marker, label, f.input.View())
		if i == m.current {
			sb.WriteString(activeLine.Render(line))
		} else {
			sb.WriteString(mutedLine.Render(line))
		}
		sb.WriteString("\n")
	}

	// Keep the hint on a fixed line so moving focus never shifts the fields.
	sb.WriteString(helpStyle.Render(indent(m.fields[m.current].help)))
	sb.WriteString("\n")

	if m.err != "" {
		sb.WriteString("\n")
		sb.WriteString(errStyle.Render(m.err))
		sb.WriteString("\n")
	}

	sb.WriteString("\n")
	sb.WriteString(mutedStyle.Render("Tab/Enter next | Shift+Tab prev | Esc cancel | Ctrl+C quit"))
	return tea.NewView(sb.String())
}

// Errors exported for callers (main.go) to distinguish cancellation and
// non-interactive terminals.
var (
	ErrCancelled = errors.New("setup cancelled")
	ErrNoTty     = errors.New("setup requires an interactive terminal (run `myagent` without -p to configure)")
)

// Shared styles. Kept local because they're wizard-specific; tui/theme.go
// owns the runtime UI palette.
var (
	titleStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("39"))
	mutedStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	accentStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("39"))
	labelStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("255"))
	helpStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	errStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("203"))
	activeLine  = lipgloss.NewStyle().Foreground(lipgloss.Color("255"))
	mutedLine   = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
)

// labelWidth is the column at which input fields start, chosen to fit the
// longest label ("Base URL").
func labelWidth(termWidth int) int {
	_ = termWidth
	return 9
}

func indent(s string) string {
	return "  " + s
}

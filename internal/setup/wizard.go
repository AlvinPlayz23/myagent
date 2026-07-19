// Package setup implements myagent's interactive provider manager.
package setup

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/myagent/myagent/internal/config"
	"github.com/myagent/myagent/internal/llm"
)

type screen int

const (
	screenList screen = iota
	screenEditor
	screenModelPicker
	screenDelete
)

type field struct {
	label string
	help  string
	input textinput.Model
}

type modelsDiscoveredMsg struct {
	models []string
	err    error
}

// wizardModel manages the named OpenAI-compatible providers stored in
// config.json. It is also the first-run setup flow when no providers exist.
type wizardModel struct {
	ctx context.Context
	cfg *config.Config

	screen    screen
	providers []string
	selected  int
	editing   string
	fields    []*field
	current   int

	modelSearch textinput.Model
	models      []string
	modelIndex  int
	discovering bool

	width, height int
	ready         bool
	err           string
	loadErr       bool
	done          bool
	quit          bool
	result        *config.Config
}

func RunWizard(ctx context.Context) (*config.Config, error) {
	if !isInteractive() {
		return nil, ErrNoTty
	}

	m := newWizardModel()
	m.ctx = ctx
	p := tea.NewProgram(m, tea.WithContext(ctx))
	out, err := p.Run()
	if err != nil {
		return nil, err
	}
	m, ok := out.(*wizardModel)
	if !ok {
		return nil, fmt.Errorf("setup: unexpected model type %T", out)
	}
	if m.result == nil {
		return nil, ErrCancelled
	}
	return m.result, nil
}

func newWizardModel() *wizardModel {
	cfg, err := config.Load()
	if cfg == nil {
		cfg = &config.Config{}
	}
	ctx := context.Background()
	m := &wizardModel{cfg: cfg, ctx: ctx}
	m.modelSearch = textinput.New()
	m.modelSearch.Placeholder = "Filter models or type a model ID"
	m.modelSearch.CharLimit = 0
	if err != nil {
		m.err = "Could not read existing config: " + err.Error()
		m.loadErr = true
		return m
	}
	m.refreshProviders()
	if len(m.providers) == 0 {
		m.openEditor("")
	}
	return m
}

func (m *wizardModel) Init() tea.Cmd { return nil }

func (m *wizardModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height, m.ready = msg.Width, msg.Height, true
		m.resizeInputs()
		return m, nil
	case modelsDiscoveredMsg:
		m.discovering = false
		if msg.err != nil {
			m.err = msg.err.Error()
			return m, nil
		}
		m.models = mergeModels(msg.models, m.fields[3].input.Value())
		m.modelIndex = 0
		m.err = ""
		return m, nil
	case tea.KeyPressMsg:
		return m.onKey(msg)
	}
	if m.screen == screenModelPicker && !m.discovering {
		var cmd tea.Cmd
		m.modelSearch, cmd = m.modelSearch.Update(msg)
		m.clampModelIndex()
		return m, cmd
	}
	if m.screen != screenEditor || m.done || m.quit || len(m.fields) == 0 {
		return m, nil
	}
	var cmd tea.Cmd
	m.fields[m.current].input, cmd = m.fields[m.current].input.Update(msg)
	return m, cmd
}

func (m *wizardModel) onKey(k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.done || m.quit {
		return m, nil
	}
	if m.loadErr {
		if k.Keystroke() == "ctrl+c" || k.Keystroke() == "q" || k.Keystroke() == "esc" {
			m.quit = true
			return m, tea.Quit
		}
		return m, nil
	}
	switch m.screen {
	case screenList:
		return m.onListKey(k)
	case screenDelete:
		return m.onDeleteKey(k)
	case screenModelPicker:
		return m.onModelPickerKey(k)
	default:
		return m.onEditorKey(k)
	}
}

func (m *wizardModel) onListKey(k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch k.Keystroke() {
	case "ctrl+c", "q", "esc":
		m.result = m.cfg
		m.quit = true
		return m, tea.Quit
	case "up", "k":
		if m.selected > 0 {
			m.selected--
		}
	case "down", "j":
		if m.selected < len(m.providers)-1 {
			m.selected++
		}
	case "a":
		m.openEditor("")
	case "e":
		m.openEditor(m.selectedProvider())
	case "enter":
		m.makeDefault(m.selectedProvider())
	case "d":
		if name := m.selectedProvider(); name != "" {
			if m.isDefault(name) {
				m.err = "Select another provider as default before deleting this one."
			} else {
				m.screen = screenDelete
				m.err = ""
			}
		}
	}
	return m, nil
}

func (m *wizardModel) onDeleteKey(k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch k.Keystroke() {
	case "y":
		name := m.selectedProvider()
		delete(m.cfg.Providers, name)
		if err := config.Save(m.cfg); err != nil {
			m.err = "Failed to write config: " + err.Error()
			m.screen = screenList
			return m, nil
		}
		m.refreshProviders()
		m.screen = screenList
		m.result = m.cfg
	case "n", "esc", "ctrl+c":
		m.screen = screenList
	}
	return m, nil
}

func (m *wizardModel) onEditorKey(k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch k.Keystroke() {
	case "ctrl+c":
		m.quit = true
		return m, tea.Quit
	case "ctrl+d":
		return m.startDiscovery()
	case "esc":
		if len(m.providers) == 0 {
			m.quit = true
			return m, tea.Quit
		}
		m.screen = screenList
		m.err = ""
		return m, nil
	case "enter", "tab", "ctrl+n":
		return m.nextField(1)
	case "shift+tab", "ctrl+p":
		return m.nextField(-1)
	}
	var cmd tea.Cmd
	m.fields[m.current].input, cmd = m.fields[m.current].input.Update(k)
	return m, cmd
}

func (m *wizardModel) startDiscovery() (tea.Model, tea.Cmd) {
	baseURL := strings.TrimSpace(m.fields[2].input.Value())
	apiKey := strings.TrimSpace(m.fields[1].input.Value())
	if baseURL == "" {
		m.err = "Enter a base URL before discovering models."
		return m, nil
	}
	m.screen = screenModelPicker
	m.discovering = true
	m.err = ""
	m.models = mergeModels(nil, m.fields[3].input.Value())
	m.modelIndex = 0
	m.modelSearch.SetValue("")
	_ = m.modelSearch.Focus()
	ctx := m.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	return m, func() tea.Msg {
		client := &http.Client{Timeout: 15 * time.Second}
		models, err := llm.ListOpenAIModels(ctx, client, apiKey, baseURL)
		return modelsDiscoveredMsg{models: models, err: err}
	}
}

func (m *wizardModel) onModelPickerKey(k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch k.Keystroke() {
	case "ctrl+c":
		m.quit = true
		return m, tea.Quit
	case "esc":
		m.screen = screenEditor
		m.err = ""
		m.modelSearch.Blur()
		return m, m.fields[m.current].input.Focus()
	case "ctrl+r":
		if m.discovering {
			return m, nil
		}
		return m.startDiscovery()
	case "up":
		if m.modelIndex > 0 {
			m.modelIndex--
		}
		return m, nil
	case "down":
		if m.modelIndex < len(m.filteredModels())-1 {
			m.modelIndex++
		}
		return m, nil
	case "ctrl+a":
		model := strings.TrimSpace(m.modelSearch.Value())
		if model == "" {
			m.err = "Type a model ID before adding it."
			return m, nil
		}
		m.models = mergeModels(m.models, model)
		m.err = ""
		m.modelIndex = 0
		return m, nil
	case "enter":
		models := m.filteredModels()
		if len(models) == 0 {
			m.err = "No model selected. Type an ID and press Ctrl+A to add it."
			return m, nil
		}
		m.fields[3].input.SetValue(models[m.modelIndex])
		m.screen = screenEditor
		m.current = 3
		m.err = ""
		m.modelSearch.Blur()
		return m, m.fields[3].input.Focus()
	}
	var cmd tea.Cmd
	m.modelSearch, cmd = m.modelSearch.Update(k)
	m.clampModelIndex()
	return m, cmd
}

func mergeModels(models []string, extra string) []string {
	seen := make(map[string]struct{}, len(models)+1)
	out := make([]string, 0, len(models)+1)
	for _, model := range append(append([]string(nil), models...), extra) {
		model = strings.TrimSpace(model)
		if model == "" {
			continue
		}
		if _, exists := seen[model]; exists {
			continue
		}
		seen[model] = struct{}{}
		out = append(out, model)
	}
	sort.Strings(out)
	return out
}

func (m *wizardModel) filteredModels() []string {
	query := strings.ToLower(strings.TrimSpace(m.modelSearch.Value()))
	if query == "" {
		return m.models
	}
	out := make([]string, 0, len(m.models))
	for _, model := range m.models {
		if strings.Contains(strings.ToLower(model), query) {
			out = append(out, model)
		}
	}
	return out
}

func (m *wizardModel) clampModelIndex() {
	models := m.filteredModels()
	if len(models) == 0 {
		m.modelIndex = 0
	} else if m.modelIndex >= len(models) {
		m.modelIndex = len(models) - 1
	}
}

func (m *wizardModel) nextField(dir int) (tea.Model, tea.Cmd) {
	m.err = ""
	next := m.current + dir
	if next >= len(m.fields) {
		return m.saveProvider()
	}
	if next < 0 {
		next = 0
	}
	m.fields[m.current].input.Blur()
	m.current = next
	return m, m.fields[next].input.Focus()
}

func (m *wizardModel) openEditor(name string) {
	m.screen, m.editing, m.current, m.err = screenEditor, name, 0, ""
	provider := config.ProviderConfig{Type: config.DefaultProviderType, BaseURL: config.DefaultBaseURL}
	model := config.DefaultModel
	if v := os.Getenv(config.EnvBaseURL); v != "" && name == "" {
		provider.BaseURL = v
	}
	if v := os.Getenv(config.EnvModel); v != "" && name == "" {
		model = v
	}
	if name != "" {
		provider = m.cfg.Providers[name]
		if provider.Model != "" {
			model = provider.Model
		} else if defaultName, defaultModel, ok := m.defaultRef(); ok && defaultName == name {
			model = defaultModel
		}
	} else {
		name = config.DefaultProviderName
		if _, exists := m.cfg.Providers[name]; exists {
			name = "provider"
		}
	}
	m.fields = []*field{
		m.newField(false, "Name", "A unique provider name used with --provider.", name),
		m.newField(true, "API key", "Optional for local endpoints such as Ollama.", provider.APIKey),
		m.newField(false, "Base URL", "OpenAI-compatible endpoint URL.", provider.BaseURL),
		m.newField(false, "Model", "Model id. Saving makes this provider the default.", model),
	}
	_ = m.fields[0].input.Focus()
	m.resizeInputs()
}

func (m *wizardModel) newField(masked bool, label, help, value string) *field {
	ti := textinput.New()
	ti.CharLimit = 0
	ti.SetValue(value)
	if masked {
		ti.EchoMode = textinput.EchoPassword
		ti.EchoCharacter = '*'
	}
	return &field{label: label, help: help, input: ti}
}

func (m *wizardModel) saveProvider() (tea.Model, tea.Cmd) {
	name := strings.TrimSpace(m.fields[0].input.Value())
	apiKey := strings.TrimSpace(m.fields[1].input.Value())
	baseURL := strings.TrimSpace(m.fields[2].input.Value())
	model := strings.TrimSpace(m.fields[3].input.Value())
	if name == "" || strings.Contains(name, "/") || strings.ContainsAny(name, " \t\n") {
		m.err = "Name must be non-empty and cannot contain spaces or '/'."
		return m, nil
	}
	if baseURL == "" || model == "" {
		m.err = "Base URL and model are required."
		return m, nil
	}
	if m.cfg.Providers == nil {
		m.cfg.Providers = make(map[string]config.ProviderConfig)
	}
	if m.editing != "" && m.editing != name {
		if _, exists := m.cfg.Providers[name]; exists {
			m.err = fmt.Sprintf("Provider %q already exists.", name)
			return m, nil
		}
		delete(m.cfg.Providers, m.editing)
	}
	m.cfg.Providers[name] = config.ProviderConfig{Type: config.DefaultProviderType, APIKey: apiKey, BaseURL: baseURL, Model: model}
	m.cfg.DefaultModel = name + "/" + model
	if err := config.Save(m.cfg); err != nil {
		m.err = "Failed to write config: " + err.Error()
		return m, nil
	}
	if _, _, err := m.cfg.Resolve("", "", ""); err != nil {
		m.err = "Config saved but is invalid: " + err.Error()
		return m, nil
	}
	m.refreshProviders()
	m.selected = m.indexOf(name)
	m.result = m.cfg
	if len(m.providers) == 1 {
		m.done = true
		return m, tea.Quit
	}
	m.screen = screenList
	return m, nil
}

func (m *wizardModel) makeDefault(name string) {
	if name == "" {
		return
	}
	model := m.cfg.Providers[name].Model
	if model == "" {
		defaultName, defaultModel, ok := m.defaultRef()
		if ok && defaultName == name {
			model = defaultModel
		}
	}
	if model == "" {
		m.err = "Edit this provider to choose its model before making it default."
		return
	}
	m.cfg.DefaultModel = name + "/" + model
	if err := config.Save(m.cfg); err != nil {
		m.err = "Failed to write config: " + err.Error()
		return
	}
	m.result = m.cfg
	m.err = ""
}

func (m *wizardModel) refreshProviders() {
	m.providers = m.providers[:0]
	for name := range m.cfg.Providers {
		m.providers = append(m.providers, name)
	}
	sort.Strings(m.providers)
	if m.selected >= len(m.providers) {
		m.selected = max(0, len(m.providers)-1)
	}
}

func (m *wizardModel) selectedProvider() string {
	if m.selected < 0 || m.selected >= len(m.providers) {
		return ""
	}
	return m.providers[m.selected]
}

func (m *wizardModel) indexOf(name string) int {
	for i, provider := range m.providers {
		if provider == name {
			return i
		}
	}
	return 0
}

func (m *wizardModel) defaultRef() (string, string, bool) {
	provider, model, ok := strings.Cut(strings.TrimSpace(m.cfg.DefaultModel), "/")
	return provider, model, ok && provider != "" && model != ""
}

func (m *wizardModel) isDefault(name string) bool {
	provider, _, ok := m.defaultRef()
	return ok && provider == name
}

func (m *wizardModel) resizeInputs() {
	if !m.ready || m.width <= 0 {
		return
	}
	w := m.width - 12
	if w < 20 {
		w = 20
	}
	for _, f := range m.fields {
		f.input.SetWidth(w)
	}
	m.modelSearch.SetWidth(w)
}

func (m *wizardModel) View() tea.View {
	if !m.ready {
		return tea.NewView("")
	}
	var sb strings.Builder
	sb.WriteString(titleStyle.Render("myagent providers"))
	sb.WriteString("\n\n")
	if m.screen == screenEditor {
		m.renderEditor(&sb)
	} else if m.screen == screenModelPicker {
		m.renderModelPicker(&sb)
	} else {
		m.renderList(&sb)
	}
	if m.err != "" {
		sb.WriteString("\n")
		sb.WriteString(errStyle.Render(m.err))
		sb.WriteString("\n")
	}
	return tea.NewView(sb.String())
}

func (m *wizardModel) renderList(sb *strings.Builder) {
	sb.WriteString(mutedStyle.Render("Choose the default provider, or manage saved OpenAI-compatible endpoints."))
	sb.WriteString("\n\n")
	for i, name := range m.providers {
		marker := "  "
		if i == m.selected {
			marker = accentStyle.Render(">")
		}
		defaultMark := " "
		if m.isDefault(name) {
			defaultMark = accentStyle.Render("*")
		}
		endpoint := m.cfg.Providers[name].BaseURL
		sb.WriteString(fmt.Sprintf("%s %s %-16s %s\n", marker, defaultMark, name, mutedStyle.Render(endpoint)))
	}
	if m.screen == screenDelete {
		sb.WriteString("\n")
		sb.WriteString(errStyle.Render(fmt.Sprintf("Delete %q? Press y to confirm, n or Esc to cancel.", m.selectedProvider())))
		sb.WriteString("\n")
	}
	sb.WriteString("\n")
	sb.WriteString(mutedStyle.Render("Enter default | a add | e edit | d delete | q quit"))
}

func (m *wizardModel) renderModelPicker(sb *strings.Builder) {
	sb.WriteString(mutedStyle.Render("Discover models from the provider, search them, or add an exact model ID."))
	sb.WriteString("\n\n")
	if m.discovering {
		sb.WriteString(accentStyle.Render("Discovering models..."))
		sb.WriteString("\n\n")
		sb.WriteString(mutedStyle.Render("Esc back | Ctrl+C quit"))
		return
	}
	sb.WriteString(labelStyle.Render("Search  "))
	sb.WriteString(m.modelSearch.View())
	sb.WriteString("\n\n")
	models := m.filteredModels()
	if len(models) == 0 {
		sb.WriteString(mutedStyle.Render("  No matching models. Ctrl+A adds the typed ID."))
		sb.WriteString("\n")
	} else {
		start := max(0, m.modelIndex-6)
		end := min(len(models), start+12)
		for i := start; i < end; i++ {
			marker := "  "
			if i == m.modelIndex {
				marker = accentStyle.Render(">")
			}
			sb.WriteString(fmt.Sprintf("%s %s\n", marker, models[i]))
		}
	}
	sb.WriteString("\n")
	sb.WriteString(mutedStyle.Render("Up/Down select | Enter use | Ctrl+A add typed ID | Ctrl+R retry | Esc back"))
}

func (m *wizardModel) renderEditor(sb *strings.Builder) {
	if m.editing == "" {
		sb.WriteString(mutedStyle.Render("Add a provider. The first saved provider is used immediately."))
	} else {
		sb.WriteString(mutedStyle.Render("Edit provider. Saving makes it the default provider."))
	}
	sb.WriteString("\n\n")
	for i, f := range m.fields {
		marker := "  "
		if i == m.current {
			marker = accentStyle.Render(">")
		}
		line := fmt.Sprintf("%s %-9s %s", marker, labelStyle.Render(f.label), f.input.View())
		if i == m.current {
			sb.WriteString(activeLine.Render(line))
		} else {
			sb.WriteString(mutedLine.Render(line))
		}
		sb.WriteString("\n")
	}
	sb.WriteString(helpStyle.Render("  " + m.fields[m.current].help))
	sb.WriteString("\n\n")
	sb.WriteString(mutedStyle.Render("Tab/Enter next | Shift+Tab prev | Ctrl+D discover models | Esc back | Ctrl+C quit"))
}

var (
	ErrCancelled = errors.New("setup cancelled")
	ErrNoTty     = errors.New("setup requires an interactive terminal (run `myagent` without -p to configure)")
)

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

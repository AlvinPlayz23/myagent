// Package plugin implements the myagent plugin system (PLUGINS.md).
//
// Plugin authors only edit plugins.json. This package loads, merges and
// validates the files, turns tool entries into tools.Tool implementations
// (ShellTool, delegating execution to tools.BashTool so timeout/abort,
// truncation and process-tree semantics cannot drift), and exposes command
// and profile definitions for the TUI / serve layers.
package plugin

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"text/template"

	"github.com/AlvinPlayz23/myagent/internal/config"
	"github.com/AlvinPlayz23/myagent/internal/llm"
)

// Limits from PLUGINS.md §3 / §6 / §10.
const (
	maxNameLen       = 32
	defaultTimeoutMs = 30000
	minTimeoutMs     = 1000
	maxTimeoutMs     = 120000
	maxBashDeny      = 20
	maxBashDenyLen   = 200
	maxInstructions  = 4000
)

// Built-in tool names (internal/tools/default.go).
var builtinTools = map[string]bool{"read": true, "write": true, "edit": true, "bash": true}

// Built-in slash commands (internal/tui/commands.go commandItems, including
// the hidden /models alias).
var builtinCommands = map[string]bool{
	"/help": true, "/model": true, "/models": true, "/effort": true,
	"/providers": true, "/customize": true, "/compact": true, "/clear": true,
	"/new": true, "/resume": true, "/rename": true, "/export": true,
	"/init": true, "/thinking": true,
}

var (
	toolNameRe    = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)
	fieldRe       = regexp.MustCompile(`\{\{\s*\.([A-Za-z0-9_]+)`)
	argsVarRe     = regexp.MustCompile(`\{\{\s*\$args\s*\}\}`)
	templateRefRe = regexp.MustCompile(`\{\{\s*\.([A-Za-z0-9_]+)`)
)

// ToolDef is a single plugin tool (§3.1).
type ToolDef struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
	Command     string         `json:"command"`
	TimeoutMs   int            `json:"timeoutMs,omitempty"`
}

// CommandDef is a single plugin slash command (§3.2).
type CommandDef struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Prompt      string `json:"prompt,omitempty"`
	Run         string `json:"run,omitempty"`
	TimeoutMs   int    `json:"timeoutMs,omitempty"`
}

// ProfileDef is a named mode (§10.1).
type ProfileDef struct {
	Name         string   `json:"name"`
	Description  string   `json:"description"`
	Tools        []string `json:"tools,omitempty"`
	BashDeny     []string `json:"bashDeny,omitempty"`
	Instructions string   `json:"instructions,omitempty"`
	Provider     string   `json:"provider,omitempty"`
	Model        string   `json:"model,omitempty"`
	Effort       string   `json:"effort,omitempty"`
}

// File is the top-level plugins.json shape.
type File struct {
	Enabled  *bool        `json:"enabled,omitempty"`
	Tools    []ToolDef    `json:"tools,omitempty"`
	Commands []CommandDef `json:"commands,omitempty"`
	Profiles []ProfileDef `json:"profiles,omitempty"`
}

// Bundle is the merged, validated result of loading global + project files.
type Bundle struct {
	// Disabled is true when either file set enabled:false or --no-plugins.
	Disabled bool
	Tools    []ToolDef
	Commands []CommandDef
	Profiles []ProfileDef
	// Warnings lists every skipped entry / collision, in load order.
	Warnings []string
	// commandsByName and profilesByName index the merged lists.
	commandsByName map[string]*CommandDef
	profilesByName map[string]*ProfileDef
}

// Empty reports whether the bundle carries no definitions.
func (b *Bundle) Empty() bool {
	if b == nil {
		return true
	}
	return len(b.Tools) == 0 && len(b.Commands) == 0 && len(b.Profiles) == 0
}

// Summary renders "Loaded plugins: ..." (§7).
func (b *Bundle) Summary() string {
	if b == nil || b.Disabled || b.Empty() {
		return "Loaded plugins: none"
	}
	parts := []string{}
	if len(b.Tools) > 0 {
		names := make([]string, 0, len(b.Tools))
		for _, t := range b.Tools {
			names = append(names, t.Name)
		}
		parts = append(parts, fmt.Sprintf("%d tool%s (%s)", len(b.Tools), plural(len(b.Tools)), strings.Join(names, ", ")))
	}
	if len(b.Commands) > 0 {
		names := make([]string, 0, len(b.Commands))
		for _, c := range b.Commands {
			names = append(names, c.Name)
		}
		parts = append(parts, fmt.Sprintf("%d command%s (%s)", len(b.Commands), plural(len(b.Commands)), strings.Join(names, ", ")))
	}
	if len(b.Profiles) > 0 {
		names := make([]string, 0, len(b.Profiles))
		for _, p := range b.Profiles {
			names = append(names, p.Name)
		}
		parts = append(parts, fmt.Sprintf("%d profile%s (%s)", len(b.Profiles), plural(len(b.Profiles)), strings.Join(names, ", ")))
	}
	return "Loaded plugins: " + strings.Join(parts, ", ")
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// Command returns the merged command definition for name, or nil.
func (b *Bundle) Command(name string) *CommandDef {
	if b == nil {
		return nil
	}
	return b.commandsByName[name]
}

// Profile returns the merged profile definition for name, or nil.
func (b *Bundle) Profile(name string) *ProfileDef {
	if b == nil {
		return nil
	}
	return b.profilesByName[name]
}

// ProfileNames returns profile names in definition order.
func (b *Bundle) ProfileNames() []string {
	if b == nil {
		return nil
	}
	out := make([]string, 0, len(b.Profiles))
	for _, p := range b.Profiles {
		out = append(out, p.Name)
	}
	return out
}

// GlobalPath returns ~/.myagent/plugins.json.
func GlobalPath() (string, error) {
	dir, err := config.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "plugins.json"), nil
}

// ProjectPath returns ./.myagent/plugins.json for cwd.
func ProjectPath(cwd string) string {
	return filepath.Join(cwd, ".myagent", "plugins.json")
}

// Load merges global + project plugins.json for cwd. Missing/unreadable
// files are skipped, never fatal. When noPlugins is true the bundle is
// marked Disabled and carries no definitions.
func Load(cwd string, noPlugins bool) *Bundle {
	b := &Bundle{
		commandsByName: map[string]*CommandDef{},
		profilesByName: map[string]*ProfileDef{},
	}
	if noPlugins {
		b.Disabled = true
		return b
	}
	var globalFile, projectFile *File
	var globalSet, projectSet bool
	if p, err := GlobalPath(); err == nil {
		if f, ok, w := readFile(p, "global"); ok {
			globalFile, globalSet = f, true
		} else if w != "" {
			b.Warnings = append(b.Warnings, w)
		}
	}
	if data, err := os.ReadFile(ProjectPath(cwd)); err == nil {
		if f, w := parseFile(data, "project"); f != nil {
			projectFile, projectSet = f, true
		} else if w != "" {
			b.Warnings = append(b.Warnings, w)
		}
	} else if !os.IsNotExist(err) {
		b.Warnings = append(b.Warnings, fmt.Sprintf("project plugins.json unreadable: %v", err))
	}
	// enabled:false in either file is a kill-switch.
	if (globalSet && globalFile.Enabled != nil && !*globalFile.Enabled) ||
		(projectSet && projectFile.Enabled != nil && !*projectFile.Enabled) {
		b.Disabled = true
		return b
	}
	var gTools []ToolDef
	var gCommands []CommandDef
	var gProfiles []ProfileDef
	if globalSet {
		gTools, gCommands, gProfiles = globalFile.Tools, globalFile.Commands, globalFile.Profiles
	}
	var pTools []ToolDef
	var pCommands []CommandDef
	var pProfiles []ProfileDef
	if projectSet {
		pTools, pCommands, pProfiles = projectFile.Tools, projectFile.Commands, projectFile.Profiles
	}
	b.mergeTools(gTools, pTools)
	b.mergeCommands(gCommands, pCommands)
	b.mergeProfiles(gTools, pTools, gProfiles, pProfiles)
	return b
}

// LoadBytes merges two raw files (used by tests and `plugin validate`).
func LoadBytes(globalData, projectData []byte) *Bundle {
	b := &Bundle{
		commandsByName: map[string]*CommandDef{},
		profilesByName: map[string]*ProfileDef{},
	}
	var gTools []ToolDef
	var gCommands []CommandDef
	var gProfiles []ProfileDef
	var pTools []ToolDef
	var pCommands []CommandDef
	var pProfiles []ProfileDef
	if len(globalData) > 0 {
		if f, w := parseFile(globalData, "global"); f != nil {
			if f.Enabled != nil && !*f.Enabled {
				b.Disabled = true
				return b
			}
			gTools, gCommands, gProfiles = f.Tools, f.Commands, f.Profiles
		} else if w != "" {
			b.Warnings = append(b.Warnings, w)
		}
	}
	if len(projectData) > 0 {
		if f, w := parseFile(projectData, "project"); f != nil {
			if f.Enabled != nil && !*f.Enabled {
				b.Disabled = true
				return b
			}
			pTools, pCommands, pProfiles = f.Tools, f.Commands, f.Profiles
		} else if w != "" {
			b.Warnings = append(b.Warnings, w)
		}
	}
	b.mergeTools(gTools, pTools)
	b.mergeCommands(gCommands, pCommands)
	b.mergeProfiles(gTools, pTools, gProfiles, pProfiles)
	return b
}

func readFile(path, scope string) (*File, bool, string) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, ""
		}
		return nil, false, fmt.Sprintf("%s plugins.json unreadable: %v", scope, err)
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return &File{}, true, ""
	}
	f, w := parseFile(data, scope)
	if f == nil {
		return nil, false, w
	}
	return f, true, ""
}

func parseFile(data []byte, scope string) (*File, string) {
	var f File
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Sprintf("%s plugins.json: invalid JSON: %v", scope, err)
	}
	return &f, ""
}

func (b *Bundle) mergeTools(global, project []ToolDef) {
	seen := map[string]bool{}
	add := func(t ToolDef, scope string) {
		w, ok := validateTool(t)
		if !ok {
			b.Warnings = append(b.Warnings, fmt.Sprintf("%s tool %q skipped: %s", scope, t.Name, w))
			return
		}
		if seen[t.Name] {
			// project wins on cross-file collision.
			for i, e := range b.Tools {
				if e.Name == t.Name {
					b.Tools[i] = t
					break
				}
			}
			b.Warnings = append(b.Warnings, fmt.Sprintf("project tool %q overrides global", t.Name))
			return
		}
		seen[t.Name] = true
		b.Tools = append(b.Tools, t)
	}
	seenGlobal := map[string]bool{}
	for _, t := range global {
		if seenGlobal[t.Name] {
			b.Warnings = append(b.Warnings, fmt.Sprintf("global tool %q skipped: duplicate name (first wins)", t.Name))
			continue
		}
		seenGlobal[t.Name] = true
		add(t, "global")
	}
	seenProject := map[string]bool{}
	for _, t := range project {
		if seenProject[t.Name] {
			b.Warnings = append(b.Warnings, fmt.Sprintf("project tool %q skipped: duplicate name (first wins)", t.Name))
			continue
		}
		seenProject[t.Name] = true
		add(t, "project")
	}
	// clamp timeouts post-merge
	for i := range b.Tools {
		if b.Tools[i].TimeoutMs == 0 {
			b.Tools[i].TimeoutMs = defaultTimeoutMs
		} else if b.Tools[i].TimeoutMs < minTimeoutMs {
			b.Tools[i].TimeoutMs = minTimeoutMs
		} else if b.Tools[i].TimeoutMs > maxTimeoutMs {
			b.Tools[i].TimeoutMs = maxTimeoutMs
		}
	}
}

func (b *Bundle) mergeCommands(global, project []CommandDef) {
	seen := map[string]bool{}
	add := func(c CommandDef, scope string) {
		w, ok := validateCommand(c)
		if !ok {
			b.Warnings = append(b.Warnings, fmt.Sprintf("%s command %q skipped: %s", scope, c.Name, w))
			return
		}
		if seen[c.Name] {
			for i, e := range b.Commands {
				if e.Name == c.Name {
					b.Commands[i] = c
					break
				}
			}
			b.Warnings = append(b.Warnings, fmt.Sprintf("project command %q overrides global", c.Name))
			return
		}
		seen[c.Name] = true
		b.Commands = append(b.Commands, c)
	}
	seenGlobal := map[string]bool{}
	for _, c := range global {
		if seenGlobal[c.Name] {
			b.Warnings = append(b.Warnings, fmt.Sprintf("global command %q skipped: duplicate name (first wins)", c.Name))
			continue
		}
		seenGlobal[c.Name] = true
		add(c, "global")
	}
	seenProject := map[string]bool{}
	for _, c := range project {
		if seenProject[c.Name] {
			b.Warnings = append(b.Warnings, fmt.Sprintf("project command %q skipped: duplicate name (first wins)", c.Name))
			continue
		}
		seenProject[c.Name] = true
		add(c, "project")
	}
	// Normalize after project overrides so every accepted command has the
	// documented timeout bounds, regardless of which file defined it.
	for i := range b.Commands {
		b.Commands[i].TimeoutMs = clampTimeout(b.Commands[i].TimeoutMs)
	}
	b.commandsByName = map[string]*CommandDef{}
	for i := range b.Commands {
		b.commandsByName[b.Commands[i].Name] = &b.Commands[i]
	}
}

func clampTimeout(timeoutMs int) int {
	if timeoutMs == 0 {
		return defaultTimeoutMs
	}
	if timeoutMs < minTimeoutMs {
		return minTimeoutMs
	}
	if timeoutMs > maxTimeoutMs {
		return maxTimeoutMs
	}
	return timeoutMs
}

func (b *Bundle) mergeProfiles(gTools, pTools []ToolDef, global, project []ProfileDef) {
	// Only tools that survived validation count; drop unknown ones.
	validTools := map[string]bool{}
	for k := range builtinTools {
		validTools[k] = true
	}
	for _, t := range b.Tools {
		validTools[t.Name] = true
	}
	seen := map[string]bool{}
	add := func(p ProfileDef, scope string) {
		cp := p
		w, ok := validateProfile(cp, validTools)
		if !ok {
			b.Warnings = append(b.Warnings, fmt.Sprintf("%s profile %q skipped: %s", scope, p.Name, w))
			return
		}
		if strings.TrimSpace(cp.Provider) != "" || strings.TrimSpace(cp.Model) != "" {
			b.Warnings = append(b.Warnings, fmt.Sprintf("%s profile %q: provider/model override is ignored (effort/tools/instructions still apply)", scope, p.Name))
		}
		// Drop unknown tool refs with a warning, keep the profile.
		if len(cp.Tools) > 0 {
			kept := cp.Tools[:0]
			for _, tn := range cp.Tools {
				if !validTools[tn] {
					b.Warnings = append(b.Warnings, fmt.Sprintf("%s profile %q: unknown tool %q skipped", scope, p.Name, tn))
					continue
				}
				kept = append(kept, tn)
			}
			cp.Tools = kept
		}
		if seen[cp.Name] {
			for i, e := range b.Profiles {
				if e.Name == cp.Name {
					b.Profiles[i] = cp
					break
				}
			}
			b.Warnings = append(b.Warnings, fmt.Sprintf("project profile %q overrides global", cp.Name))
			return
		}
		seen[cp.Name] = true
		b.Profiles = append(b.Profiles, cp)
	}
	seenGlobal := map[string]bool{}
	for _, p := range global {
		if seenGlobal[p.Name] {
			b.Warnings = append(b.Warnings, fmt.Sprintf("global profile %q skipped: duplicate name (first wins)", p.Name))
			continue
		}
		seenGlobal[p.Name] = true
		add(p, "global")
	}
	seenProject := map[string]bool{}
	for _, p := range project {
		if seenProject[p.Name] {
			b.Warnings = append(b.Warnings, fmt.Sprintf("project profile %q skipped: duplicate name (first wins)", p.Name))
			continue
		}
		seenProject[p.Name] = true
		add(p, "project")
	}
	b.profilesByName = map[string]*ProfileDef{}
	for i := range b.Profiles {
		b.profilesByName[b.Profiles[i].Name] = &b.Profiles[i]
	}
}

func validateTool(t ToolDef) (string, bool) {
	if t.Name == "" || len(t.Name) > maxNameLen || !toolNameRe.MatchString(t.Name) {
		return "name must match ^[a-z0-9][a-z0-9-]*$, max 32 chars", false
	}
	if builtinTools[t.Name] {
		return fmt.Sprintf("collides with built-in tool %q", t.Name), false
	}
	if strings.TrimSpace(t.Description) == "" {
		return "description is required", false
	}
	if len(t.Parameters) == 0 {
		return "parameters is required", false
	}
	typ, _ := t.Parameters["type"].(string)
	if typ != "object" {
		return "parameters.type must be \"object\"", false
	}
	if strings.TrimSpace(t.Command) == "" {
		return "command is required", false
	}
	allowed := map[string]bool{"cwd": true, "sessionId": true}
	if props, ok := t.Parameters["properties"].(map[string]any); ok {
		for k := range props {
			allowed[k] = true
		}
	} else {
		// parameters.properties may decode differently; accept any declared
		// shape here and rely on render-time missingkey=error.
		allowed["*"] = true
	}
	if refs := templateRefs(t.Command); len(refs) > 0 && !allowed["*"] {
		for _, r := range refs {
			if !allowed[r] {
				return fmt.Sprintf("command references undeclared {{.%s}}", r), false
			}
		}
	}
	if _, err := template.New("tool").Option("missingkey=error").Parse(normalizeArgsVar(t.Command)); err != nil {
		return fmt.Sprintf("command template: %v", err), false
	}
	return "", true
}

func validateCommand(c CommandDef) (string, bool) {
	if !strings.HasPrefix(c.Name, "/") || len(c.Name) > maxNameLen+1 {
		return "name must match ^/[a-z0-9][a-z0-9-]*$, max 32 chars including /", false
	}
	if !toolNameRe.MatchString(strings.TrimPrefix(c.Name, "/")) {
		return "name must match ^/[a-z0-9][a-z0-9-]*$, max 32 chars including /", false
	}
	if builtinCommands[c.Name] {
		return fmt.Sprintf("collides with built-in command %q", c.Name), false
	}
	if strings.TrimSpace(c.Description) == "" {
		return "description is required", false
	}
	hasPrompt := strings.TrimSpace(c.Prompt) != ""
	hasRun := strings.TrimSpace(c.Run) != ""
	if hasPrompt == hasRun {
		return "exactly one of prompt or run must be set", false
	}
	allowed := map[string]bool{"args": true, "cwd": true}
	tmpl := c.Prompt
	if hasRun {
		tmpl = c.Run
	}
	for _, r := range templateRefs(tmpl) {
		if !allowed[r] {
			return fmt.Sprintf("template references unsupported {{.%s}} (commands allow args, cwd)", r), false
		}
	}
	if _, err := template.New("cmd").Option("missingkey=error").Parse(normalizeArgsVar(tmpl)); err != nil {
		return fmt.Sprintf("template: %v", err), false
	}
	return "", true
}

func validateProfile(p ProfileDef, validTools map[string]bool) (string, bool) {
	if p.Name == "" || len(p.Name) > maxNameLen || !toolNameRe.MatchString(p.Name) {
		return "name must match ^[a-z0-9][a-z0-9-]*$, max 32 chars", false
	}
	if strings.TrimSpace(p.Description) == "" {
		return "description is required", false
	}
	if len(p.BashDeny) > maxBashDeny {
		return fmt.Sprintf("bashDeny max %d entries", maxBashDeny), false
	}
	for _, re := range p.BashDeny {
		if len(re) > maxBashDenyLen {
			return "bashDeny entry exceeds 200 chars", false
		}
		if _, err := regexp.Compile(re); err != nil {
			return fmt.Sprintf("bashDeny invalid regex %q: %v", re, err), false
		}
	}
	if len(p.Instructions) > maxInstructions {
		return fmt.Sprintf("instructions exceed %d chars", maxInstructions), false
	}
	if p.Effort != "" {
		if _, err := llm.ParseEffort(p.Effort); err != nil {
			return fmt.Sprintf("invalid effort: %v", err), false
		}
	}
	return "", true
}

// templateRefs returns {{.field}} names in tmpl (after $args normalization).
func templateRefs(tmpl string) []string {
	normalized := normalizeArgsVar(tmpl)
	matches := templateRefRe.FindAllStringSubmatch(normalized, -1)
	var out []string
	for _, m := range matches {
		out = append(out, m[1])
	}
	return out
}

// normalizeArgsVar rewrites {{$args}} (any spacing) to {{.args}} so Go
// templates can render it from a plain map.
func normalizeArgsVar(s string) string {
	return argsVarRe.ReplaceAllString(s, "{{.args}}")
}

// RenderCommand renders a prompt/run template with args + cwd.
func RenderCommand(tmpl, args, cwd string, quoteCwd bool) (string, error) {
	data := map[string]any{"args": args, "cwd": cwd}
	if quoteCwd {
		data["cwd"] = ShellQuoteForCwd(cwd)
	}
	return render(normalizeArgsVar(tmpl), data)
}

func render(normalized string, data map[string]any) (string, error) {
	t, err := template.New("").Option("missingkey=error").Parse(normalized)
	if err != nil {
		return "", err
	}
	var sb strings.Builder
	if err := t.Execute(&sb, data); err != nil {
		return "", err
	}
	return sb.String(), nil
}

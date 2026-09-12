package plugin

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/AlvinPlayz23/myagent/internal/agent"
	"github.com/AlvinPlayz23/myagent/internal/llm"
	"github.com/AlvinPlayz23/myagent/internal/tools"
	"github.com/AlvinPlayz23/myagent/internal/types"
)

// CompileDeny builds a pre-exec deny check from bashDeny regexes (§10.4.4).
// A match returns a Go error (same contract as BashTool: loop.go converts it).
func CompileDeny(patterns []string) (func(command string) error, error) {
	if len(patterns) == 0 {
		return nil, nil
	}
	res := make([]*regexp.Regexp, 0, len(patterns))
	for _, p := range patterns {
		re, err := regexp.Compile(p)
		if err != nil {
			return nil, fmt.Errorf("bashDeny invalid regex %q: %v", p, err)
		}
		res = append(res, re)
	}
	return func(command string) error {
		for i, re := range res {
			if re.MatchString(command) {
				return fmt.Errorf("blocked by profile bashDeny[%d] (%q): command denied", i, patterns[i])
			}
		}
		return nil
	}, nil
}

// WrapDeny installs the deny check on the bash tool and every ShellTool in
// the registry. It returns a new registry (base order preserved); the input
// registry is not mutated.
func WrapDeny(base *tools.Registry, deny func(command string) error) *tools.Registry {
	if deny == nil || base == nil {
		return base
	}
	out := make([]tools.Tool, 0, len(base.All()))
	for _, t := range base.All() {
		switch st := t.(type) {
		case *tools.BashTool:
			out = append(out, &denyBashTool{inner: st, deny: deny})
		case *ShellTool:
			cp := *st
			prev := cp.denyFn
			cp.denyFn = func(cmd string) error {
				if prev != nil {
					if err := prev(cmd); err != nil {
						return err
					}
				}
				return deny(cmd)
			}
			out = append(out, &cp)
		default:
			out = append(out, t)
		}
	}
	return tools.NewRegistry(out...)
}

// denyBashTool wraps *tools.BashTool with a pre-exec deny check on the
// `command` argument.
type denyBashTool struct {
	inner *tools.BashTool
	deny  func(command string) error
}

func (d *denyBashTool) Name() string                { return d.inner.Name() }
func (d *denyBashTool) Description() string         { return d.inner.Description() }
func (d *denyBashTool) Parameters() map[string]any { return d.inner.Parameters() }

func (d *denyBashTool) Execute(ctx context.Context, id string, args map[string]any) (*types.ToolResult, error) {
	if cmd, ok := args["command"].(string); ok {
		if err := d.deny(cmd); err != nil {
			return nil, err
		}
	}
	return d.inner.Execute(ctx, id, args)
}

// AppliedProfile is the result of resolving a profile name against a bundle.
type AppliedProfile struct {
	// Def is the profile definition (nil when name is "" = no profile).
	Def *ProfileDef
	// Registry is base filtered by Def.Tools + deny-wrapped. Nil when no profile.
	Registry *tools.Registry
	// SystemPrompt is the rebuilt prompt (base prompt + instructions).
	SystemPrompt string
	// Effort is the profile effort override, "" when unset/invalid.
	Effort llm.Effort
}

// Apply resolves name against b and builds the filtered registry + prompt.
// basePrompt is the already-built prompt for the full (plugin-merged)
// registry; when a profile is active the prompt is rebuilt from the filtered
// registry and instructions are appended under a "Mode instructions:" heading.
// Unknown name returns an error listing available profiles. Empty name
// returns a zero AppliedProfile (no profile) and no error.
func Apply(b *Bundle, base *tools.Registry, basePrompt, cwd, name string) (AppliedProfile, error) {
	if strings.TrimSpace(name) == "" {
		return AppliedProfile{}, nil
	}
	if b == nil || b.Profile(name) == nil {
		return AppliedProfile{}, fmt.Errorf("unknown profile %q (available: %s)", name, strings.Join(b.ProfileNames(), ", "))
	}
	def := b.Profile(name)
	filtered := FilteredRegistry(base, def.Tools)
	deny, err := CompileDeny(def.BashDeny)
	if err != nil {
		return AppliedProfile{}, err
	}
	filtered = WrapDeny(filtered, deny)
	prompt := agent.BuildSystemPrompt(filtered, cwd)
	if strings.TrimSpace(def.Instructions) != "" {
		prompt += "\n\nMode instructions:\n" + def.Instructions
	}
	var effort llm.Effort
	if strings.TrimSpace(def.Effort) != "" {
		effort, err = llm.ParseEffort(def.Effort)
		if err != nil {
			return AppliedProfile{}, fmt.Errorf("profile %q: %v", name, err)
		}
	}
	_ = basePrompt
	return AppliedProfile{Def: def, Registry: filtered, SystemPrompt: prompt, Effort: effort}, nil
}

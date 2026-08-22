package tools

import (
	"context"
	"fmt"
	"math"

	"github.com/AlvinPlayz23/myagent/internal/types"
)

// Tool is the runtime tool contract. Ported from pi's AgentTool: Execute must
// return an error on failure rather than encoding it in the result content;
// the agent loop converts a returned error into an error tool-result.
type Tool interface {
	Name() string
	Description() string
	// Parameters returns the JSON Schema for the tool arguments.
	Parameters() map[string]any
	// Execute runs the tool. args are the validated/decoded arguments.
	Execute(ctx context.Context, toolCallID string, args map[string]any) (*types.ToolResult, error)
}

// Registry is an ordered, name-indexed set of tools.
type Registry struct {
	order  []string
	byName map[string]Tool
}

// NewRegistry builds a Registry from the given tools, preserving order.
func NewRegistry(tools ...Tool) *Registry {
	r := &Registry{byName: map[string]Tool{}}
	for _, t := range tools {
		r.order = append(r.order, t.Name())
		r.byName[t.Name()] = t
	}
	return r
}

// Get returns the tool with the given name, or nil.
func (r *Registry) Get(name string) Tool {
	return r.byName[name]
}

// All returns the tools in registration order.
func (r *Registry) All() []Tool {
	out := make([]Tool, 0, len(r.order))
	for _, n := range r.order {
		out = append(out, r.byName[n])
	}
	return out
}

// argString returns args[key] as a string, or "" if missing/wrong type.
func argString(args map[string]any, key string) (string, bool) {
	v, ok := args[key]
	if !ok {
		return "", false
	}
	s, ok := v.(string)
	return s, ok
}

// argInt returns args[key] as an int. JSON numbers decode to float64.
func argInt(args map[string]any, key string) (int, bool) {
	v, ok := args[key]
	if !ok {
		return 0, false
	}
	switch n := v.(type) {
	case float64:
		return int(n), true
	case int:
		return n, true
	}
	return 0, false
}

// maxLineArg bounds offset/limit-style numeric arguments: larger than any real
// file, yet small enough that float64 holds it exactly and int arithmetic on
// it cannot overflow.
const maxLineArg = 1 << 30

// argLineInt returns args[key] as a whole number in [min, maxLineArg]. Unlike
// argInt it rejects non-finite values (JSON "1e999" decodes to +Inf, and
// out-of-range float64→int conversion wraps), fractional values, and
// out-of-range magnitudes, so the result is always safe in slice arithmetic.
func argLineInt(args map[string]any, key string, min int) (int, bool, error) {
	v, ok := args[key]
	if !ok {
		return 0, false, nil
	}
	invalid := fmt.Errorf("%s must be a whole number between %d and %d", key, min, maxLineArg)
	f, isFloat := v.(float64)
	if !isFloat {
		n, isInt := v.(int)
		if !isInt || n < min || n > maxLineArg {
			return 0, true, invalid
		}
		return n, true, nil
	}
	if math.IsNaN(f) || math.IsInf(f, 0) || f != math.Trunc(f) || f < float64(min) || f > maxLineArg {
		return 0, true, invalid
	}
	return int(f), true, nil
}

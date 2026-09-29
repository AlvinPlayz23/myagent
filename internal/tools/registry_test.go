package tools

import (
	"context"
	"reflect"
	"testing"

	"github.com/AlvinPlayz23/myagent/internal/types"
)

// stubTool is a minimal Tool for registry tests.
type stubTool struct{ name string }

func (s *stubTool) Name() string               { return s.name }
func (s *stubTool) Description() string        { return "stub " + s.name }
func (s *stubTool) Parameters() map[string]any { return map[string]any{"type": "object"} }
func (s *stubTool) Execute(context.Context, string, map[string]any) (*types.ToolResult, error) {
	return types.TextResult("ok", nil), nil
}

func TestRegistryWithoutPreservesOrderAndDropsNamed(t *testing.T) {
	reg := NewRegistry(&stubTool{"read"}, &stubTool{"write"}, &stubTool{"edit"}, &stubTool{"bash"})

	got := reg.Without([]string{"write", "bash"})
	want := []string{"read", "edit"}
	if !reflect.DeepEqual(got.Names(), want) {
		t.Fatalf("Without names = %v, want %v", got.Names(), want)
	}
	// The original registry must be untouched.
	if n := len(reg.Names()); n != 4 {
		t.Fatalf("original registry len = %d, want 4", n)
	}
}

func TestRegistryWithoutEmptyReturnsSameRegistry(t *testing.T) {
	reg := NewRegistry(&stubTool{"read"})
	if got := reg.Without(nil); got != reg {
		t.Fatalf("Without(nil) returned a new registry; want the same pointer")
	}
	if got := reg.Without([]string{}); got != reg {
		t.Fatalf("Without(empty) returned a new registry; want the same pointer")
	}
}

func TestRegistryWithoutIgnoresUnknownNames(t *testing.T) {
	reg := NewRegistry(&stubTool{"read"}, &stubTool{"bash"})
	got := reg.Without([]string{"nope", "bash", "also-missing"})
	if want := []string{"read"}; !reflect.DeepEqual(got.Names(), want) {
		t.Fatalf("Without names = %v, want %v", got.Names(), want)
	}
}

func TestRegistryNamesIsACopy(t *testing.T) {
	reg := NewRegistry(&stubTool{"read"}, &stubTool{"bash"})
	names := reg.Names()
	names[0] = "mutated"
	if reg.Names()[0] != "read" {
		t.Fatalf("Names() returned an aliased slice; registry was mutated")
	}
}

func TestRegistryAddReplacesAndPreservesOrder(t *testing.T) {
	reg := NewRegistry(&stubTool{"read"}, &stubTool{"bash"})
	reg.Add(&stubTool{"read"})
	if want := []string{"read", "bash"}; !reflect.DeepEqual(reg.Names(), want) {
		t.Fatalf("after replace names = %v, want %v", reg.Names(), want)
	}
	reg.Add(&stubTool{"write"})
	if want := []string{"read", "bash", "write"}; !reflect.DeepEqual(reg.Names(), want) {
		t.Fatalf("after append names = %v, want %v", reg.Names(), want)
	}
}

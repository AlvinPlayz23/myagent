package kit

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestMenuDetailsKeepsTextInOneLineViewport(t *testing.T) {
	got := ansi.Strip(RenderMenuDetails("Useful description", 24, 1))
	if !strings.Contains(got, "Useful description") {
		t.Fatalf("one-line details hide description: %q", got)
	}
}

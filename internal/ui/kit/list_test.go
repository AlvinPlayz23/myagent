package kit

import (
	"strings"
	"testing"
)

func TestListKeepsSelectableRowInOneLineViewport(t *testing.T) {
	list := NewList(ListOpts{Rows: []ListRow{
		{ID: "first", Title: "First"},
		{ID: "selected", Title: "Selected"},
		{ID: "last", Title: "Last"},
	}})
	list.Selected = 1

	got := list.Render(20, 1)
	if !strings.Contains(got, "Selected") {
		t.Fatalf("one-line list hides selected row: %q", got)
	}
	if start, count := list.Window(1); start != 1 || count != 1 {
		t.Fatalf("one-line window = (%d, %d), want (1, 1)", start, count)
	}
}

package tools

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeReadFixture(t *testing.T, content string) (string, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "sample.txt")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir, path
}

// TestReadRejectsInvalidOffsetLimit covers hostile offset/limit values. Before
// validation these crashed the process with a slice-bounds panic (negative
// limit) or wrapped to garbage ints (huge/Inf values via float64 conversion).
func TestReadRejectsInvalidOffsetLimit(t *testing.T) {
	dir, path := writeReadFixture(t, "l1\nl2\nl3\n")
	tool := &ReadTool{Cwd: dir}

	cases := []struct {
		name string
		args map[string]any
	}{
		{"negative limit", map[string]any{"path": path, "limit": float64(-1)}},
		{"zero limit", map[string]any{"path": path, "limit": float64(0)}},
		{"negative offset", map[string]any{"path": path, "offset": float64(-3)}},
		{"zero offset", map[string]any{"path": path, "offset": float64(0)}},
		{"huge offset", map[string]any{"path": path, "offset": 1e19}},
		{"huge limit", map[string]any{"path": path, "limit": 1e19}},
		{"infinite offset", map[string]any{"path": path, "offset": math.Inf(1)}},
		{"fractional limit", map[string]any{"path": path, "limit": 2.5}},
		{"wrong type", map[string]any{"path": path, "limit": "many"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("Execute panicked: %v", r)
				}
			}()
			res, err := tool.Execute(context.Background(), "call-1", tc.args)
			if err == nil {
				t.Fatalf("expected error, got result %+v", res)
			}
		})
	}
}

func TestReadOffsetLimitSlicing(t *testing.T) {
	dir, path := writeReadFixture(t, "one\ntwo\nthree\nfour\nfive\n")
	tool := &ReadTool{Cwd: dir}

	res, err := tool.Execute(context.Background(), "call-1", map[string]any{
		"path": path, "offset": float64(2), "limit": float64(2),
	})
	if err != nil {
		t.Fatal(err)
	}
	text := res.Content[0].Text
	if !strings.HasPrefix(text, "two\nthree") {
		t.Errorf("text = %q, want prefix %q", text, "two\nthree")
	}
	if !strings.Contains(text, "[3 more lines in file. Use offset=4 to continue.]") {
		t.Errorf("text = %q, want continuation hint", text)
	}
}

func TestReadLimitBeyondEOFReturnsRest(t *testing.T) {
	dir, path := writeReadFixture(t, "one\ntwo\nthree\n")
	tool := &ReadTool{Cwd: dir}

	res, err := tool.Execute(context.Background(), "call-1", map[string]any{
		"path": path, "offset": float64(2), "limit": float64(maxLineArg),
	})
	if err != nil {
		t.Fatal(err)
	}
	if text := res.Content[0].Text; !strings.HasPrefix(text, "two\nthree") {
		t.Errorf("text = %q, want the remainder of the file", text)
	}
}

func TestReadOffsetBeyondEOFErrors(t *testing.T) {
	dir, path := writeReadFixture(t, "one\ntwo\n")
	tool := &ReadTool{Cwd: dir}

	if _, err := tool.Execute(context.Background(), "call-1", map[string]any{
		"path": path, "offset": float64(99),
	}); err == nil || !strings.Contains(err.Error(), "beyond end of file") {
		t.Errorf("err = %v, want beyond-end-of-file error", err)
	}
}

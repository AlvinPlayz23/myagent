package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeEditFixture(t *testing.T, content string) (dir, path string) {
	t.Helper()
	dir = t.TempDir()
	path = filepath.Join(dir, "file.txt")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir, path
}

func readEditedFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func runEdit(t *testing.T, dir, path string, edits ...map[string]any) error {
	t.Helper()
	args := map[string]any{"path": path}
	items := make([]any, len(edits))
	for i, e := range edits {
		items[i] = e
	}
	args["edits"] = items
	_, err := (&EditTool{Cwd: dir}).Execute(context.Background(), "call-1", args)
	return err
}

func TestEditExactMatch(t *testing.T) {
	dir, path := writeEditFixture(t, "alpha\nbeta\n")
	err := runEdit(t, dir, path, map[string]any{"oldText": "beta", "newText": "gamma"})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := readEditedFile(t, path), "alpha\ngamma\n"; got != want {
		t.Fatalf("content = %q, want %q", got, want)
	}
}

func TestEditMatchesTrailingWhitespace(t *testing.T) {
	dir, path := writeEditFixture(t, "line one   \nline two  \nline three\n")
	err := runEdit(t, dir, path, map[string]any{"oldText": "line one\nline two\n", "newText": "replaced\n"})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := readEditedFile(t, path), "replaced\nline three\n"; got != want {
		t.Fatalf("content = %q, want %q", got, want)
	}
}

func TestEditPreservesUntouchedLines(t *testing.T) {
	dir, path := writeEditFixture(t, "keep   \nreplace me   \nafter   \n")
	err := runEdit(t, dir, path, map[string]any{"oldText": "replace me\n", "newText": "after\n"})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := readEditedFile(t, path), "keep   \nafter\nafter   \n"; got != want {
		t.Fatalf("content = %q, want %q", got, want)
	}
}

func TestEditPreservesCRLF(t *testing.T) {
	dir, path := writeEditFixture(t, "alpha\r\nbeta\r\n")
	err := runEdit(t, dir, path, map[string]any{"oldText": "alpha\nbeta\n", "newText": "one\ntwo\n"})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := readEditedFile(t, path), "one\r\ntwo\r\n"; got != want {
		t.Fatalf("content = %q, want %q", got, want)
	}
}

func TestEditPreservesBOM(t *testing.T) {
	dir, path := writeEditFixture(t, "\uFEFFhello\n")
	err := runEdit(t, dir, path, map[string]any{"oldText": "hello", "newText": "bye"})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := readEditedFile(t, path), "\uFEFFbye\n"; got != want {
		t.Fatalf("content = %q, want %q", got, want)
	}
}

func TestEditFuzzyUnicode(t *testing.T) {
	dir, path := writeEditFixture(t, "你好，世界\n你好（世界）\n")
	err := runEdit(t, dir, path, map[string]any{"oldText": "你好,世界\n你好(世界)\n", "newText": "你好，pi\n你好(pi)\n"})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := readEditedFile(t, path), "你好，pi\n你好(pi)\n"; got != want {
		t.Fatalf("content = %q, want %q", got, want)
	}
}

func TestEditFuzzyPunctuationAndSpaces(t *testing.T) {
	dir, path := writeEditFixture(t, "console.log(\u2018hello\u2019);\nrange: 1\u20135\nhello\u00A0world\n")
	edits := []map[string]any{
		{"oldText": "console.log('hello');", "newText": "console.log('world');"},
		{"oldText": "range: 1-5", "newText": "range: 10-50"},
		{"oldText": "hello world", "newText": "hello universe"},
	}
	if err := runEdit(t, dir, path, edits...); err != nil {
		t.Fatal(err)
	}
	want := "console.log('world');\nrange: 10-50\nhello universe\n"
	if got := readEditedFile(t, path); got != want {
		t.Fatalf("content = %q, want %q", got, want)
	}
}

func TestEditFuzzyPreservesUntouchedLinesAcrossMultipleEdits(t *testing.T) {
	original := strings.Join([]string{
		"keep before  ",
		"first target  ",
		"first after",
		"keep middle   ",
		"second target  ",
		"second after",
		"keep after  ",
		"",
	}, "\n")
	dir, path := writeEditFixture(t, original)
	err := runEdit(t, dir, path,
		map[string]any{"oldText": "first target\nfirst after", "newText": "FIRST\nFIRST2"},
		map[string]any{"oldText": "second target\nsecond after", "newText": "SECOND\nSECOND2"},
	)
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Join([]string{
		"keep before  ",
		"FIRST",
		"FIRST2",
		"keep middle   ",
		"SECOND",
		"SECOND2",
		"keep after  ",
		"",
	}, "\n")
	if got := readEditedFile(t, path); got != want {
		t.Fatalf("content = %q, want %q", got, want)
	}
}

func TestEditRejectsDuplicateAfterFuzzyNormalization(t *testing.T) {
	dir, path := writeEditFixture(t, "hello world   \nhello world\n")
	err := runEdit(t, dir, path, map[string]any{"oldText": "hello world", "newText": "replaced"})
	if err == nil || !strings.Contains(err.Error(), "Found 2 occurrences") {
		t.Fatalf("err = %v, want duplicate error", err)
	}
}

func TestEditRejectsNotFound(t *testing.T) {
	dir, path := writeEditFixture(t, "completely different content\n")
	err := runEdit(t, dir, path, map[string]any{"oldText": "this does not exist", "newText": "replacement"})
	if err == nil || !strings.Contains(err.Error(), "Could not find the exact text") {
		t.Fatalf("err = %v, want not-found error", err)
	}
}

func TestEditRejectsEmptyOldText(t *testing.T) {
	dir, path := writeEditFixture(t, "content\n")
	err := runEdit(t, dir, path, map[string]any{"oldText": "", "newText": "inserted"})
	if err == nil || !strings.Contains(err.Error(), "oldText must not be empty") {
		t.Fatalf("err = %v, want empty-oldText error", err)
	}
}

func TestEditRejectsNoChange(t *testing.T) {
	dir, path := writeEditFixture(t, "content\n")
	err := runEdit(t, dir, path, map[string]any{"oldText": "content", "newText": "content"})
	if err == nil || !strings.Contains(err.Error(), "No changes made") {
		t.Fatalf("err = %v, want no-change error", err)
	}
}

func TestEditRejectsOverlappingEdits(t *testing.T) {
	dir, path := writeEditFixture(t, "abc\ndef\nghi\n")
	err := runEdit(t, dir, path,
		map[string]any{"oldText": "abc\ndef", "newText": "one"},
		map[string]any{"oldText": "def\nghi", "newText": "two"},
	)
	if err == nil || !strings.Contains(err.Error(), "overlap") {
		t.Fatalf("err = %v, want overlap error", err)
	}
}

func TestEditMatchesAgainstOriginalNotIncrementally(t *testing.T) {
	dir, path := writeEditFixture(t, "a\nb\n")
	err := runEdit(t, dir, path,
		map[string]any{"oldText": "a", "newText": "b"},
		map[string]any{"oldText": "b", "newText": "c"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := readEditedFile(t, path), "b\nc\n"; got != want {
		t.Fatalf("content = %q, want %q", got, want)
	}
}

func TestEditAcceptsMalformedEditArguments(t *testing.T) {
	cases := []struct {
		name string
		args map[string]any
	}{
		{
			name: "edits as JSON string",
			args: map[string]any{"edits": `[{"oldText":"old","newText":"new"}]`},
		},
		{
			name: "single edit object",
			args: map[string]any{"edits": map[string]any{"oldText": "old", "newText": "new"}},
		},
		{
			name: "legacy top-level fields",
			args: map[string]any{"oldText": "old", "newText": "new"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir, path := writeEditFixture(t, "old\n")
			tc.args["path"] = path
			res, err := (&EditTool{Cwd: dir}).Execute(context.Background(), "call-1", tc.args)
			if err != nil {
				t.Fatal(err)
			}
			if res == nil || len(res.Content) == 0 || !strings.Contains(res.Content[0].Text, "Successfully replaced") {
				t.Fatalf("result = %+v, want success text", res)
			}
			if got, want := readEditedFile(t, path), "new\n"; got != want {
				t.Fatalf("content = %q, want %q", got, want)
			}
		})
	}
}

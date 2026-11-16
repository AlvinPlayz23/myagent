package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/AlvinPlayz23/myagent/internal/tui"
	"github.com/AlvinPlayz23/myagent/internal/ui/kit"
)

// fakeBlock is a deterministic KitBlock. The shell only consumes Render/Rev;
// renders/lastWidth exist so tests can prove delegation and width invalidation.
type fakeBlock struct {
	lines     []string
	rev       int
	kind      string
	renders   int
	lastWidth int
}

func (b *fakeBlock) Render(width int) string {
	b.renders++
	b.lastWidth = width
	return strings.Join(b.lines, "\n")
}

func (b *fakeBlock) Rev() int { return b.rev }

func (b *fakeBlock) KitKind() string {
	if b.kind == "" {
		return "assistant"
	}
	return b.kind
}

type fakeClick struct {
	row, col int
	mod      tea.KeyMod
}

type fakePoint struct {
	row, col int
}

// fakeHost implements the narrow tui.KitHost surface. It doubles the negative
// space around the real model so shell tests stay deterministic.
type fakeHost struct {
	welcome     bool
	welcomeText string
	blocks      []*fakeBlock
	transcript  int

	composer      string
	composerH     int
	composerEmpty bool
	composerRev   int

	keys     []string
	clicks   []fakeClick
	motions  []fakePoint
	releases []fakePoint

	overlays int
	sizes    [][2]int
	lastSize int

	tabs      []tui.KitTab
	activeTab string
	barTitle  string
	barRows   []tui.KitStat
	todos     []tui.KitTodo
	footer    tui.KitFooter
	notice    tui.KitNotice

	picker         tui.KitPickerState
	pickerActive   bool
	accepted       string
	cancels        int
	selectedTab    string
	pickerCmd      tea.Cmd
	tabSelectCalls int

	onKey func(tea.KeyPressMsg) tea.Cmd
	cmd   tea.Cmd
}

func (f *fakeHost) KitShowWelcome() bool { return f.welcome }

func (f *fakeHost) KitWelcome(int, int) string { return f.welcomeText }

func (f *fakeHost) KitBlocks() []tui.KitBlock {
	out := make([]tui.KitBlock, len(f.blocks))
	for i, b := range f.blocks {
		out[i] = b
	}
	return out
}

func (f *fakeHost) KitTranscriptRev() int { return f.transcript }

func (f *fakeHost) KitComposer(int) string { return f.composer }

func (f *fakeHost) KitComposerHeight() int { return f.composerH }

func (f *fakeHost) KitComposerEmpty() bool { return f.composerEmpty }

func (f *fakeHost) KitComposerRev() int { return f.composerRev }

func (f *fakeHost) KitKey(ev tea.KeyPressMsg) tea.Cmd {
	f.keys = append(f.keys, ev.Keystroke())
	f.composerRev++
	if f.onKey != nil {
		return f.onKey(ev)
	}
	return f.cmd
}

func (f *fakeHost) KitMouseClick(row, col int, mod tea.KeyMod) tea.Cmd {
	f.clicks = append(f.clicks, fakeClick{row: row, col: col, mod: mod})
	return nil
}

func (f *fakeHost) KitMouseMotion(row, col int, _ tea.MouseButton) tea.Cmd {
	f.motions = append(f.motions, fakePoint{row: row, col: col})
	return nil
}

func (f *fakeHost) KitMouseRelease(row, col int) tea.Cmd {
	f.releases = append(f.releases, fakePoint{row: row, col: col})
	return nil
}

func (f *fakeHost) KitOverlay(content string, _ int) string {
	f.overlays++
	return content
}

// KitSetSize mirrors the model: a width change bumps the transcript revision
// through the legacy invalidate path.
func (f *fakeHost) KitSetSize(w, h int) {
	f.sizes = append(f.sizes, [2]int{w, h})
	if w != f.lastSize {
		f.lastSize = w
		f.transcript++
	}
}

func (f *fakeHost) KitTabs() ([]tui.KitTab, string) {
	return f.tabs, f.activeTab
}

func (f *fakeHost) KitSelectTab(id string) tea.Cmd {
	f.tabSelectCalls++
	f.selectedTab = id
	f.activeTab = id
	return nil
}

func (f *fakeHost) KitSidebar() (string, []tui.KitStat) {
	return f.barTitle, f.barRows
}

func (f *fakeHost) KitTodos() []tui.KitTodo { return f.todos }

func (f *fakeHost) KitFooter() tui.KitFooter { return f.footer }

func (f *fakeHost) KitNotice() tui.KitNotice { return f.notice }

func (f *fakeHost) KitPicker() (tui.KitPickerState, bool) {
	return f.picker, f.pickerActive
}

func (f *fakeHost) KitPickerAccept(id string) tea.Cmd {
	f.accepted = id
	f.pickerActive = false
	return f.pickerCmd
}

func (f *fakeHost) KitPickerCancel() tea.Cmd {
	f.cancels++
	f.pickerActive = false
	return nil
}

func textBlock(lines ...string) *fakeBlock { return &fakeBlock{lines: lines} }

func manyBlocks(n int) []*fakeBlock {
	out := make([]*fakeBlock, n)
	for i := range out {
		out[i] = textBlock("block line a", "block line b")
	}
	return out
}

func newTestShell(host *fakeHost) *Shell {
	s := newShell(host)
	s.Resize(80, 24)
	return s
}

func keyRune(r rune) tea.KeyPressMsg { return tea.KeyPressMsg{Code: r, Text: string(r)} }

func TestShellWelcomeAndTranscriptGoldens(t *testing.T) {
	cases := []struct {
		name    string
		welcome bool
		w, h    int
	}{
		{"s3_welcome_80x24", true, 80, 24},
		{"s3_welcome_120x40", true, 120, 40},
		{"s3_transcript_80x24", false, 80, 24},
		{"s3_transcript_120x40", false, 120, 40},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			host := &fakeHost{
				welcome:     tc.welcome,
				welcomeText: "myagent\n───────\nkit shell · S3 strangler",
				blocks: []*fakeBlock{
					textBlock("user › hello", "show me the kit shell"),
					textBlock("assistant › ready", "scroll · select · compose"),
					textBlock("tool › read GOTUI.md", "section 3.0.D — dialog bands"),
					textBlock("assistant › done", "legacy stays byte-identical"),
				},
				composer:      "> draft prompt\n  enter to send",
				composerH:     2,
				composerEmpty: false,
			}
			s := newShell(host)
			s.Resize(tc.w, tc.h)
			raw := s.Render(tc.w, tc.h)
			assertFrame(t, raw, tc.w, tc.h)
			plain := trimLines(ansi.Strip(raw))

			path := filepath.Join("testdata", tc.name+".txt")
			if os.Getenv("UPDATE_GOLDENS") == "1" {
				if err := os.MkdirAll("testdata", 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(plain+"\n"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read golden: %v (run UPDATE_GOLDENS=1 to create)", err)
			}
			wantStr := strings.TrimSuffix(strings.TrimSuffix(string(want), "\n"), "\r")
			if plain != wantStr {
				t.Fatalf("golden mismatch for %s\n--- got ---\n%s\n--- want ---\n%s", path, plain, wantStr)
			}
		})
	}
}

func TestShellComposerIsFocusTarget(t *testing.T) {
	host := &fakeHost{welcome: true, welcomeText: "welcome", composerH: 1}
	s := newTestShell(host)
	s.Render(80, 24)
	if got := s.root.Focus.FocusedID(); got != "kit.composer" {
		t.Fatalf("focused id = %q, want kit.composer", got)
	}
}

func TestShellKeyRouting(t *testing.T) {
	host := &fakeHost{
		blocks:        manyBlocks(40),
		composer:      "",
		composerH:     2,
		composerEmpty: true,
	}
	s := newTestShell(host)
	s.Render(80, 24)
	maxScroll := s.scroll.MaxScroll()
	if maxScroll != 97 {
		t.Fatalf("maxScroll = %d, want 97", maxScroll)
	}
	if got := s.scroll.ScrollTop(); got != maxScroll {
		t.Fatalf("initial scrollTop = %d, want %d", got, maxScroll)
	}

	// Scroll keys bubble past the composer to the ScrollBox.
	if !s.Key(tea.KeyPressMsg{Code: tea.KeyPgUp}) {
		t.Fatal("pgup not consumed")
	}
	if got, want := s.scroll.ScrollTop(), maxScroll-20; got != want {
		t.Fatalf("pgup scrollTop = %d, want %d", got, want)
	}
	if !s.Key(tea.KeyPressMsg{Code: tea.KeyHome}) {
		t.Fatal("home not consumed with an empty composer")
	}
	if got := s.scroll.ScrollTop(); got != 0 {
		t.Fatalf("home scrollTop = %d, want 0", got)
	}
	if len(host.keys) != 0 {
		t.Fatalf("scroll keys leaked to composer: %v", host.keys)
	}
	if !s.Key(tea.KeyPressMsg{Code: tea.KeyEnd}) || !s.scroll.AtBottom() {
		t.Fatal("end should return to the tail")
	}

	// A non-empty composer takes Home/End; Root routes it to the composer,
	// which forwards to the legacy onKey path.
	host.composerEmpty = false
	if !s.Key(tea.KeyPressMsg{Code: tea.KeyHome}) {
		t.Fatal("home not consumed by the focused composer")
	}
	if got := host.keys; len(got) != 1 || got[0] != "home" {
		t.Fatalf("composer keys = %v, want [home]", got)
	}
	if !s.Key(keyRune('x')) {
		t.Fatal("rune not consumed")
	}
	if got := host.keys; len(got) != 2 || got[1] != "x" {
		t.Fatalf("composer keys = %v, want [home x]", got)
	}
	s.Sync() // model.Update syncs after each event batch
	if !strings.Contains(s.composer.content, "x") {
		// The fake onKey did not mutate the composer; content is host-driven.
		t.Log("composer content unchanged by fake host (expected)")
	}
}

func TestShellKeysStayInDialog(t *testing.T) {
	host := &fakeHost{
		blocks:        manyBlocks(40),
		composerH:     2,
		composerEmpty: true,
	}
	s := newTestShell(host)
	s.Render(80, 24)
	before := s.scroll.ScrollTop()

	s.dialogs.Push(kit.NewDialogLayout(kit.DialogLayoutOpts{
		Title: "Confirm",
		Body:  kit.NewText(kit.TextOpts{Content: "stay here"}),
	}))
	s.Render(80, 24)
	if !s.dialogs.Modal() {
		t.Fatal("dialog should be modal after Push")
	}

	if !s.Key(tea.KeyPressMsg{Code: tea.KeyPgUp}) {
		t.Fatal("modal dialog should consume pgup")
	}
	if got := s.scroll.ScrollTop(); got != before {
		t.Fatalf("modal pgup scrolled transcript to %d, want %d", got, before)
	}
	if len(host.keys) != 0 {
		t.Fatalf("modal keys leaked to composer: %v", host.keys)
	}
	if !s.Key(tea.KeyPressMsg{Code: tea.KeyEsc}) {
		t.Fatal("esc should pop the dialog")
	}
	if s.dialogs.Modal() {
		t.Fatal("dialog still modal after esc")
	}
}

func TestShellMouseRouting(t *testing.T) {
	host := &fakeHost{
		blocks:    manyBlocks(10),
		composerH: 2,
	}
	s := newTestShell(host)
	s.Render(80, 24)
	start := s.scroll.ScrollTop()
	if start == 0 {
		t.Fatal("test needs scrollable content")
	}

	// A click inside a block is translated to transcript coordinates by the
	// block widget itself.
	if !s.Mouse(tea.MouseClickMsg{X: 3, Y: 2, Button: tea.MouseLeft}) {
		t.Fatal("in-block click not consumed")
	}
	if len(host.clicks) != 1 {
		t.Fatalf("clicks = %d, want 1", len(host.clicks))
	}
	got := host.clicks[0]
	// First visible item starts at start; y=2 is inside the first visible block.
	if got.col != 3 || got.row != start+2 {
		t.Fatalf("in-block click = %+v, want row %d col 3", got, start+2)
	}

	// Motion and release inside the block continue the same selection path.
	if !s.Mouse(tea.MouseMotionMsg{X: 5, Y: 3, Button: tea.MouseLeft}) {
		t.Fatal("drag motion not consumed")
	}
	if len(host.motions) != 1 || host.motions[0] != (fakePoint{row: start + 3, col: 5}) {
		t.Fatalf("motion = %+v, want row %d col 5", host.motions, start+3)
	}
	if !s.Mouse(tea.MouseReleaseMsg{X: 5, Y: 3, Button: tea.MouseLeft}) {
		t.Fatal("release not consumed")
	}
	if len(host.releases) != 1 || host.releases[0] != (fakePoint{row: start + 3, col: 5}) {
		t.Fatalf("release = %+v, want row %d col 5", host.releases, start+3)
	}

	// A click on the separator row outside block widgets falls through to the
	// shell's content-coordinate handler.
	host.clicks = nil
	if !s.Mouse(tea.MouseClickMsg{X: 4, Y: 2, Button: tea.MouseLeft}) {
		t.Fatal("gap click not consumed")
	}
	if len(host.clicks) != 1 || host.clicks[0].col != 4 {
		t.Fatalf("gap click = %+v, want col 4", host.clicks)
	}

	// Clicking the composer focuses it and does not start a transcript drag.
	host.clicks = nil
	if !s.Mouse(tea.MouseClickMsg{X: 4, Y: 22, Button: tea.MouseLeft}) {
		t.Fatal("composer click not consumed")
	}
	if len(host.clicks) != 0 {
		t.Fatalf("composer click leaked to transcript: %+v", host.clicks)
	}
	if got := s.root.Focus.FocusedID(); got != "kit.composer" {
		t.Fatalf("focus after composer click = %q", got)
	}

	// Wheel up over the transcript scrolls the ScrollBox.
	if !s.Mouse(tea.MouseWheelMsg{X: 4, Y: 2, Button: tea.MouseWheelUp}) {
		t.Fatal("wheel not consumed")
	}
	if got := s.scroll.ScrollTop(); got != start-1 {
		t.Fatalf("wheel scrollTop = %d, want %d", got, start-1)
	}

	// With a dialog open, lower-band mouse hits are gated. A backdrop click is
	// consumed by the dialog stack and closes it; it must never start a
	// transcript selection underneath.
	s.dialogs.Push(kit.NewDialogLayout(kit.DialogLayoutOpts{
		Title: "Confirm",
		Body:  kit.NewText(kit.TextOpts{Content: "blocked"}),
	}))
	s.Render(80, 24)
	host.clicks = nil
	if !s.Mouse(tea.MouseClickMsg{X: 4, Y: 2, Button: tea.MouseLeft}) {
		t.Fatal("backdrop click should be consumed by the modal stack")
	}
	if s.dialogs.Modal() {
		t.Fatal("backdrop click should close the dialog")
	}
	if len(host.clicks) != 0 {
		t.Fatalf("modal click leaked to transcript: %+v", host.clicks)
	}
}

func TestShellComposeOncePerBatch(t *testing.T) {
	host := &fakeHost{
		blocks:        manyBlocks(4),
		composer:      "> ready",
		composerH:     2,
		composerEmpty: false,
	}
	s := newTestShell(host)
	s.Render(80, 24)
	if got := s.ComposeCount(); got != 1 {
		t.Fatalf("first render composed %d frames, want 1", got)
	}

	// Idle frames are cache hits.
	s.Render(80, 24)
	s.Render(80, 24)
	if got := s.ComposeCount(); got != 1 {
		t.Fatalf("idle renders composed %d frames, want 1", got)
	}

	// A streaming burst mutates several blocks but bumps the revision once.
	for i := 0; i < 5; i++ {
		host.blocks = append(host.blocks, textBlock("streamed", "batch"))
	}
	host.transcript++
	s.Sync()
	s.Render(80, 24)
	if got := s.ComposeCount(); got != 2 {
		t.Fatalf("batch append composed %d frames, want 2 total", got)
	}
	s.Render(80, 24)
	if got := s.ComposeCount(); got != 2 {
		t.Fatalf("idle after append composed %d frames, want 2 total", got)
	}

	// A composer change is one more frame.
	host.composer = "> ready!"
	host.composerRev++
	s.Sync()
	s.Render(80, 24)
	if got := s.ComposeCount(); got != 3 {
		t.Fatalf("composer edit composed %d frames, want 3 total", got)
	}

	// A Sync with no host change must not dirty the frame.
	s.Sync()
	s.Render(80, 24)
	if got := s.ComposeCount(); got != 3 {
		t.Fatalf("no-op sync composed %d frames, want 3 total", got)
	}
}

func TestShellResizeUpdatesTranscriptWidth(t *testing.T) {
	host := &fakeHost{
		blocks:    []*fakeBlock{textBlock("one", "two")},
		composerH: 2,
	}
	s := newTestShell(host)
	s.Render(80, 24)
	if got := host.blocks[0].lastWidth; got != 79 {
		t.Fatalf("block width at 80 = %d, want 79 (scrollbar reserved)", got)
	}

	rev := host.transcript
	s.Resize(60, 15)
	if host.transcript == rev {
		t.Fatal("resize with a width change should invalidate the transcript")
	}
	last := host.sizes[len(host.sizes)-1]
	if last != [2]int{59, 13} {
		t.Fatalf("published size = %v, want [59 13]", last)
	}
	s.Render(60, 15)
	if got := host.blocks[0].lastWidth; got != 59 {
		t.Fatalf("block width at 60 = %d, want 59", got)
	}

	// Welcome mode uses the full width: no scrollbar column is reserved.
	host.welcome = true
	host.welcomeText = "welcome"
	host.transcript++
	s.Sync()
	s.Render(60, 15)
	if got := host.sizes[len(host.sizes)-1]; got != [2]int{60, 13} {
		t.Fatalf("welcome published size = %v, want [60 13]", got)
	}
}

func assertFrame(t *testing.T, frame string, w, h int) {
	t.Helper()
	lines := strings.Split(frame, "\n")
	if len(lines) != h {
		t.Fatalf("frame height = %d, want %d", len(lines), h)
	}
	for i, line := range lines {
		if got := ansi.StringWidth(line); got != w {
			t.Fatalf("line %d width = %d, want %d (%q)", i, got, w, line)
		}
	}
}

func trimLines(s string) string {
	lines := strings.Split(s, "\n")
	for i := range lines {
		lines[i] = strings.TrimRight(lines[i], " ")
	}
	return strings.Join(lines, "\n")
}

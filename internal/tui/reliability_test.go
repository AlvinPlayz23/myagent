package tui

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/AlvinPlayz23/myagent/internal/agent"
	"github.com/AlvinPlayz23/myagent/internal/export"
	"github.com/AlvinPlayz23/myagent/internal/llm"
	modelcatalog "github.com/AlvinPlayz23/myagent/internal/models"
	"github.com/AlvinPlayz23/myagent/internal/tui/engine"
	"github.com/AlvinPlayz23/myagent/internal/types"
)

type scriptedStreamProvider struct {
	events []llm.StreamEvent
}

func (p scriptedStreamProvider) Stream(context.Context, llm.Model, llm.Request) (<-chan llm.StreamEvent, error) {
	stream := make(chan llm.StreamEvent, len(p.events))
	for _, event := range p.events {
		stream <- event
	}
	close(stream)
	return stream, nil
}

type cancelBlockingProvider struct {
	started chan<- struct{}
	stopped chan<- struct{}
}

func (p cancelBlockingProvider) Stream(ctx context.Context, _ llm.Model, _ llm.Request) (<-chan llm.StreamEvent, error) {
	p.started <- struct{}{}
	stream := make(chan llm.StreamEvent)
	go func() {
		<-ctx.Done()
		p.stopped <- struct{}{}
		close(stream)
	}()
	return stream, nil
}

func screenText(scr *engine.Screen) string {
	var b strings.Builder
	for y := 0; y < scr.H; y++ {
		for x := 0; x < scr.W; x++ {
			cell := scr.CellAt(x, y)
			if cell.Ch == 0 {
				b.WriteByte(' ')
			} else {
				b.WriteRune(cell.Ch)
			}
		}
		b.WriteByte('\n')
	}
	return b.String()
}

func TestRunnerStreamsActiveGenerationBeforeCompletion(t *testing.T) {
	partial := &types.Message{Role: types.RoleAssistant}
	completed := &types.Message{
		Role:       types.RoleAssistant,
		Content:    []types.ContentBlock{types.TextBlock("final answer")},
		StopReason: types.StopStop,
	}
	a := newTestApp(t, agent.Config{Provider: scriptedStreamProvider{events: []llm.StreamEvent{
		{Type: "start", Partial: partial},
		{Type: "thinking_start"},
		{Type: "thinking_delta", Delta: "plan"},
		{Type: "thinking_end"},
		{Type: "text_delta", Delta: "final answer"},
		{Type: "done", Message: completed},
	}}}, nil)

	var received []types.AgentEvent
	a.r.onEvent = func(event types.AgentEvent) error {
		if !a.working {
			t.Fatal("stream event arrived after completion")
		}
		received = append(received, event)
		return nil
	}
	a.startRun("question", userMessage("question"))
	if len(received) == 0 {
		t.Fatal("runner did not forward stream events")
	}
	if a.working {
		t.Fatal("completion did not settle the active run")
	}
	text := sbText(a.sb, 60, false, true)
	if !strings.Contains(text, "plan") || !strings.Contains(text, "final answer") {
		t.Fatalf("streamed thinking/text missing after completion:\n%s", text)
	}
}

func TestSynchronousRunnerHandlesStreamsLargerThanEventQueue(t *testing.T) {
	const chunks = 300
	events := make([]llm.StreamEvent, 0, chunks+2)
	events = append(events, llm.StreamEvent{Type: "start", Partial: &types.Message{Role: types.RoleAssistant}})
	for range chunks {
		events = append(events, llm.StreamEvent{Type: "text_delta", Delta: "x"})
	}
	events = append(events, llm.StreamEvent{Type: "done", Message: &types.Message{Role: types.RoleAssistant, StopReason: types.StopStop}})
	a := newTestApp(t, agent.Config{Provider: scriptedStreamProvider{events: events}}, nil)

	a.startRun("question", userMessage("question"))
	if a.working {
		t.Fatal("long synchronous stream did not complete")
	}
	last := a.sb.entries[len(a.sb.entries)-1]
	if last.kind != sbAssistant || len(last.text) != chunks {
		t.Fatalf("streamed text = kind %d length %d, want assistant/%d", last.kind, len(last.text), chunks)
	}
}

func TestDispatchLoopIgnoresStaleRunEvents(t *testing.T) {
	a := newTestApp(t, cfgForTest(), nil)
	a.generation = 2
	a.working = true
	a.sessionTitle = "current title"

	a.dispatchLoop(loopEvent{agent: &agentEventEnvelope{
		generation: 1,
		ev: types.AgentEvent{
			Type: types.EventMessageUpdate,
			AssistantMessageEvent: &types.AssistantMessageEvent{
				Type:  "text_delta",
				Delta: "stale text",
			},
		},
	}})
	a.dispatchLoop(loopEvent{title: &agentTitleEvent{generation: 1, title: "stale title"}})
	a.dispatchLoop(loopEvent{done: &agentDoneEvent{generation: 1}})

	if len(a.sb.entries) != 0 {
		t.Fatalf("stale event changed scrollback: %#v", a.sb.entries)
	}
	if a.sessionTitle != "current title" {
		t.Fatalf("stale title changed session title to %q", a.sessionTitle)
	}
	if !a.working {
		t.Fatal("stale completion settled the active run")
	}
}

func TestLoopQuitsWhenInputCloses(t *testing.T) {
	a := newTestApp(t, cfgForTest(), nil)
	input := make(chan engine.Event)
	close(input)
	a.input = input

	done := make(chan error, 1)
	go func() { done <- a.loop() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("loop did not exit after input EOF")
	}
}

func TestLoopCancelsActiveRunWhenInputCloses(t *testing.T) {
	started := make(chan struct{}, 1)
	stopped := make(chan struct{}, 1)
	a := newTestApp(t, agent.Config{Provider: cancelBlockingProvider{started: started, stopped: stopped}}, nil)
	a.r.synchronous = false
	input := make(chan engine.Event)
	a.input = input
	a.startRun("question", userMessage("question"))

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("runner did not start provider stream")
	}
	close(input)

	done := make(chan error, 1)
	go func() { done <- a.loop() }()
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("input EOF did not cancel active provider stream")
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("loop did not wait for cancelled run completion")
	}
	if a.working || a.cancel != nil {
		t.Fatalf("run state remained active after input EOF: working=%v cancel=%v", a.working, a.cancel != nil)
	}
}

func TestRunnerDropsLateEventsAfterLoopStops(t *testing.T) {
	a := newTestApp(t, cfgForTest(), nil)
	a.r.synchronous = false
	input := make(chan engine.Event)
	close(input)
	a.input = input
	if err := a.loop(); err != nil {
		t.Fatal(err)
	}

	done := make(chan struct{})
	go func() {
		for range cap(a.loopCh) + 1 {
			a.r.send(loopEvent{done: &agentDoneEvent{generation: 1}})
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("late runner events blocked after UI loop stopped")
	}
}

func TestScrollbackPinnedViewportFollowsTail(t *testing.T) {
	s := newScrollback()
	s.addUser("oldest")
	s.addUser("middle")
	s.addUser("newest")

	area := engine.Rect{W: 32, H: 1}
	scr := engine.NewScreen(area.W, area.H)
	total := s.render(scr, area)
	if total <= area.H {
		t.Fatalf("test setup did not overflow the viewport: total=%d", total)
	}
	if !strings.Contains(screenText(scr), "newest") {
		t.Fatalf("pinned viewport did not render the tail:\n%s", screenText(scr))
	}
	maxOffset := total - area.H
	if s.offset != maxOffset {
		t.Fatalf("pinned offset = %d, want tail offset %d", s.offset, maxOffset)
	}

	s.scrollBy(-1, total, area.H)
	if s.pinned || s.offset != maxOffset-1 {
		t.Fatalf("scrolling up from tail = pinned %v offset %d, want false/%d", s.pinned, s.offset, maxOffset-1)
	}

	s.scrollEnd()
	scr = engine.NewScreen(area.W, area.H)
	s.render(scr, area)
	if !strings.Contains(screenText(scr), "newest") {
		t.Fatalf("scroll end did not restore the tail:\n%s", screenText(scr))
	}
}

func TestScrollbackGeometryUsesRenderedRows(t *testing.T) {
	a := newTestApp(t, cfgForTest(), nil)
	a.w, a.h = 32, 20
	a.sb.addUser("first")
	a.sb.addUser("second")

	if got, want := a.sbHeight()+footerHeight+statusHeight+a.composerHeight()+a.dropdownHeight(), a.h; got != want {
		t.Fatalf("layout height = %d, want terminal height %d", got, want)
	}
	area := engine.Rect{W: a.w, H: a.sbHeight()}
	if got, want := a.sb.render(engine.NewScreen(area.W, area.H), area), a.sbTotal(); got != want {
		t.Fatalf("rendered total rows = %d, sbTotal = %d", got, want)
	}
}

func TestScrollbackUsesOneSpacerBetweenRailEntries(t *testing.T) {
	s := newScrollback()
	s.addUser("first")
	s.addUser("second")

	rows := s.layoutRows(32)
	spacers := 0
	for _, row := range rows {
		if row.entry == nil {
			spacers++
		}
	}
	if spacers != sbVpad {
		t.Fatalf("rail entry spacers = %d, want %d", spacers, sbVpad)
	}
}

func TestModelDiscoveryOwnsItsPickerRequest(t *testing.T) {
	a := newTestApp(t, cfgForTest(), nil)
	var persisted []string
	a.rememberDiscoveredModels = func(provider string, ids []string) {
		persisted = append([]string{provider}, ids...)
	}
	a.models.open([]modelcatalog.Model{{Provider: "primary", ID: "catalog"}}, "", "primary")
	a.modalKind = modalModels
	a.modelDiscovery = 8
	a.discovering = "primary"
	a.statusMsg = "Catalog unavailable; discovering models from the provider…"

	a.onDiscovered(&discoveryResult{provider: "primary", request: 8, models: []string{"live"}})
	if a.discovering != "" {
		t.Fatalf("discovery remained active for %q", a.discovering)
	}
	if len(a.models.items) != 2 || a.models.items[1].Ref() != "primary/live" {
		t.Fatalf("discovered models = %#v", a.models.items)
	}
	if a.statusMsg != "Models refreshed from primary." {
		t.Fatalf("success status = %q", a.statusMsg)
	}
	if want := []string{"primary", "live"}; !sameStrings(persisted, want) {
		t.Fatalf("persisted result = %v, want %v", persisted, want)
	}

	a.models.open(nil, "", "secondary")
	a.modelDiscovery = 10
	a.discovering = "secondary"
	a.statusMsg = "Searching secondary"
	a.onDiscovered(&discoveryResult{provider: "primary", request: 8, models: []string{"stale"}})
	if len(a.models.items) != 0 || a.discovering != "secondary" || a.statusMsg != "Searching secondary" {
		t.Fatalf("stale result changed active picker: models=%#v discovering=%q status=%q", a.models.items, a.discovering, a.statusMsg)
	}
	if want := []string{"primary", "live"}; !sameStrings(persisted, want) {
		t.Fatalf("stale result persisted = %v, want %v", persisted, want)
	}

	a.onDiscovered(&discoveryResult{provider: "secondary", request: 10, err: errors.New("offline")})
	if a.discovering != "" || !strings.Contains(a.statusMsg, "Could not discover models from secondary: offline") {
		t.Fatalf("failed discovery state = discovering %q status %q", a.discovering, a.statusMsg)
	}
}

func TestExportPickerIsInitializedAndSafeToSelect(t *testing.T) {
	a := newTestApp(t, cfgForTest(), nil)
	a.startAgent()
	a.exportSession = func(export.Format, string, bool) (string, error) { return "", nil }

	a.runCommand("/export")
	if a.modalKind != modalExportFormat || !a.exportPick.active || len(a.exportPick.items) == 0 {
		t.Fatalf("export picker = kind %d active %v items %#v", a.modalKind, a.exportPick.active, a.exportPick.items)
	}
	a.modalKey(engine.Key{Code: "enter"})
	if a.modalKind != modalExportName || a.exportFormat != export.Markdown {
		t.Fatalf("export selection = kind %d format %q, want name/markdown", a.modalKind, a.exportFormat)
	}
}

func TestExportOverwriteCapturesInputAndClearsState(t *testing.T) {
	a := newTestApp(t, cfgForTest(), nil)
	a.startAgent()
	a.modalKind = modalExportName
	a.modalOverwrite = true
	a.modalInput = "existing.md"
	var calls []bool
	var names []string
	a.exportSession = func(_ export.Format, name string, overwrite bool) (string, error) {
		names = append(names, name)
		calls = append(calls, overwrite)
		return name, nil
	}

	a.handleInput(engine.Event{Paste: &engine.Paste{Text: "-changed"}})
	if a.modalInput != "existing.md" {
		t.Fatalf("paste changed overwrite target to %q", a.modalInput)
	}
	a.handleInput(engine.Event{Key: &engine.Key{Code: "enter"}})
	if len(calls) != 1 || !calls[0] || names[0] != "existing.md" {
		t.Fatalf("overwrite call = names %v flags %v", names, calls)
	}
	if a.modalKind != modalNone || a.modalOverwrite || a.modalInput != "" {
		t.Fatalf("successful overwrite left modal state: kind=%d overwrite=%v input=%q", a.modalKind, a.modalOverwrite, a.modalInput)
	}

	a.modalKind = modalExportName
	a.modalOverwrite = true
	a.modalInput = "existing.md"
	a.handleInput(engine.Event{Key: &engine.Key{Code: "esc"}})
	if a.modalKind != modalNone || a.modalOverwrite || a.exportFormat != "" || a.modalInput != "" {
		t.Fatalf("cancel left export state: kind=%d overwrite=%v format=%q input=%q", a.modalKind, a.modalOverwrite, a.exportFormat, a.modalInput)
	}
}

func sameStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

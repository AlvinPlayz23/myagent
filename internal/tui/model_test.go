package tui

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/AlvinPlayz23/myagent/internal/agent"
	"github.com/AlvinPlayz23/myagent/internal/llm"
	modelcatalog "github.com/AlvinPlayz23/myagent/internal/models"
	"github.com/AlvinPlayz23/myagent/internal/types"
)

// TestContextGauge covers the kj-style context indicator: hidden when the
// window or usage is unknown, graded by occupancy, capped at 100%, driven by
// the last turn's input tokens rather than the cumulative session total, and
// suggesting /compact once usage crosses the warn threshold.
func TestContextGauge(t *testing.T) {
	newGaugeModel := func(window int) *model {
		m := newModel(nil, nil, nil, newTheme(), newMDRenderer(), "test-model", "")
		if window > 0 {
			m.availableModels = func() []modelcatalog.Model {
				return []modelcatalog.Model{{Provider: "p", ID: "test-model", ContextWindow: window}}
			}
			m.modelID = "p/test-model"
		}
		return m
	}

	t.Run("hidden without window", func(t *testing.T) {
		m := newGaugeModel(0)
		m.addUsage(types.Usage{Input: 1000})
		if got := m.contextGauge(); got != "" {
			t.Fatalf("gauge = %q, want empty when window unknown", got)
		}
	})
	t.Run("hidden without usage", func(t *testing.T) {
		m := newGaugeModel(100000)
		if got := m.contextGauge(); got != "" {
			t.Fatalf("gauge = %q, want empty when no usage reported", got)
		}
	})
	t.Run("renders percent", func(t *testing.T) {
		m := newGaugeModel(100000)
		m.addUsage(types.Usage{Input: 42000})
		if got := ansi.Strip(m.contextGauge()); got != "ctx 42%" {
			t.Fatalf("gauge = %q, want %q", got, "ctx 42%")
		}
	})
	t.Run("driven by last turn, not cumulative", func(t *testing.T) {
		m := newGaugeModel(100000)
		m.addUsage(types.Usage{Input: 30000})
		m.addUsage(types.Usage{Input: 10000})
		if got := ansi.Strip(m.contextGauge()); got != "ctx 10%" {
			t.Fatalf("gauge = %q, want %q (last turn wins)", got, "ctx 10%")
		}
	})
	t.Run("grades severity", func(t *testing.T) {
		m := newGaugeModel(100000)
		m.addUsage(types.Usage{Input: 96000})
		if got := m.contextGauge(); !strings.Contains(got, "96%") {
			t.Fatalf("critical gauge missing percent: %q", got)
		}
	})
	t.Run("warn tier suggests compaction", func(t *testing.T) {
		m := newGaugeModel(100000)
		m.addUsage(types.Usage{Input: 80000})
		got := ansi.Strip(m.contextGauge())
		if !strings.Contains(got, "80%") || !strings.Contains(got, "/compact") {
			t.Fatalf("warn gauge = %q, want percent plus /compact hint", got)
		}
	})
	t.Run("caps at 100", func(t *testing.T) {
		m := newGaugeModel(100000)
		m.addUsage(types.Usage{Input: 250000})
		if got := ansi.Strip(m.contextGauge()); got != "ctx 100% — /compact" {
			t.Fatalf("gauge = %q, want %q", got, "ctx 100% — /compact")
		}
	})
}

// The picker's labels and descriptions are written by hand, so it is the one
// place the registered levels cannot be derived from llm.EffortLevels(). Guard
// it so retiring or adding a level cannot silently skip the TUI.
func TestEffortChoicesCoverRegisteredLevels(t *testing.T) {
	if got, want := len(effortChoices), len(llm.EffortLevels())+1; got != want {
		t.Fatalf("effort choices = %d, want %d (registered levels plus Default)", got, want)
	}
	if effortChoices[0].effort != "" {
		t.Errorf("first choice = %q, want the provider default", effortChoices[0].effort)
	}
	for i, level := range llm.EffortLevels() {
		choice := effortChoices[i+1]
		if choice.effort != level {
			t.Errorf("choice %d = %q, want %q (ascending registered order)", i+1, choice.effort, level)
		}
		if choice.label == "" || choice.description == "" {
			t.Errorf("choice %q missing label or description", level)
		}
	}
}

func TestQueuedFollowUpPromotesToTranscriptWhenConsumed(t *testing.T) {
	m := newModel(nil, nil, nil, newTheme(), newMDRenderer(), "model", "")
	m.onResize(50, 20)
	message := userMessage("run the tests after this")
	// Pin the timestamp: queue matching is timestamp-sensitive, so
	// wall-clock creation time must not decide promotion.
	message.Timestamp = 1
	m.queuedFollowUps = []queuedMessage{{display: "run the tests after this", message: message}}
	m.updateLayout()

	if queued := m.renderQueuedFollowUps(); !strings.Contains(queued, "next") {
		t.Fatalf("queued follow-up has no pending label: %q", queued)
	}
	ev := userMessageStartEvent("run the tests after this")
	ev.Message.Timestamp = 1
	m.onAgentEvent(ev)
	if len(m.queuedFollowUps) != 0 {
		t.Fatalf("queued follow-ups = %#v, want empty", m.queuedFollowUps)
	}
	if got := m.transcript.render(50); !strings.Contains(got, "run the tests after this") || strings.Contains(got, "next") {
		t.Fatalf("consumed follow-up was not promoted to transcript: %q", got)
	}
}

func TestEffortPickerKeepsSelectionVisibleOnShortTerminals(t *testing.T) {
	m := newModel(nil, newRunner(agent.Config{}, newMsgQueue(), nil), newMsgQueue(), newTheme(), newMDRenderer(), "model", "")
	m.onResize(80, 12)
	m.effort.active = true
	m.effort.sel = len(effortChoices) - 1

	panel := m.renderEffortPicker()
	if got, want := strings.Count(panel, "\n")+1, m.panelHeight(); got > want {
		t.Fatalf("effort picker rows = %d, want at most %d", got, want)
	}
	if !strings.Contains(panel, "Max") {
		t.Fatalf("effort picker omitted selected option: %q", panel)
	}
}

func TestEffortPickerMarksMyagentDefault(t *testing.T) {
	newPicker := func(current, def llm.Effort) *model {
		q := newMsgQueue()
		r := newRunner(agent.Config{Effort: current}, q, nil)
		m := newModel(nil, r, q, newTheme(), newMDRenderer(), "model", "")
		m.saveDefaultEffort = func(llm.Effort) error { return nil }
		m.defaultEffort = def
		m.onResize(80, 24)
		m.effort.open(current)
		return m
	}

	// Saved default differs from the live effort: both rows marked.
	m := newPicker(llm.EffortHigh, llm.EffortLow)
	panel := m.renderEffortPicker()
	if !strings.Contains(panel, "High") || !strings.Contains(panel, "(current)") {
		t.Fatalf("picker missing live marker: %q", panel)
	}
	lowLine := ""
	for _, line := range strings.Split(panel, "\n") {
		if strings.Contains(line, "Low") {
			lowLine = line
		}
	}
	if !strings.Contains(lowLine, "(myagent default)") || strings.Contains(lowLine, "(current)") {
		t.Fatalf("Low row = %q, want myagent-default marker only", lowLine)
	}

	// Saved default equals the live effort: one row carries both markers.
	m = newPicker(llm.EffortMedium, llm.EffortMedium)
	panel = m.renderEffortPicker()
	if !strings.Contains(panel, "(current) · myagent default") {
		t.Fatalf("picker missing combined marker: %q", panel)
	}
}

func TestFollowUpConsumptionClearsQueuedStatus(t *testing.T) {
	m := newModel(nil, nil, nil, newTheme(), newMDRenderer(), "model", "")
	m.working = true
	m.statusMsg = "Queued follow-up (1 pending)"
	message := userMessage("later")
	message.Timestamp = 1
	m.queuedFollowUps = []queuedMessage{{display: "later", message: message}}

	ev := userMessageStartEvent("later")
	ev.Message.Timestamp = 1
	m.onAgentEvent(ev)
	if m.statusMsg != "" {
		t.Fatalf("status = %q, want empty", m.statusMsg)
	}
	if status := m.statusLine(); !strings.Contains(status, "Working…") || strings.Contains(status, "Queued follow-up") {
		t.Fatalf("running status = %q", status)
	}
}

func TestEnterQueuesFollowUpOutsideTranscriptWhileWorking(t *testing.T) {
	q := newMsgQueue()
	m := newModel(nil, nil, q, newTheme(), newMDRenderer(), "model", "")
	m.working = true
	m.input.SetValue("hi")
	m.onResize(50, 20)

	m.onKey(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	if len(m.queuedFollowUps) != 1 || m.queuedFollowUps[0].display != "hi" {
		t.Fatalf("queued follow-ups = %#v, want hi", m.queuedFollowUps)
	}
	if got := m.transcript.render(50); strings.Contains(got, "hi") {
		t.Fatalf("queued follow-up leaked into transcript: %q", got)
	}
	view := m.View().Content
	if !strings.Contains(view, "next") || !strings.Contains(view, "hi") {
		t.Fatalf("queued follow-up is not attached to composer: %q", view)
	}
}

func TestCtrlEnterInsertsNewlineInsteadOfSubmitting(t *testing.T) {
	m := newModel(nil, nil, newMsgQueue(), newTheme(), newMDRenderer(), "model", "")
	m.input.SetValue("first line")

	m.onKey(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter, Mod: tea.ModCtrl}))

	if got := m.input.Value(); got != "first line\n" {
		t.Fatalf("input = %q, want newline inserted", got)
	}
	if len(m.queuedFollowUps) != 0 {
		t.Fatalf("ctrl+enter queued follow-ups = %#v, want none", m.queuedFollowUps)
	}
}

func TestCtrlJInsertsNewlineInsteadOfSubmitting(t *testing.T) {
	m := newModel(nil, nil, newMsgQueue(), newTheme(), newMDRenderer(), "model", "")
	m.input.SetValue("first line")

	m.onKey(tea.KeyPressMsg(tea.Key{Code: 'j', Mod: tea.ModCtrl}))

	if got := m.input.Value(); got != "first line\n" {
		t.Fatalf("input = %q, want newline inserted", got)
	}
	if len(m.queuedFollowUps) != 0 {
		t.Fatalf("ctrl+j queued follow-ups = %#v, want none", m.queuedFollowUps)
	}
}

func TestAltEnterSteersWhileWorking(t *testing.T) {
	q := newMsgQueue()
	m := newModel(nil, nil, q, newTheme(), newMDRenderer(), "model", "")
	m.working = true
	m.input.SetValue("change direction")
	m.onResize(50, 20)

	m.onKey(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter, Mod: tea.ModAlt}))
	if len(m.queuedFollowUps) != 0 || len(m.queuedSteering) != 1 {
		t.Fatalf("follow-ups=%#v steering=%#v", m.queuedFollowUps, m.queuedSteering)
	}
	if got := m.transcript.render(50); !strings.Contains(got, "change direction") {
		t.Fatalf("steering missing from transcript: %q", got)
	}
}

func TestQueuedFollowUpsPromoteInFIFOOrder(t *testing.T) {
	m := newModel(nil, nil, nil, newTheme(), newMDRenderer(), "model", "")
	m.onResize(50, 20)
	first := userMessage("first")
	second := userMessage("second")
	// Pin timestamps: queue matching is timestamp-sensitive, so wall-clock
	// creation time must not decide promotion.
	first.Timestamp, second.Timestamp = 1, 1
	m.queuedFollowUps = []queuedMessage{{display: "first", message: first}, {display: "second", message: second}}
	m.updateLayout()

	ev := userMessageStartEvent("first")
	ev.Message.Timestamp = 1
	m.onAgentEvent(ev)
	if len(m.queuedFollowUps) != 1 || m.queuedFollowUps[0].display != "second" {
		t.Fatalf("queued follow-ups after first promotion = %#v", m.queuedFollowUps)
	}
	if queued := m.renderQueuedFollowUps(); strings.Contains(queued, "first") || !strings.Contains(queued, "second") {
		t.Fatalf("unexpected queued cards after first promotion: %q", queued)
	}
	if transcript := m.transcript.render(50); !strings.Contains(transcript, "first") || strings.Contains(transcript, "second") {
		t.Fatalf("unexpected transcript after first promotion: %q", transcript)
	}
}

func TestViewFitsTerminalWithQueuedFollowUp(t *testing.T) {
	m := newModel(nil, nil, nil, newTheme(), newMDRenderer(), "model", "")
	m.queuedFollowUps = []queuedMessage{{display: "run the tests after this", message: userMessage("run the tests after this")}}
	m.onResize(80, 16)

	view := m.View()
	if got := strings.Count(view.Content, "\n") + 1; got > m.height {
		t.Fatalf("view height with queued follow-up = %d, terminal height = %d", got, m.height)
	}
	if !strings.Contains(view.Content, "run the tests after this") {
		t.Fatalf("view does not contain queued follow-up: %q", view.Content)
	}
}

func TestStatusLineOffersWayBackDown(t *testing.T) {
	m := newModel(nil, nil, nil, newTheme(), newMDRenderer(), "model", "")
	m.onResize(80, 24)
	m.hasSessionTitle = true
	for i := 0; i < 40; i++ {
		m.transcript.addUser(strings.Repeat("scrollback line ", 10))
	}
	m.refreshViewport()

	// Pinned to the bottom, the status line stays quiet.
	if got := m.statusLine(); got != "" {
		t.Fatalf("status at bottom = %q, want empty", got)
	}

	// Scrolled up, it advertises the way back down.
	m.viewport.ScrollUp(m.viewport.Height() / 2)
	got := ansi.Strip(m.statusLine())
	if !strings.Contains(got, "ctrl+g") {
		t.Fatalf("status while scrolled up = %q, want the ctrl+g hint", got)
	}

	// Higher-priority lines still win: an in-flight run and status
	// messages take precedence over the scroll hint.
	m.working = true
	m.startedAt = time.Now()
	if got := ansi.Strip(m.statusLine()); strings.Contains(got, "ctrl+g") {
		t.Fatalf("working status = %q, want the spinner line, not the scroll hint", got)
	}
	m.working = false
	m.statusMsg = "hello"
	if got := ansi.Strip(m.statusLine()); strings.Contains(got, "ctrl+g") || !strings.Contains(got, "hello") {
		t.Fatalf("status message = %q, want the message, not the scroll hint", got)
	}
}

func TestSteeringEventDoesNotRemoveQueuedFollowUp(t *testing.T) {
	m := newModel(nil, nil, nil, newTheme(), newMDRenderer(), "model", "")
	steer := userMessage("steer")
	steer.Timestamp = 1
	m.queuedSteering = []types.Message{steer}
	m.queuedFollowUps = []queuedMessage{{display: "later", message: userMessage("later")}}

	ev := userMessageStartEvent("steer")
	ev.Message.Timestamp = 1
	m.onAgentEvent(ev)
	if len(m.queuedFollowUps) != 1 || m.queuedFollowUps[0].display != "later" {
		t.Fatalf("steering removed queued follow-up: %#v", m.queuedFollowUps)
	}
}

func TestInitialPromptEventDoesNotRemoveQueuedFollowUp(t *testing.T) {
	m := newModel(nil, nil, nil, newTheme(), newMDRenderer(), "model", "")
	initial := userMessage("initial")
	initial.Timestamp = 1
	m.activePrompt = &initial
	m.queuedFollowUps = []queuedMessage{{display: "later", message: userMessage("later")}}

	ev := userMessageStartEvent("initial")
	ev.Message.Timestamp = 1
	m.onAgentEvent(ev)
	if m.activePrompt != nil {
		t.Fatal("initial prompt remained active after its event")
	}
	if len(m.queuedFollowUps) != 1 {
		t.Fatalf("initial prompt removed queued follow-up: %#v", m.queuedFollowUps)
	}
}

func TestSubmissionIsRejectedWhileAborting(t *testing.T) {
	m := newModel(nil, nil, nil, newTheme(), newMDRenderer(), "model", "")
	m.working = true
	m.abortRequested = true
	m.input.SetValue("do not queue")

	m.submit(submitFollowUp)
	if len(m.queuedFollowUps) != 0 {
		t.Fatalf("queued during abort: %#v", m.queuedFollowUps)
	}
	if m.input.Value() != "do not queue" {
		t.Fatalf("composer was cleared during abort: %q", m.input.Value())
	}
	if m.statusMsg != "Wait for the current run to finish aborting." {
		t.Fatalf("status = %q", m.statusMsg)
	}
}

func userMessageStartEvent(text string) types.AgentEvent {
	message := userMessage(text)
	return types.AgentEvent{Type: types.EventMessageStart, Message: &message}
}

func TestRefreshViewportFollowsOnlyWhenAtBottom(t *testing.T) {
	m := newModel(nil, nil, nil, newTheme(), newMDRenderer(), "model", "")
	m.onResize(80, 24)
	m.hasSessionTitle = true
	for i := 0; i < 40; i++ {
		m.transcript.addUser(strings.Repeat("scrollback line ", 10))
	}
	m.working = true
	m.refreshViewport()
	if !m.viewport.AtBottom() {
		t.Fatalf("expected to start pinned to bottom, offset = %d", m.viewport.YOffset())
	}
	pinnedOffset := m.viewport.YOffset()
	if pinnedOffset == 0 {
		t.Fatal("test setup did not overflow the viewport; cannot prove follow-mode")
	}

	// Inspecting history mid-run must survive a refresh: new output arrives
	// while the user is scrolled up.
	m.viewport.ScrollUp(m.viewport.Height() / 2)
	scrolledOffset := m.viewport.YOffset()
	if scrolledOffset >= pinnedOffset {
		t.Fatalf("scroll up offset = %d, want less than %d", scrolledOffset, pinnedOffset)
	}
	m.transcript.addUser("new output while scrolled up")
	m.refreshViewport()
	if m.viewport.YOffset() != scrolledOffset {
		t.Fatalf("refresh while scrolled up moved offset to %d, want %d", m.viewport.YOffset(), scrolledOffset)
	}

	// Back at the bottom, new output is followed again.
	m.viewport.GotoBottom()
	bottomOffset := m.viewport.YOffset()
	m.transcript.addUser("more output at bottom")
	m.refreshViewport()
	if !m.viewport.AtBottom() {
		t.Fatalf("refresh at bottom did not follow, offset = %d", m.viewport.YOffset())
	}
	if m.viewport.YOffset() < bottomOffset {
		t.Fatalf("follow-mode moved backwards: offset = %d, was %d", m.viewport.YOffset(), bottomOffset)
	}
}

func TestEmptyHomeJumpsToTopFromBottom(t *testing.T) {
	m := newModel(nil, nil, nil, newTheme(), newMDRenderer(), "model", "")
	m.onResize(80, 24)
	m.hasSessionTitle = true
	for i := 0; i < 40; i++ {
		m.transcript.addUser(strings.Repeat("scrollback line ", 10))
	}
	m.refreshViewport()
	if !m.viewport.AtBottom() {
		t.Fatalf("expected to start pinned to bottom, offset = %d", m.viewport.YOffset())
	}
	if m.viewport.YOffset() == 0 {
		t.Fatal("test setup did not overflow the viewport; cannot prove Home")
	}

	m.onKey(tea.KeyPressMsg(tea.Key{Code: tea.KeyHome}))
	if m.viewport.YOffset() != 0 {
		t.Fatalf("empty Home from bottom offset = %d, want 0", m.viewport.YOffset())
	}

	m.onKey(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnd}))
	if !m.viewport.AtBottom() {
		t.Fatalf("empty End from top did not re-pin, offset = %d", m.viewport.YOffset())
	}
}

func TestCtrlGJumpsToBottomWhileTyping(t *testing.T) {
	m := newModel(nil, nil, nil, newTheme(), newMDRenderer(), "model", "")
	m.onResize(80, 24)
	m.hasSessionTitle = true
	for i := 0; i < 40; i++ {
		m.transcript.addUser(strings.Repeat("scrollback line ", 10))
	}
	m.working = true
	m.input.SetValue("half-typed prompt")
	m.refreshViewport()
	if !m.viewport.AtBottom() {
		t.Fatalf("expected to start pinned to bottom, offset = %d", m.viewport.YOffset())
	}
	pinnedOffset := m.viewport.YOffset()
	if pinnedOffset == 0 {
		t.Fatal("test setup did not overflow the viewport; cannot prove ctrl+g")
	}

	m.viewport.ScrollUp(m.viewport.Height() / 2)
	if m.viewport.AtBottom() {
		t.Fatal("scroll up did not leave the bottom")
	}

	m.onKey(tea.KeyPressMsg(tea.Key{Code: 'g', Mod: tea.ModCtrl}))
	if !m.viewport.AtBottom() {
		t.Fatalf("ctrl+g offset = %d, want pinned to bottom", m.viewport.YOffset())
	}
	if got := m.input.Value(); got != "half-typed prompt" {
		t.Fatalf("ctrl+g disturbed the composer: %q", got)
	}

	// Re-pinning resumes follow-mode: new output stays visible.
	m.transcript.addUser("more output after ctrl+g")
	m.refreshViewport()
	if !m.viewport.AtBottom() {
		t.Fatalf("follow-mode did not resume after ctrl+g, offset = %d", m.viewport.YOffset())
	}
}

func TestTranscriptScrollsWithMouseWheel(t *testing.T) {
	m := newModel(nil, nil, nil, newTheme(), newMDRenderer(), "model", "")
	m.onResize(80, 12)
	m.viewport.SetContent(strings.Repeat("line\n", m.viewport.Height()*2))
	m.viewport.GotoBottom()

	initialOffset := m.viewport.YOffset()
	m.onMouseWheel(tea.MouseWheelMsg{Y: 0, Button: tea.MouseWheelUp})
	if m.viewport.YOffset() >= initialOffset {
		t.Fatalf("wheel up offset = %d, want less than %d", m.viewport.YOffset(), initialOffset)
	}

	scrolledOffset := m.viewport.YOffset()
	m.onMouseWheel(tea.MouseWheelMsg{Y: m.viewport.Height(), Button: tea.MouseWheelDown})
	if m.viewport.YOffset() != scrolledOffset {
		t.Fatalf("wheel outside transcript offset = %d, want %d", m.viewport.YOffset(), scrolledOffset)
	}
}

func TestTranscriptDragCopiesDisplayedText(t *testing.T) {
	m := newModel(nil, nil, nil, newTheme(), newMDRenderer(), "model", "")
	m.hasSessionTitle = true
	m.transcript.addUser("hello world")
	m.onResize(40, 20)
	var copied string
	m.clipboardWrite = func(text string) error {
		copied = text
		return nil
	}

	// User blocks carry a left accent bar plus padding, so text starts at
	// column 2 rather than column 1.
	m.onMouseClick(tea.MouseClickMsg{X: 2, Y: 0, Button: tea.MouseLeft})
	m.onMouseMotion(tea.MouseMotionMsg{X: 6, Y: 0, Button: tea.MouseLeft})
	m.onMouseRelease(tea.MouseReleaseMsg{X: 6, Y: 0, Button: tea.MouseLeft})

	if copied != "hello" {
		t.Fatalf("clipboard = %q, want %q", copied, "hello")
	}
	if m.selection != nil {
		t.Fatal("selection remained active after mouse release")
	}
	if m.statusMsg != "Copied 5 characters." {
		t.Fatalf("status = %q", m.statusMsg)
	}
}

func TestTranscriptClickWithoutDragDoesNotCopy(t *testing.T) {
	m := newModel(nil, nil, nil, newTheme(), newMDRenderer(), "model", "")
	m.hasSessionTitle = true
	m.transcript.addUser("hello")
	m.onResize(40, 20)
	calls := 0
	m.clipboardWrite = func(string) error {
		calls++
		return nil
	}

	m.onMouseClick(tea.MouseClickMsg{X: 1, Y: 0, Button: tea.MouseLeft})
	m.onMouseRelease(tea.MouseReleaseMsg{X: 1, Y: 0, Button: tea.MouseLeft})

	if calls != 0 {
		t.Fatalf("clipboard writes = %d, want 0", calls)
	}
}

func TestTranscriptCopyFailureIsReported(t *testing.T) {
	m := newModel(nil, nil, nil, newTheme(), newMDRenderer(), "model", "")
	m.hasSessionTitle = true
	m.transcript.addUser("hello")
	m.onResize(40, 20)
	m.clipboardWrite = func(string) error { return fmt.Errorf("clipboard unavailable") }

	m.onMouseClick(tea.MouseClickMsg{X: 1, Y: 0, Button: tea.MouseLeft})
	m.onMouseMotion(tea.MouseMotionMsg{X: 5, Y: 0, Button: tea.MouseLeft})
	m.onMouseRelease(tea.MouseReleaseMsg{X: 5, Y: 0, Button: tea.MouseLeft})

	if m.statusMsg != "Could not copy selection: clipboard unavailable" {
		t.Fatalf("status = %q", m.statusMsg)
	}
}

func TestTranscriptPointIncludesViewportOffset(t *testing.T) {
	m := newModel(nil, nil, nil, newTheme(), newMDRenderer(), "model", "")
	m.onResize(20, 12)
	m.viewport.SetContent(strings.Repeat("line\n", m.viewport.Height()*2))
	m.viewport.GotoBottom()

	point, ok := m.transcriptPoint(3, 1)
	if !ok {
		t.Fatal("transcript point was rejected")
	}
	if point.row != m.viewport.YOffset()+1 || point.col != m.viewport.XOffset()+3 {
		t.Fatalf("point = %#v, offsets = (%d,%d)", point, m.viewport.XOffset(), m.viewport.YOffset())
	}
}

func TestViewFitsTerminalHeight(t *testing.T) {
	m := newModel(nil, nil, nil, newTheme(), newMDRenderer(), "model", "")
	m.onResize(80, 12)

	view := m.View()
	if !view.AltScreen {
		t.Fatal("view should use the alternate screen")
	}
	if got := strings.Count(view.Content, "\n") + 1; got > m.height {
		t.Fatalf("view height = %d, terminal height = %d", got, m.height)
	}
}

func TestWelcomeShownForEmptySession(t *testing.T) {
	m := newModel(nil, nil, nil, newTheme(), newMDRenderer(), "model", "")
	m.onResize(80, 20)

	if content := m.viewport.View(); !strings.Contains(content, "myagent") || !strings.Contains(content, "Type a prompt to begin") {
		t.Fatalf("empty-session viewport does not contain welcome: %q", content)
	}
}

func TestWelcomeHiddenAfterFirstPrompt(t *testing.T) {
	q := newMsgQueue()
	r := newRunner(agent.Config{}, q, nil)
	m := newModel(context.Background(), r, q, newTheme(), newMDRenderer(), "model", "")
	m.onResize(80, 20)
	m.input.SetValue("hello")
	m.submit(submitFollowUp)

	content := m.viewport.View()
	if strings.Contains(content, "Your terminal coding agent") {
		t.Fatalf("welcome remained after prompt submission: %q", content)
	}
	if !strings.Contains(content, "hello") {
		t.Fatalf("submitted prompt missing from viewport: %q", content)
	}
}

func TestWelcomeDoesNotReturnAfterClearingEstablishedConversation(t *testing.T) {
	m := newModel(nil, nil, nil, newTheme(), newMDRenderer(), "model", "")
	m.onResize(80, 20)
	m.hasSessionTitle = true
	m.transcript.addUser("prior prompt")
	m.refreshViewport()
	m.transcript.clear()
	m.refreshViewport()

	if content := m.viewport.View(); strings.Contains(content, "Your terminal coding agent") {
		t.Fatalf("welcome returned after clearing an established conversation: %q", content)
	}
}

func TestWelcomeUsesCompactCopyInNarrowTerminal(t *testing.T) {
	m := newModel(nil, nil, nil, newTheme(), newMDRenderer(), "model", "")
	m.onResize(30, 20)

	content := m.viewport.View()
	if !strings.Contains(content, "Type a prompt to begin") {
		t.Fatalf("narrow welcome missing compact hint: %q", content)
	}
	if strings.Contains(content, "/help for commands") {
		t.Fatalf("narrow welcome retained wide hint: %q", content)
	}
}

func TestOrbWelcomeAnimatesWhileSessionIsEmpty(t *testing.T) {
	m := newModel(nil, nil, nil, newTheme(), newMDRenderer(), "model", "")
	m.welcomeStyle = welcomeOrb
	m.onResize(80, 24)
	first := m.viewport.View()

	for range 8 {
		m.Update(tickMsg{})
	}
	second := m.viewport.View()
	if first == second {
		t.Fatal("orb welcome did not change across animation ticks")
	}
	if !strings.Contains(second, "myagent") || !strings.Contains(second, "●") {
		t.Fatalf("animated orb welcome is incomplete: %q", second)
	}
}

func TestTypingSlashOpensCommandPicker(t *testing.T) {
	m := newModel(nil, nil, nil, newTheme(), newMDRenderer(), "model", "")
	m.onResize(80, 20)
	m.onKey(tea.KeyPressMsg(tea.Key{Text: "/", Code: '/'}))

	if got := m.input.Value(); got != "/" {
		t.Fatalf("input = %q, want slash", got)
	}
	if !m.picker.active || len(m.picker.matched) != len(commandItems) {
		t.Fatalf("picker = active %v, matches %d; want all commands", m.picker.active, len(m.picker.matched))
	}
}

func TestCommandPickerFitsTerminalAndBorrowsViewportRows(t *testing.T) {
	m := newModel(nil, nil, nil, newTheme(), newMDRenderer(), "model", "")
	m.onResize(80, 12)
	baseHeight := m.viewport.Height()
	m.input.SetValue("/")
	m.picker.sync(m.input.Value())
	m.updateLayout()

	if !m.picker.active {
		t.Fatal("picker did not open")
	}
	if m.viewport.Height() >= baseHeight {
		t.Fatalf("viewport height = %d, want less than %d", m.viewport.Height(), baseHeight)
	}
	view := m.View()
	if got := strings.Count(view.Content, "\n") + 1; got > m.height {
		t.Fatalf("view height with picker = %d, terminal height = %d", got, m.height)
	}
	if !strings.Contains(view.Content, "/help") || !strings.Contains(view.Content, "/models [provider/model-id]") {
		t.Fatal("picker view does not contain command choices")
	}

	m.picker.dismiss(m.input.Value())
	m.updateLayout()
	if m.viewport.Height() != baseHeight {
		t.Fatalf("viewport height after dismiss = %d, want %d", m.viewport.Height(), baseHeight)
	}
}

func TestStartupStatusClearsAfterFiveSeconds(t *testing.T) {
	m := newModel(nil, nil, nil, newTheme(), newMDRenderer(), "model", "")
	m.statusMsg = "Loaded AGENTS.md"
	m.Update(clearStatusMsg{status: "Loaded AGENTS.md"})
	if m.statusMsg != "" {
		t.Fatalf("status = %q, want empty", m.statusMsg)
	}
}

func TestStartupStatusDoesNotClearNewerStatus(t *testing.T) {
	m := newModel(nil, nil, nil, newTheme(), newMDRenderer(), "model", "")
	m.statusMsg = "Model set to test/model."
	m.Update(clearStatusMsg{status: "Loaded AGENTS.md"})
	if m.statusMsg != "Model set to test/model." {
		t.Fatalf("status = %q, want newer status", m.statusMsg)
	}
}

func TestPromptHistoryNavigatesFromNewestToOldest(t *testing.T) {
	m := newModel(nil, nil, nil, newTheme(), newMDRenderer(), "model", "")
	m.addPromptHistory("first prompt")
	m.addPromptHistory("second prompt")

	m.onKey(tea.KeyPressMsg(tea.Key{Code: tea.KeyUp}))
	if got := m.input.Value(); got != "second prompt" {
		t.Fatalf("first up = %q, want newest prompt", got)
	}
	m.onKey(tea.KeyPressMsg(tea.Key{Code: tea.KeyUp}))
	if got := m.input.Value(); got != "first prompt" {
		t.Fatalf("second up = %q, want oldest prompt", got)
	}
	m.onKey(tea.KeyPressMsg(tea.Key{Code: tea.KeyUp}))
	if got := m.input.Value(); got != "first prompt" {
		t.Fatalf("up at oldest = %q, want oldest prompt", got)
	}
}

func TestPromptHistoryDownReturnsToEmptyComposer(t *testing.T) {
	m := newModel(nil, nil, nil, newTheme(), newMDRenderer(), "model", "")
	m.addPromptHistory("first prompt")
	m.addPromptHistory("second prompt")
	m.navigatePromptHistory(-1)
	m.navigatePromptHistory(-1)

	m.onKey(tea.KeyPressMsg(tea.Key{Code: tea.KeyDown}))
	if got := m.input.Value(); got != "second prompt" {
		t.Fatalf("first down = %q, want newer prompt", got)
	}
	m.onKey(tea.KeyPressMsg(tea.Key{Code: tea.KeyDown}))
	if got := m.input.Value(); got != "" {
		t.Fatalf("down after newest = %q, want empty composer", got)
	}
	if m.historyIndex != -1 {
		t.Fatalf("history index = %d, want -1", m.historyIndex)
	}
}

func TestPromptHistoryExcludesSlashCommandsAndConsecutiveDuplicates(t *testing.T) {
	m := newModel(nil, nil, nil, newTheme(), newMDRenderer(), "model", "")
	m.addPromptHistory("prompt")
	m.addPromptHistory("prompt")
	if len(m.promptHistory) != 1 {
		t.Fatalf("history length = %d, want 1", len(m.promptHistory))
	}

	m.input.SetValue("/help")
	m.submit(submitFollowUp)
	if len(m.promptHistory) != 1 {
		t.Fatalf("slash command was added to history: %#v", m.promptHistory)
	}
}

func TestEditingRecalledPromptExitsHistoryNavigation(t *testing.T) {
	m := newModel(nil, nil, nil, newTheme(), newMDRenderer(), "model", "")
	m.addPromptHistory("prompt")
	m.navigatePromptHistory(-1)
	m.onKey(tea.KeyPressMsg(tea.Key{Text: "x", Code: 'x'}))
	if m.historyIndex != -1 {
		t.Fatalf("history index = %d, want -1 after editing", m.historyIndex)
	}
}

func TestPromptHistoryKeepsMostRecentHundredPrompts(t *testing.T) {
	m := newModel(nil, nil, nil, newTheme(), newMDRenderer(), "model", "")
	for i := 0; i <= promptHistoryLimit; i++ {
		m.addPromptHistory(fmt.Sprintf("prompt %d", i))
	}
	if len(m.promptHistory) != promptHistoryLimit {
		t.Fatalf("history length = %d, want %d", len(m.promptHistory), promptHistoryLimit)
	}
	if m.promptHistory[0] != "prompt 100" || m.promptHistory[len(m.promptHistory)-1] != "prompt 1" {
		t.Fatalf("unexpected retained history: newest=%q oldest=%q", m.promptHistory[0], m.promptHistory[len(m.promptHistory)-1])
	}
}

func TestCtrlVAttachesClipboardImage(t *testing.T) {
	m := newModel(nil, nil, newMsgQueue(), newTheme(), newMDRenderer(), "model", "")
	m.onResize(60, 20)
	png := append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 32)...)
	m.clipboardRead = func() (clipboardPayload, error) {
		return clipboardPayload{image: png}, nil
	}

	_, cmd := m.onKey(tea.KeyPressMsg(tea.Key{Code: 'v', Mod: tea.ModCtrl}))
	if cmd == nil || !m.clipboardBusy {
		t.Fatal("ctrl+v did not start an asynchronous clipboard read")
	}
	m.Update(cmd())

	if m.clipboardBusy || m.attachments.len() != 1 {
		t.Fatalf("clipboardBusy=%v attachments=%d", m.clipboardBusy, m.attachments.len())
	}
	if view := m.View().Content; !strings.Contains(view, "[image]") || !strings.Contains(view, "1 attached") {
		t.Fatalf("attachment component missing from view: %q", view)
	}
}

func TestCtrlVFallsBackToClipboardText(t *testing.T) {
	m := newModel(nil, nil, newMsgQueue(), newTheme(), newMDRenderer(), "model", "")
	m.clipboardRead = func() (clipboardPayload, error) {
		return clipboardPayload{text: "pasted text"}, nil
	}

	_, cmd := m.onKey(tea.KeyPressMsg(tea.Key{Code: 'v', Mod: tea.ModCtrl}))
	m.Update(cmd())
	if got := m.input.Value(); got != "pasted text" {
		t.Fatalf("input = %q, want pasted text", got)
	}
	if m.attachments.len() != 0 {
		t.Fatalf("attachments = %d, want none", m.attachments.len())
	}
}

func TestAltVAttachesClipboardImage(t *testing.T) {
	m := newModel(nil, nil, newMsgQueue(), newTheme(), newMDRenderer(), "model", "")
	m.onResize(60, 20)
	png := append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 32)...)
	m.clipboardRead = func() (clipboardPayload, error) {
		return clipboardPayload{image: png}, nil
	}

	_, cmd := m.onKey(tea.KeyPressMsg(tea.Key{Code: 'v', Mod: tea.ModAlt}))
	if cmd == nil || !m.clipboardBusy {
		t.Fatal("alt+v did not start an asynchronous clipboard read")
	}
	m.Update(cmd())

	if m.clipboardBusy || m.attachments.len() != 1 {
		t.Fatalf("clipboardBusy=%v attachments=%d", m.clipboardBusy, m.attachments.len())
	}
}

func TestPasteCommandAttachesClipboardImage(t *testing.T) {
	m := newModel(nil, nil, newMsgQueue(), newTheme(), newMDRenderer(), "model", "")
	m.onResize(60, 20)
	png := append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 32)...)
	m.clipboardRead = func() (clipboardPayload, error) {
		return clipboardPayload{image: png}, nil
	}

	_, cmd := m.runCommand("/paste")
	if cmd == nil || !m.clipboardBusy {
		t.Fatal("/paste did not start an asynchronous clipboard read")
	}
	m.Update(cmd())

	if m.clipboardBusy || m.attachments.len() != 1 {
		t.Fatalf("clipboardBusy=%v attachments=%d", m.clipboardBusy, m.attachments.len())
	}
}

func TestPasteCommandWorksWhileWorking(t *testing.T) {
	m := newModel(nil, nil, newMsgQueue(), newTheme(), newMDRenderer(), "model", "")
	m.working = true
	png := append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 32)...)
	m.clipboardRead = func() (clipboardPayload, error) {
		return clipboardPayload{image: png}, nil
	}

	_, cmd := m.runCommand("/paste")
	if cmd == nil || !m.clipboardBusy {
		t.Fatal("/paste was rejected while a run was active")
	}
}

func TestSubmitIncludesAndClearsClipboardImage(t *testing.T) {
	q := newMsgQueue()
	m := newModel(nil, nil, q, newTheme(), newMDRenderer(), "model", "")
	m.working = true
	m.input.SetValue("describe this")
	png := append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 32)...)
	if err := m.attachments.add(png); err != nil {
		t.Fatal(err)
	}

	m.submit(submitFollowUp)
	if m.attachments.len() != 0 {
		t.Fatalf("attachments remained after submission: %d", m.attachments.len())
	}
	if len(m.queuedFollowUps) != 1 {
		t.Fatalf("queued follow-ups = %#v", m.queuedFollowUps)
	}
	content := m.queuedFollowUps[0].message.Content
	if len(content) != 2 || content[0].Text != "describe this" || content[1].Type != types.ContentImage {
		t.Fatalf("submitted content = %#v", content)
	}
	if !strings.Contains(m.queuedFollowUps[0].display, "1 image attached") {
		t.Fatalf("queued display = %q", m.queuedFollowUps[0].display)
	}
}

func TestImageOnlySubmissionAndBackspaceRemoval(t *testing.T) {
	q := newMsgQueue()
	m := newModel(nil, nil, q, newTheme(), newMDRenderer(), "model", "")
	m.working = true
	png := append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 32)...)
	if err := m.attachments.add(png); err != nil {
		t.Fatal(err)
	}
	m.onKey(tea.KeyPressMsg(tea.Key{Code: tea.KeyBackspace}))
	if m.attachments.len() != 0 {
		t.Fatal("backspace on an empty prompt did not remove the attachment")
	}
	if err := m.attachments.add(png); err != nil {
		t.Fatal(err)
	}

	m.submit(submitFollowUp)
	if len(m.queuedFollowUps) != 1 || len(m.queuedFollowUps[0].message.Content) != 2 {
		t.Fatalf("image-only submission = %#v", m.queuedFollowUps)
	}
	if m.queuedFollowUps[0].message.Content[1].Type != types.ContentImage {
		t.Fatalf("image-only content = %#v", m.queuedFollowUps[0].message.Content)
	}
}

func TestBoxedComposerStartsOneLineTall(t *testing.T) {
	m := newModel(nil, nil, nil, newTheme(), newMDRenderer(), "model", "")
	m.onResize(60, 20)

	if got := m.composerHeight(); got != 3 {
		t.Fatalf("composerHeight = %d, want 3 (one text row plus box border)", got)
	}
	out := m.renderComposer()
	plain := ansi.Strip(out)
	lines := strings.Split(plain, "\n")
	if len(lines) != 3 {
		t.Fatalf("boxed composer rows = %d, want 3:\n%s", len(lines), plain)
	}
	for _, line := range lines {
		if got := len([]rune(line)); got != 60 {
			t.Errorf("composer row width = %d, want 60: %q", got, line)
		}
	}
	for _, want := range []string{"╭", "╮", "╰", "╯", "› ", "Ask anything…"} {
		if !strings.Contains(plain, want) {
			t.Errorf("boxed composer missing %q:\n%s", want, plain)
		}
	}
}

func TestBoxedComposerGrowsWithInput(t *testing.T) {
	m := newModel(nil, nil, nil, newTheme(), newMDRenderer(), "model", "")
	m.onResize(60, 20)
	m.input.SetValue("line1\nline2\nline3")

	if got := m.composerHeight(); got != 5 {
		t.Fatalf("composerHeight = %d, want 5 (three text rows plus box border)", got)
	}
	plain := ansi.Strip(m.renderComposer())
	lines := strings.Split(plain, "\n")
	if len(lines) != 5 {
		t.Fatalf("boxed composer rows = %d, want 5:\n%s", len(lines), plain)
	}
	for _, line := range lines {
		if got := len([]rune(line)); got != 60 {
			t.Errorf("composer row width = %d, want 60: %q", got, line)
		}
	}
}

func TestBoxedComposerCapsGrowthOnShortTerminals(t *testing.T) {
	m := newModel(nil, nil, nil, newTheme(), newMDRenderer(), "model", "")
	m.onResize(60, 20)
	if m.input.MaxHeight != composerMaxRows {
		t.Fatalf("input MaxHeight = %d, want %d", m.input.MaxHeight, composerMaxRows)
	}
	m.onResize(60, 10)
	// 10 rows minus chrome, box border, and transcript reserve leaves one row.
	if m.input.MaxHeight != composerMinRows {
		t.Fatalf("input MaxHeight = %d, want %d on a short terminal", m.input.MaxHeight, composerMinRows)
	}
}

func TestFooterShowsModelAndEffort(t *testing.T) {
	m := newModel(nil, newRunner(agent.Config{Effort: llm.EffortHigh}, newMsgQueue(), nil), newMsgQueue(), newTheme(), newMDRenderer(), "opencode/muse-spark-1.3-contributor-free", "")
	m.onResize(80, 20)
	plain := ansi.Strip(m.footer())
	if !strings.Contains(plain, "opencode/muse-spark-1.3-contributor-free • high") {
		t.Fatalf("footer = %q, want model • high", plain)
	}

	m.runner.setEffort("")
	plain = ansi.Strip(m.footer())
	if !strings.Contains(plain, "opencode/muse-spark-1.3-contributor-free • default") {
		t.Fatalf("footer = %q, want model • default", plain)
	}
}

func TestRuledComposerKeepsRules(t *testing.T) {
	m := newModel(nil, nil, nil, newTheme(), newMDRenderer(), "model", "")
	m.onResize(60, 20)
	m.promptStyle = promptRuled
	m.syncComposerStyle()
	m.updateLayout()

	out := m.renderComposer()
	plain := ansi.Strip(out)
	if strings.ContainsAny(plain, "╭╮╰╯") {
		t.Fatalf("ruled composer should not render a box:\n%s", plain)
	}
	if !strings.Contains(plain, strings.Repeat("─", 60)) {
		t.Fatalf("ruled composer lost its rules:\n%s", plain)
	}
	if got, want := m.composerHeight(), m.input.Height()+composerChromeRows; got != want {
		t.Fatalf("composerHeight = %d, want input height plus rules (%d)", got, want)
	}
}

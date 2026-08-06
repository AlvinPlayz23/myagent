package tui

import (
	"context"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/AlvinPlayz23/myagent/internal/agent"
	"github.com/AlvinPlayz23/myagent/internal/types"
)

func TestQueuedFollowUpPromotesToTranscriptWhenConsumed(t *testing.T) {
	m := newModel(nil, nil, nil, newTheme(), newMDRenderer(), "model", "")
	m.onResize(50, 20)
	message := userMessage("run the tests after this")
	m.queuedFollowUps = []queuedMessage{{display: "run the tests after this", message: message}}
	m.updateLayout()

	if queued := m.renderQueuedFollowUps(); !strings.Contains(queued, "next") {
		t.Fatalf("queued follow-up has no pending label: %q", queued)
	}
	m.onAgentEvent(userMessageStartEvent("run the tests after this"))
	if len(m.queuedFollowUps) != 0 {
		t.Fatalf("queued follow-ups = %#v, want empty", m.queuedFollowUps)
	}
	if got := m.transcript.render(50); !strings.Contains(got, "run the tests after this") || strings.Contains(got, "next") {
		t.Fatalf("consumed follow-up was not promoted to transcript: %q", got)
	}
}

func TestFollowUpConsumptionClearsQueuedStatus(t *testing.T) {
	m := newModel(nil, nil, nil, newTheme(), newMDRenderer(), "model", "")
	m.working = true
	m.statusMsg = "Queued follow-up (1 pending)"
	message := userMessage("later")
	m.queuedFollowUps = []queuedMessage{{display: "later", message: message}}

	m.onAgentEvent(userMessageStartEvent("later"))
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
	m.queuedFollowUps = []queuedMessage{{display: "first", message: first}, {display: "second", message: second}}
	m.updateLayout()

	m.onAgentEvent(userMessageStartEvent("first"))
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

func TestSteeringEventDoesNotRemoveQueuedFollowUp(t *testing.T) {
	m := newModel(nil, nil, nil, newTheme(), newMDRenderer(), "model", "")
	m.queuedSteering = []types.Message{userMessage("steer")}
	m.queuedFollowUps = []queuedMessage{{display: "later", message: userMessage("later")}}

	m.onAgentEvent(userMessageStartEvent("steer"))
	if len(m.queuedFollowUps) != 1 || m.queuedFollowUps[0].display != "later" {
		t.Fatalf("steering removed queued follow-up: %#v", m.queuedFollowUps)
	}
}

func TestInitialPromptEventDoesNotRemoveQueuedFollowUp(t *testing.T) {
	m := newModel(nil, nil, nil, newTheme(), newMDRenderer(), "model", "")
	initial := userMessage("initial")
	m.activePrompt = &initial
	m.queuedFollowUps = []queuedMessage{{display: "later", message: userMessage("later")}}

	m.onAgentEvent(userMessageStartEvent("initial"))
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

	m.onMouseClick(tea.MouseClickMsg{X: 1, Y: 0, Button: tea.MouseLeft})
	m.onMouseMotion(tea.MouseMotionMsg{X: 5, Y: 0, Button: tea.MouseLeft})
	m.onMouseRelease(tea.MouseReleaseMsg{X: 5, Y: 0, Button: tea.MouseLeft})

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
	if !strings.Contains(view.Content, "/help") || !strings.Contains(view.Content, "/model [provider/model-id]") {
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

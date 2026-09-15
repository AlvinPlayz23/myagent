package tui

import (
	"strings"
	"testing"
)

func TestTimelineLaneWidthRequiresWideTerminalAndTwoTurns(t *testing.T) {
	for _, test := range []struct {
		width, turns, want int
	}{
		{width: timelineMinWidth - 1, turns: timelineMinTurns, want: 0},
		{width: timelineMinWidth, turns: timelineMinTurns - 1, want: 0},
		{width: timelineMinWidth, turns: timelineMinTurns, want: timelineRailWidth},
		{width: 120, turns: 8, want: timelineRailWidth},
	} {
		if got := timelineLaneWidth(test.width, test.turns); got != test.want {
			t.Fatalf("timelineLaneWidth(%d, %d) = %d, want %d", test.width, test.turns, got, test.want)
		}
	}
}

func TestTimelineRailCentersTicksAndChevrons(t *testing.T) {
	rail, ok := computeTimelineRail(tuiRect{X: 20, Y: 4, Width: 2, Height: 12}, 5, 2, 1, 3, false)
	if !ok {
		t.Fatal("timeline rail was not computed")
	}
	if got, want := rail.windowFrom, 0; got != want {
		t.Fatalf("window start = %d, want %d", got, want)
	}
	if got, want := rail.ticksY, 7; got != want {
		t.Fatalf("ticks y = %d, want %d", got, want)
	}
	if got, want := timelineTickRow(rail, 2), 9; got != want {
		t.Fatalf("active tick row = %d, want %d", got, want)
	}
	if rail.upY >= rail.ticksY || rail.downY <= rail.ticksY {
		t.Fatalf("chevrons are not outside tick stack: %#v", rail)
	}
}

func TestTimelineRailWindowsAroundActiveTurn(t *testing.T) {
	rail, ok := computeTimelineRail(tuiRect{Width: 2, Height: 6}, 12, 9, 8, 10, false)
	if !ok {
		t.Fatal("timeline rail was not computed")
	}
	if rail.windowFrom > rail.active || rail.active >= rail.windowTo {
		t.Fatalf("active turn %d is outside window %#v", rail.active, rail)
	}
	if got := timelineTickRow(rail, rail.windowFrom-1); got != -1 {
		t.Fatalf("out-of-window turn row = %d, want -1", got)
	}
	if got := timelineTickRow(rail, rail.windowTo); got != -1 {
		t.Fatalf("trailing out-of-window turn row = %d, want -1", got)
	}
}

func TestTimelineActiveTurnFollowsViewportOffset(t *testing.T) {
	starts := []int{0, 4, 11, 18}
	for _, test := range []struct {
		offset, want int
	}{
		{offset: 0, want: 0},
		{offset: 10, want: 1},
		{offset: 11, want: 2},
		{offset: 40, want: 3},
	} {
		if got := timelineActiveTurn(starts, test.offset); got != test.want {
			t.Fatalf("timelineActiveTurn(%d) = %d, want %d", test.offset, got, test.want)
		}
	}
}

func TestTimelineReservationChangesWithTurnCountAndWidth(t *testing.T) {
	m := newModel(nil, nil, nil, newTheme(), newMDRenderer(), "model", "")
	m.transcript.addUser("first")
	m.transcript.addUser("second")
	m.onResize(80, 24)
	if got, want := m.layout.timeline.Width, timelineRailWidth; got != want {
		t.Fatalf("wide two-turn timeline width = %d, want %d", got, want)
	}
	if got, want := m.layout.scrollbackContent.Width, 80-timelineRailWidth; got != want {
		t.Fatalf("wide content width = %d, want %d", got, want)
	}

	m.onResize(timelineMinWidth-1, 24)
	if !m.layout.timeline.empty() || !m.layout.scrollbar.empty() || m.layout.scrollbackContent.Width != timelineMinWidth-1 {
		t.Fatalf("narrow terminal fallback lane = %#v", m.layout)
	}
}

func TestTimelineTargetsBracketViewportTop(t *testing.T) {
	starts := []int{0, 4, 11, 18}
	for _, test := range []struct {
		offset, up, down int
	}{
		{offset: 0, up: -1, down: 1},
		{offset: 4, up: 0, down: 2},
		{offset: 7, up: 1, down: 2},
		{offset: 40, up: 3, down: -1},
	} {
		if got := timelineTurnAbove(starts, test.offset); got != test.up {
			t.Fatalf("timelineTurnAbove(%d) = %d, want %d", test.offset, got, test.up)
		}
		if got := timelineTurnBelow(starts, test.offset); got != test.down {
			t.Fatalf("timelineTurnBelow(%d) = %d, want %d", test.offset, got, test.down)
		}
	}
}

func TestTimelineRailHitUsesDisplayedTargets(t *testing.T) {
	rail, ok := computeTimelineRail(tuiRect{X: 10, Y: 3, Width: 2, Height: 8}, 4, 1, 0, 2, false)
	if !ok {
		t.Fatal("timeline rail was not computed")
	}
	if got, handled := rail.targetAt(11, rail.upY); !handled || got != 0 {
		t.Fatalf("up hit = (%d, %v), want (0, true)", got, handled)
	}
	if got, handled := rail.targetAt(10, timelineTickRow(rail, 2)); !handled || got != 2 {
		t.Fatalf("tick hit = (%d, %v), want (2, true)", got, handled)
	}
	if got, handled := rail.targetAt(10, rail.ticksY); !handled || got != 0 {
		t.Fatalf("whole-rail hit = (%d, %v), want (0, true)", got, handled)
	}
}

func TestRenderScrollbackShowsReferenceTimelineGlyphs(t *testing.T) {
	m := newModel(nil, nil, nil, newTheme(), newMDRenderer(), "model", "")
	m.transcript.addUser("first")
	m.transcript.beginAssistant()
	m.transcript.appendAssistantDelta("reply")
	m.transcript.endAssistant()
	m.transcript.addUser("second")
	m.onResize(80, 24)

	content := m.renderScrollback()
	for _, glyph := range []string{"▴", "▾", "━━"} {
		if !strings.Contains(content, glyph) {
			t.Fatalf("scrollback missing timeline glyph %q: %q", glyph, content)
		}
	}
}

func TestRenderTimelineUsesScreenRelativeRailRows(t *testing.T) {
	m := newModel(nil, nil, nil, newTheme(), newMDRenderer(), "model", "")
	m.hasSessionTitle = true
	m.transcript.addUser("first")
	m.transcript.beginAssistant()
	m.transcript.appendAssistantDelta("reply")
	m.transcript.endAssistant()
	m.transcript.addUser("second")
	m.onResize(80, 24)

	rail, ok := m.timelineRailFor(m.scrollbackContentWidth())
	if !ok || m.layout.scrollback.Y == 0 {
		t.Fatalf("timeline setup = rail=%#v layout=%#v", rail, m.layout)
	}
	rows := strings.Split(m.renderScrollback(), "\n")
	localRow := rail.upY - m.layout.scrollback.Y
	if localRow < 0 || localRow >= len(rows) {
		t.Fatalf("up row %d is outside rendered rows: %d", localRow, len(rows))
	}
	if !strings.Contains(rows[localRow], timelineChevronUpGlyph()) {
		t.Fatalf("up chevron rendered at wrong row %d: %q", localRow, m.renderScrollback())
	}
	if localRow > 0 && strings.Contains(rows[localRow-1], timelineChevronUpGlyph()) {
		t.Fatalf("up chevron still uses screen row as local row: %q", m.renderScrollback())
	}
}

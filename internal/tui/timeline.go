package tui

import (
	"sort"
	"strings"
)

const (
	// timelineRailWidth matches the reference rail's two-cell tick geometry.
	timelineRailWidth = 2
	// A single turn does not provide useful navigation and narrow terminals
	// need every transcript column they can keep.
	timelineMinWidth = 60
	timelineMinTurns = 2
)

// timelineRail is the complete geometry used to paint the rail. Keeping the
// window and row positions together prevents the active tick from drifting
// away from the chevrons as the transcript grows.
type timelineRail struct {
	rect       tuiRect
	windowFrom int
	windowTo   int
	ticksY     int
	upY        int
	downY      int
	active     int
	upTarget   int
	downTarget int
}

// targetAt is shared by rendering and mouse routing: a click anywhere in the
// two-cell rail resolves to the same turn that the displayed chevron or tick
// represents. A negative target means the hit is an intentional end stop.
func (r timelineRail) targetAt(x, y int) (int, bool) {
	if !r.rect.contains(x, y) {
		return -1, false
	}
	if y == r.upY {
		return r.upTarget, true
	}
	if y == r.downY {
		return r.downTarget, true
	}
	if y >= r.ticksY {
		rel := y - r.ticksY
		if rel >= 0 && rel < r.windowTo-r.windowFrom {
			return r.windowFrom + rel, true
		}
	}
	return -1, true
}

func timelineLaneWidth(width, turns int) int {
	if width >= timelineMinWidth && turns >= timelineMinTurns {
		return timelineRailWidth
	}
	return 0
}

func (m *model) timelineTurnCount() int {
	if m.transcript == nil {
		return 0
	}
	count := 0
	for _, block := range m.transcript.blocks {
		if block.kind == blockUser {
			count++
		}
	}
	return count
}

// timelineTurnStarts returns the rendered row at which each user turn begins.
// The transcript's blank separator and trailing newline are counted explicitly
// so the viewport offset and the highlighted tick share one coordinate system.
func (m *model) timelineTurnStarts(width int) []int {
	if m.transcript == nil {
		return nil
	}
	starts := make([]int, 0, m.timelineTurnCount())
	row := 0
	for _, block := range m.transcript.blocks {
		if block.kind == blockThinking && !m.transcript.showThinking {
			continue
		}
		if block.kind == blockUser {
			starts = append(starts, row)
		}
		row += timelineRenderedRows(m.transcript.renderBlock(block, max(1, width))) + 1
	}
	return starts
}

func timelineRenderedRows(content string) int {
	content = strings.TrimSuffix(content, "\n")
	if content == "" {
		return 1
	}
	return strings.Count(content, "\n") + 1
}

func timelineActiveTurn(starts []int, offset int) int {
	if len(starts) == 0 {
		return -1
	}
	active := 0
	for i, start := range starts {
		if start > offset {
			break
		}
		active = i
	}
	return active
}

func timelineTurnAbove(starts []int, offset int) int {
	count := sort.Search(len(starts), func(i int) bool { return starts[i] >= offset })
	if count == 0 {
		return -1
	}
	return count - 1
}

func timelineTurnBelow(starts []int, offset int) int {
	count := sort.Search(len(starts), func(i int) bool { return starts[i] > offset })
	if count >= len(starts) {
		return -1
	}
	return count
}

// computeTimelineRail follows the reference geometry: two rows belong to the
// directional chevrons, and the remaining rows show a contiguous turn window.
func computeTimelineRail(rect tuiRect, turnCount, active, upTarget, downTarget int, atBottom bool) (timelineRail, bool) {
	if rect.empty() || rect.Width < timelineRailWidth || rect.Height < 3 || turnCount < timelineMinTurns {
		return timelineRail{}, false
	}
	active = max(-1, min(active, turnCount-1))
	maxTicks := rect.Height - 2
	windowSize := min(turnCount, maxTicks)
	start := 0
	if turnCount > windowSize {
		tailStart := turnCount - windowSize
		if atBottom && active >= 0 {
			start = min(active, tailStart)
		} else {
			anchor := active
			if anchor < 0 {
				anchor = turnCount - 1
			}
			start = min(max(0, anchor-windowSize/2), tailStart)
		}
	}
	totalRows := windowSize + 2
	top := rect.Y + max(0, (rect.Height-totalRows)/2)
	return timelineRail{
		rect:       rect,
		windowFrom: start,
		windowTo:   start + windowSize,
		ticksY:     top + 1,
		upY:        top,
		downY:      top + 1 + windowSize,
		active:     active,
		upTarget:   upTarget,
		downTarget: downTarget,
	}, true
}

func (m *model) timelineRailFor(width int) (timelineRail, bool) {
	turns := m.timelineTurnStarts(width)
	if timelineLaneWidth(m.width, len(turns)) == 0 || m.layout.timeline.empty() {
		return timelineRail{}, false
	}
	active := timelineActiveTurn(turns, m.viewport.YOffset())
	upTarget := timelineTurnAbove(turns, m.viewport.YOffset())
	downTarget := timelineTurnBelow(turns, m.viewport.YOffset())
	return computeTimelineRail(m.layout.timeline, len(turns), active, upTarget, downTarget, m.viewport.AtBottom())
}

func (m *model) timelineTargetAt(x, y int) (int, bool) {
	if !m.ready || m.layout.timeline.empty() {
		return -1, false
	}
	rail, ok := m.timelineRailFor(m.scrollbackContentWidth())
	if !ok {
		return -1, false
	}
	return rail.targetAt(x, y)
}

func (m *model) jumpToTimelineTurn(turn int) {
	starts := m.timelineTurnStarts(m.scrollbackContentWidth())
	if turn < 0 || turn >= len(starts) {
		return
	}
	m.viewport.SetYOffset(starts[turn])
	m.refreshViewport()
}

func (m *model) scrollbarTargetAt(x, y int) bool {
	if !m.ready || m.layout.scrollbar.empty() || !m.layout.scrollbar.contains(x, y) {
		return false
	}
	total := m.viewport.TotalLineCount()
	viewportHeight := m.layout.scrollback.Height
	if total <= viewportHeight || viewportHeight <= 1 {
		return true
	}
	row := y - m.layout.scrollbar.Y
	maxOffset := total - viewportHeight
	maxRow := max(1, viewportHeight-1)
	m.viewport.SetYOffset(row * maxOffset / maxRow)
	m.refreshViewport()
	return true
}

func timelineTickRow(rail timelineRail, turn int) int {
	if turn < rail.windowFrom || turn >= rail.windowTo {
		return -1
	}
	return rail.ticksY + turn - rail.windowFrom
}

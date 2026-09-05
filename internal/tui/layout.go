package tui

// tuiRect is the string-rendering equivalent of ratatui's Rect. Keeping the
// geometry independent of Bubble Tea makes short-terminal behavior testable.
type tuiRect struct {
	X, Y          int
	Width, Height int
}

func (r tuiRect) empty() bool { return r.Width <= 0 || r.Height <= 0 }

func (r tuiRect) contains(x, y int) bool {
	return !r.empty() && x >= r.X && x < r.X+r.Width && y >= r.Y && y < r.Y+r.Height
}

// tuiLayoutParams describes the rows requested by the agent view. Optional
// rows are omitted together with their adjacent gap; scrollback is the only
// region with a minimum claim.
type tuiLayoutParams struct {
	width, height int

	topBarRows     int
	panelRows      int
	queueRows      int
	attachmentRows int
	composerRows   int
	statusRows     int
	shortcutRows   int
	footerRows     int

	// The rail is reserved inside scrollback rather than appended after it.
	// timelineWidth is the two-cell timeline rail. scrollbarWidth is the
	// fallback lane total (one-cell gap plus one-cell track).
	timelineWidth  int
	scrollbarWidth int
}

// tuiLayout is the named geometry for one frame of the agent view. Rectangles
// are stacked top-to-bottom and never overlap.
type tuiLayout struct {
	topBar            tuiRect
	panel             tuiRect
	scrollback        tuiRect
	scrollbackContent tuiRect
	timeline          tuiRect
	scrollbar         tuiRect
	queue             tuiRect
	attachments       tuiRect
	composer          tuiRect
	statusLine        tuiRect
	shortcuts         tuiRect
	footer            tuiRect

	compact       bool
	shortTerminal bool
}

const (
	shortTerminalRows          = 16
	compactTerminalRows        = 20
	minScrollbackRows          = 5
	scrollbarGapWidth          = 1
	scrollbarTrackWidth        = 1
	fallbackScrollbarLaneWidth = scrollbarGapWidth + scrollbarTrackWidth
)

// computeTUILayout mirrors the reference AgentViewLayout::compute policy:
// fixed rows keep their requested height when possible, optional rows are
// clamped before scrollback, and the scrollback retains at least five visible
// row when the terminal is overfull. The one-row terminal exception gives the
// scrollback the whole surface instead of allowing the composer to overflow it.
func computeTUILayout(p tuiLayoutParams) tuiLayout {
	width := max(0, p.width)
	height := max(0, p.height)
	if height == 0 {
		return tuiLayout{}
	}
	short := height > 0 && height <= shortTerminalRows

	topRows := max(0, p.topBarRows)
	footerRows := max(0, p.footerRows)
	panelRows := max(0, p.panelRows)
	queueRows := max(0, p.queueRows)
	attachmentRows := max(0, p.attachmentRows)
	// Footer metadata and optional side rows are deliberately expendable on a
	// short terminal; prompt, shortcuts, and a useful transcript remain.
	if short {
		footerRows = 0
		queueRows = 0
		attachmentRows = 0
		panelRows = 0
	}
	shortcutRows := max(0, p.shortcutRows)
	if height > 0 && height < 8 {
		shortcutRows = 0
	}

	composerRows := max(1, p.composerRows)
	statusRows := max(0, p.statusRows)

	// Keep one row for the transcript and one for the composer whenever the
	// terminal can accommodate both. On a two-row terminal the top bar and all
	// optional chrome disappear; on a one-row terminal the transcript wins.
	scrollbackFloor := min(minScrollbackRows, height)
	if height == 1 {
		topRows = 0
		composerRows = 0
		statusRows = 0
		shortcutRows = 0
		footerRows = 0
		panelRows = 0
		queueRows = 0
		attachmentRows = 0
	} else {
		topRows = min(topRows, max(0, height-scrollbackFloor-1))
		remaining := max(0, height-topRows-scrollbackFloor)

		// The prompt is the primary input surface. It may shrink to one row,
		// but optional bottom chrome never steals that minimum.
		composerRows = min(composerRows, remaining)
		remaining -= composerRows
		statusRows = min(statusRows, remaining)
		remaining -= statusRows
		shortcutRows = min(shortcutRows, remaining)
		remaining -= shortcutRows
		footerRows = min(footerRows, remaining)
		remaining -= footerRows

		// Pickers, queued sends, and attachment chips borrow surplus before the
		// transcript. Keep the established priority so a short picker remains
		// discoverable without starving its viewport completely.
		queueRows = min(queueRows, remaining)
		remaining -= queueRows
		attachmentRows = min(attachmentRows, remaining)
		remaining -= attachmentRows
		panelRows = min(panelRows, remaining)
		remaining -= panelRows

		scrollbackFloor = min(scrollbackFloor, height-topRows-composerRows-statusRows-shortcutRows-footerRows-queueRows-attachmentRows-panelRows)
	}

	reserved := topRows + composerRows + statusRows + shortcutRows + footerRows + queueRows + attachmentRows + panelRows
	scrollbackRows := max(scrollbackFloor, height-reserved)

	y := 0
	place := func(rows int) tuiRect {
		r := tuiRect{X: 0, Y: y, Width: width, Height: rows}
		y += rows
		return r
	}
	l := tuiLayout{
		topBar:        place(topRows),
		panel:         tuiRect{},
		scrollback:    tuiRect{},
		scrollbar:     tuiRect{},
		queue:         tuiRect{},
		attachments:   tuiRect{},
		composer:      tuiRect{},
		statusLine:    tuiRect{},
		shortcuts:     tuiRect{},
		footer:        tuiRect{},
		compact:       height > 0 && height <= compactTerminalRows,
		shortTerminal: short,
	}
	if panelRows > 0 {
		l.panel = place(panelRows)
	}
	l.scrollback = place(scrollbackRows)
	if queueRows > 0 {
		l.queue = place(queueRows)
	}
	if attachmentRows > 0 {
		l.attachments = place(attachmentRows)
	}
	if statusRows > 0 {
		l.statusLine = place(statusRows)
	}
	l.composer = place(composerRows)
	if shortcutRows > 0 {
		l.shortcuts = place(shortcutRows)
	}
	if footerRows > 0 {
		l.footer = place(footerRows)
	}

	lane := 0
	l.scrollbackContent = l.scrollback
	if p.timelineWidth > 0 {
		lane = min(max(0, p.timelineWidth), max(0, width-1))
		if lane > 0 && !l.scrollback.empty() {
			l.scrollbackContent.Width = max(1, l.scrollback.Width-lane)
			l.timeline = tuiRect{
				X:      l.scrollback.X + l.scrollback.Width - lane,
				Y:      l.scrollback.Y,
				Width:  lane,
				Height: l.scrollback.Height,
			}
		}
	} else if p.scrollbarWidth > 0 && !l.scrollback.empty() {
		// The ordinary scrollbar always keeps a one-cell visual gap from the
		// transcript. Its track is the rightmost cell; rendering decides
		// whether content actually overflows enough to show a thumb.
		lane = fallbackScrollbarLaneWidth
		if width > lane {
			l.scrollbackContent.Width = l.scrollback.Width - lane
			l.scrollbar = tuiRect{
				X:      l.scrollback.X + l.scrollback.Width - scrollbarTrackWidth,
				Y:      l.scrollback.Y,
				Width:  scrollbarTrackWidth,
				Height: l.scrollback.Height,
			}
		}
	}
	return l
}

func (l tuiLayout) rows() int {
	maxY := 0
	for _, r := range []tuiRect{l.topBar, l.panel, l.scrollback, l.queue, l.attachments, l.composer, l.statusLine, l.shortcuts, l.footer} {
		maxY = max(maxY, r.Y+r.Height)
	}
	return maxY
}

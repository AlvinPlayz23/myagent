package sticky

import "testing"

func TestStreamingAppendWhilePinned(t *testing.T) {
	b := New(10, 100, true, Bottom)
	if b.ScrollTop() != 90 || !b.StickyActive() {
		t.Fatalf("initial top=%d active=%v, want 90/true", b.ScrollTop(), b.StickyActive())
	}
	for i := 0; i < 5; i++ {
		b.Append(1)
		b.Layout(10)
		want := 90 + i + 1
		if b.ScrollTop() != want {
			t.Fatalf("append %d: top=%d, want %d", i+1, b.ScrollTop(), want)
		}
		if !b.StickyActive() {
			t.Fatalf("append %d: stickiness paused while pinned", i+1)
		}
	}
	if b.ScrollHeight() != 105 {
		t.Fatalf("scrollHeight=%d, want 105", b.ScrollHeight())
	}
}

func TestWheelUpPausesAndWheelDownResumes(t *testing.T) {
	b := New(10, 100, true, Bottom) // top 90
	b.ScrollBy(-3)                  // wheel up
	if b.ScrollTop() != 87 || b.StickyActive() {
		t.Fatalf("after wheel up top=%d active=%v, want 87/false", b.ScrollTop(), b.StickyActive())
	}

	b.Append(20) // content 120, max 110
	b.Layout(10)
	if b.ScrollTop() != 87 || b.StickyActive() {
		t.Fatalf("after paused growth top=%d active=%v, want 87/false", b.ScrollTop(), b.StickyActive())
	}

	b.ScrollBy(23) // 87 + 23 = 110 = max: back at the edge
	if b.ScrollTop() != 110 || !b.StickyActive() {
		t.Fatalf("at edge top=%d active=%v, want 110/true", b.ScrollTop(), b.StickyActive())
	}
	b.Append(5) // content 125, max 115
	b.Layout(10)
	if b.ScrollTop() != 115 || !b.StickyActive() {
		t.Fatalf("after resumed growth top=%d active=%v, want 115/true", b.ScrollTop(), b.StickyActive())
	}
}

func TestPendingTargetFlushAndDeltaAccumulation(t *testing.T) {
	b := New(10, 100, false, Bottom)
	b.ScrollTo(50)
	b.ScrollBy(5) // delta adds to the pending absolute target
	if b.ScrollTop() != 55 {
		t.Fatalf("immediate top=%d, want 55", b.ScrollTop())
	}

	b.Append(50) // staged: committed height is still 100
	if b.ScrollHeight() != 100 {
		t.Fatalf("staged append leaked into scrollHeight: %d", b.ScrollHeight())
	}
	b.Layout(10) // content 150, max 140; pending 55 is applied
	if b.ScrollTop() != 55 {
		t.Fatalf("flushed top=%d, want 55", b.ScrollTop())
	}

	b.Append(20)
	b.Layout(10) // pending target is one-shot: no re-apply
	if b.ScrollTop() != 55 {
		t.Fatalf("target re-applied after flush: top=%d, want 55", b.ScrollTop())
	}
}

func TestScrollInSameTickAsGrowthLandsOnNewContent(t *testing.T) {
	b := New(10, 100, true, Bottom) // max 90
	b.Append(50)                    // not laid out yet
	b.ScrollToEnd()                 // applies to old layout now, queues end
	if b.ScrollTop() != 90 {
		t.Fatalf("immediate top=%d, want old max 90", b.ScrollTop())
	}
	b.Layout(10) // content 150, max 140
	if b.ScrollTop() != 140 {
		t.Fatalf("post-layout top=%d, want new max 140", b.ScrollTop())
	}

	b.Append(10) // end target survives growth
	b.Layout(10)
	if b.ScrollTop() != 150 {
		t.Fatalf("end target did not survive growth: top=%d, want 150", b.ScrollTop())
	}
}

func TestScrollToScrollHeightStaysAtEnd(t *testing.T) {
	b := New(10, 100, true, Bottom)
	b.Append(50)
	b.ScrollTo(b.ScrollHeight()) // scrollHeight=100 >= max 90
	b.Layout(10)
	if b.ScrollTop() != 140 {
		t.Fatalf("top=%d, want 140", b.ScrollTop())
	}
	b.Append(10)
	b.Layout(10)
	if b.ScrollTop() != 150 {
		t.Fatalf("top=%d, want 150 after further growth", b.ScrollTop())
	}
}

func TestScrollHeightAndScrollTopArithmeticOnResize(t *testing.T) {
	b := New(10, 25, false, Bottom)
	if b.MaxScroll() != 15 {
		t.Fatalf("max=%d, want 15", b.MaxScroll())
	}
	b.ScrollTo(12)
	b.SetContent(20) // max 10
	b.Layout(10)
	if b.ScrollTop() != 10 {
		t.Fatalf("shrink clamp top=%d, want 10", b.ScrollTop())
	}
	b.SetContent(30) // max 20; absolute target was consumed, so stay at 10
	b.Layout(10)
	if b.ScrollTop() != 10 {
		t.Fatalf("growth moved an unpinned box: top=%d, want 10", b.ScrollTop())
	}

	b.Layout(40) // viewport grows past content: max 0
	if b.ScrollTop() != 0 {
		t.Fatalf("viewport growth top=%d, want 0", b.ScrollTop())
	}
}

func TestScrollToTopIsNotAnEndTargetWhenContentFits(t *testing.T) {
	b := New(10, 5, false, Bottom) // max 0
	b.ScrollTo(0)
	b.SetContent(50)
	b.Layout(10) // max 40
	if b.ScrollTop() != 0 {
		t.Fatalf("scrollTo(0) followed growth to %d, want 0", b.ScrollTop())
	}
}

func TestTopStickyPauseResume(t *testing.T) {
	b := New(10, 100, true, Top)
	if b.ScrollTop() != 0 || !b.StickyActive() {
		t.Fatalf("initial top=%d active=%v, want 0/true", b.ScrollTop(), b.StickyActive())
	}
	b.ScrollBy(5)
	b.SetContent(200)
	b.Layout(10)
	if b.ScrollTop() != 5 || b.StickyActive() {
		t.Fatalf("paused top=%d active=%v, want 5/false", b.ScrollTop(), b.StickyActive())
	}
	b.ScrollBy(-5)
	b.SetContent(300)
	b.Layout(10)
	if b.ScrollTop() != 0 || !b.StickyActive() {
		t.Fatalf("resumed top=%d active=%v, want 0/true", b.ScrollTop(), b.StickyActive())
	}
}

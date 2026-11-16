package kit

import (
	"time"

	tea "charm.land/bubbletea/v2"
)

// Tick rate bounds from GOTUI.md §3.2: one driver at 15–20 fps.
const (
	TickFPSMin = 15
	TickFPSMax = 20
)

// TickMsg is one driver tick. Tick counts up from 1 for each Emit, so a
// subscriber can derive elapsed time from Interval() when frames are dropped.
type TickMsg struct {
	Tick int
}

// TickFunc receives every tick. Subscribers must be cheap: they run inside the
// Bubble Tea update goroutine, once per interval, for every active animation.
type TickFunc func(TickMsg)

// TickDriver is the single animation clock. It owns no goroutine and no timer of
// its own: it counts subscribers and produces one tea.Cmd at a time, so the
// driver is completely inert (NextTick returns nil) until something subscribes.
//
// OpenCode counterpart: the renderer's 40 ms frame loop, narrowed here to the
// 15–20 fps tick GOTUI §3.2 specifies. OpenTUI: Timeline, deliberately not
// adopted (see §3.0.1).
//
// Integration with the app loop (the model owns one driver):
//
//	func (m model) Init() tea.Cmd { return m.ticks.NextTick() }
//
//	func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
//		if tick, ok := msg.(kit.TickMsg); ok {
//			_ = tick // the driver already dispatched to subscribers
//			m.ticks.Emit()
//			m.toasts.Advance(m.ticks.Interval())
//			return m, m.ticks.NextTick()
//		}
//		...
//	}
//
// NextTick is the only scheduling edge: with no subscribers it returns nil, so
// an idle app schedules nothing. Call it after the first Add/Subscribe (and
// after each Emit) to keep the loop armed.
type TickDriver struct {
	interval time.Duration
	subs     map[int]TickFunc
	order    []int
	nextID   int
	tick     int
}

// NewTickDriver builds a driver at fps, clamped to [TickFPSMin, TickFPSMax].
// A non-positive fps selects the minimum.
// OpenTUI: new Timeline({ fps }).
func NewTickDriver(fps int) *TickDriver {
	if fps < TickFPSMin {
		fps = TickFPSMin
	}
	if fps > TickFPSMax {
		fps = TickFPSMax
	}
	return &TickDriver{
		interval: time.Second / time.Duration(fps),
		subs:     make(map[int]TickFunc),
	}
}

// Interval is the frame period (66 ms at 15 fps, 50 ms at 20 fps).
func (d *TickDriver) Interval() time.Duration { return d.interval }

// Subscribe registers fn and returns an idempotent cancel function. It is the
// convenience form of Add + Remove. The first subscriber arms the driver;
// cancelling the last one disarms it.
// OpenTUI: Timeline.add/remove.
func (d *TickDriver) Subscribe(fn TickFunc) func() {
	id := d.Add(fn)
	return func() { d.Remove(id) }
}

// Add registers fn and returns its subscriber id. A nil fn is ignored and
// returns 0. The first Add arms the driver; ids are stable for the driver's
// lifetime and can be passed to Remove.
func (d *TickDriver) Add(fn TickFunc) int {
	if fn == nil {
		return 0
	}
	d.nextID++
	id := d.nextID
	d.subs[id] = fn
	d.order = append(d.order, id)
	return id
}

// Remove drops one subscriber. Unknown and already-removed ids are no-ops, so
// removing the last subscriber disarms the driver idempotently.
func (d *TickDriver) Remove(id int) {
	if id == 0 {
		return
	}
	if _, ok := d.subs[id]; !ok {
		return
	}
	delete(d.subs, id)
	for i, v := range d.order {
		if v == id {
			d.order = append(d.order[:i], d.order[i+1:]...)
			break
		}
	}
}

// Len is the subscriber count.
func (d *TickDriver) Len() int { return len(d.subs) }

// Active reports whether anything is subscribed; it is the "only while used"
// gate from §3.2.
func (d *TickDriver) Active() bool { return len(d.subs) > 0 }

// Tick is the last emitted tick number (0 before the first Emit).
func (d *TickDriver) Tick() int { return d.tick }

// Emit advances the clock by one tick and dispatches to every subscriber in
// subscription order. The app calls it when the tick message arrives; tests
// call it directly to stay time-independent. The order is snapshotted so a
// subscriber can cancel itself (or a peer) without skipping the rest.
func (d *TickDriver) Emit() TickMsg {
	d.tick++
	msg := TickMsg{Tick: d.tick}
	for _, id := range append([]int(nil), d.order...) {
		if fn, ok := d.subs[id]; ok && fn != nil {
			fn(msg)
		}
	}
	return msg
}

// NextTick produces the single next tick as a tea.Cmd, or nil when nothing is
// subscribed. Re-arm it from Update after each tick; the driver never schedules
// more than one timer, which is the §3.2 "single driver" rule.
func (d *TickDriver) NextTick() tea.Cmd {
	if !d.Active() {
		return nil
	}
	next := d.tick + 1
	return tea.Tick(d.interval, func(time.Time) tea.Msg {
		return TickMsg{Tick: next}
	})
}

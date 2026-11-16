package kit

import (
	"testing"
	"time"
)

func TestTickDriverAddRemove(t *testing.T) {
	d := NewTickDriver(20)
	if id := d.Add(nil); id != 0 || d.Active() {
		t.Fatalf("nil Add = %d active=%v", id, d.Active())
	}

	var seen []int
	idA := d.Add(func(m TickMsg) { seen = append(seen, m.Tick) })
	idB := d.Add(func(m TickMsg) { seen = append(seen, m.Tick) })
	if idA == 0 || idB == 0 || idA == idB {
		t.Fatalf("Add ids = %d/%d", idA, idB)
	}
	if d.Len() != 2 || !d.Active() {
		t.Fatalf("subscribers=%d active=%v", d.Len(), d.Active())
	}
	d.Emit()
	if len(seen) != 2 {
		t.Fatalf("Add deliveries = %v", seen)
	}

	d.Remove(idA)
	d.Remove(idA) // idempotent
	if d.Len() != 1 {
		t.Fatalf("Remove left %d subscribers", d.Len())
	}
	d.Emit()
	if len(seen) != 3 {
		t.Fatalf("removed subscriber still ran: %v", seen)
	}

	d.Remove(idB)
	if d.Active() || d.NextTick() != nil {
		t.Fatal("driver stayed armed after Remove")
	}

	// A callback may cancel itself without skipping the next subscriber.
	self := NewTickDriver(20)
	var order []int
	var cancel func()
	cancel = self.Subscribe(func(TickMsg) {
		order = append(order, 1)
		cancel()
	})
	self.Subscribe(func(TickMsg) { order = append(order, 2) })
	self.Emit()
	if len(order) != 2 || order[0] != 1 || order[1] != 2 {
		t.Fatalf("self-cancel dispatch = %v", order)
	}
}

func TestTickDriverIntervalClamp(t *testing.T) {
	cases := []struct {
		fps  int
		want time.Duration
	}{
		{0, time.Second / 15},
		{5, time.Second / 15},
		{15, time.Second / 15},
		{20, time.Second / 20},
		{120, time.Second / 20},
	}
	for _, c := range cases {
		if got := NewTickDriver(c.fps).Interval(); got != c.want {
			t.Fatalf("fps %d interval %v, want %v", c.fps, got, c.want)
		}
	}
}

func TestTickDriverOnlyRunsWhileSubscribed(t *testing.T) {
	d := NewTickDriver(15)
	if d.Active() || d.NextTick() != nil {
		t.Fatal("idle driver armed a tick")
	}

	var seen []int
	cancelA := d.Subscribe(func(m TickMsg) { seen = append(seen, m.Tick) })
	cancelB := d.Subscribe(func(m TickMsg) { seen = append(seen, m.Tick) })
	if !d.Active() || d.Len() != 2 {
		t.Fatalf("subscribers=%d active=%v", d.Len(), d.Active())
	}
	if d.NextTick() == nil {
		t.Fatal("subscribed driver returned a nil tick")
	}

	msg := d.Emit()
	if msg.Tick != 1 || d.Tick() != 1 {
		t.Fatalf("tick=%d driver=%d", msg.Tick, d.Tick())
	}
	if len(seen) != 2 || seen[0] != 1 || seen[1] != 1 {
		t.Fatalf("deliveries = %v", seen)
	}

	cancelA()
	cancelA() // idempotent
	if d.Len() != 1 {
		t.Fatalf("cancel left %d subscribers", d.Len())
	}
	d.Emit()
	if len(seen) != 3 {
		t.Fatalf("cancelled subscriber still ran: %v", seen)
	}

	cancelB()
	if d.Active() || d.NextTick() != nil {
		t.Fatal("driver stayed armed with no subscribers")
	}
}

// TestTickDriverGoldenProof snapshots the on/off proof the spec asks for.
func TestTickDriverGoldenProof(t *testing.T) {
	d := NewTickDriver(20)
	lines := []string{driverState(d, 0)}
	cancel := d.Subscribe(func(TickMsg) {})
	lines = append(lines, driverState(d, 0))
	d.Emit()
	lines = append(lines, driverState(d, 1))
	cancel()
	lines = append(lines, driverState(d, d.Tick()))
	frame := ""
	for _, l := range lines {
		frame += l + "\n"
	}
	writeGolden(t, "s2b_tickdriver.txt", frame)
}

// driverState formats one proof line: subscriber count, active flag, and
// whether NextTick would schedule anything.
func driverState(d *TickDriver, delivered int) string {
	armed := "nil"
	if d.NextTick() != nil {
		armed = "set"
	}
	return "subscribers=" + itoa(d.Len()) + " active=" + boolStr(d.Active()) +
		" next-tick=" + armed + " tick=" + itoa(d.Tick()) + " delivered=" + itoa(delivered)
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

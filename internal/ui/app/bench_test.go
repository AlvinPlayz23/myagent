package app

import (
	"testing"
)

// BenchmarkShellStreamingAppend200x60 measures the S3 streaming path: one event
// batch mutates the transcript and bumps its revision once, then Sync + Render
// must compose exactly one frame. The compose/op metric is the G5 gate.
func BenchmarkShellStreamingAppend200x60(b *testing.B) {
	host := &fakeHost{
		blocks:    manyBlocks(40),
		composer:  "> streaming benchmark",
		composerH: 2,
	}
	s := newShell(host)
	s.Resize(200, 60)
	s.Render(200, 60)
	before := s.ComposeCount()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		last := host.blocks[len(host.blocks)-1]
		last.lines = append(last.lines, "streamed assistant text · wrapped line")
		host.transcript++
		s.Sync()
		s.Render(200, 60)
	}
	b.StopTimer()
	if got := s.ComposeCount() - before; got != b.N {
		b.Fatalf("composes = %d, want one per event batch (%d)", got, b.N)
	}
	b.ReportMetric(float64(s.ComposeCount()-before)/float64(b.N), "compose/op")
}

// BenchmarkShellIdleFrame200x60 proves idle ticks do not compose.
func BenchmarkShellIdleFrame200x60(b *testing.B) {
	host := &fakeHost{
		blocks:    manyBlocks(40),
		composer:  "> streaming benchmark",
		composerH: 2,
	}
	s := newShell(host)
	s.Resize(200, 60)
	s.Render(200, 60)
	before := s.ComposeCount()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.Render(200, 60)
	}
	b.StopTimer()
	if got := s.ComposeCount() - before; got != 0 {
		b.Fatalf("idle frames composed %d times, want 0", got)
	}
	b.ReportMetric(0, "compose/op")
}

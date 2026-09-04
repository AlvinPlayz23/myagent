//go:build unix

package ptytest

import (
	"fmt"
	"os"
	"testing"
)

// rawdump is a debugging helper: replay a captured raw PTY log through the
// screen emulator and print the numbered projection. Skipped unless
// PTYTEST_RAW points at a log file.
func TestRawDump(t *testing.T) {
	path := os.Getenv("PTYTEST_RAW")
	if path == "" {
		t.Skip("set PTYTEST_RAW=<raw log> to replay a capture")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read raw log: %v", err)
	}
	s := NewScreen(100, 30)
	s.Feed(data)
	for i, row := range s.cells {
		fmt.Printf("%2d|%s\n", i, string(row))
	}
	fmt.Printf("cursor row=%d col=%d\n", s.row, s.col)
}

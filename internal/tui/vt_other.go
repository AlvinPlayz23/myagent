//go:build !windows

package tui

// enableVTIfRequested is a no-op on non-Windows platforms.
func enableVTIfRequested() error {
	return nil
}

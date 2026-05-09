//go:build windows

package main

import "github.com/jtolio/tinyemu-go/machine"

// setupResizeHandler is a no-op on Windows as SIGWINCH is not available.
func setupResizeHandler(term *Terminal, m machine.Board) {
	// Windows does not have SIGWINCH. Terminal resize detection would need
	// to use a different mechanism (e.g., polling or Windows Console API).
}

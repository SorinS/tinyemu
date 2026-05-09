//go:build windows

package main

import (
	"errors"
	"os"
)

// Terminal handles terminal I/O.
// On Windows, raw terminal mode is not yet implemented.
type Terminal struct{}

// NewTerminal returns an error on Windows as raw terminal mode is not yet implemented.
func NewTerminal(allowCtrlC bool) (*Terminal, error) {
	return nil, errors.New("raw terminal mode not supported on Windows")
}

// Restore is a no-op on Windows.
func (t *Terminal) Restore() {}

// Read returns 0 on Windows (no input).
func (t *Terminal) Read(buf []byte) int {
	return 0
}

// Write writes to stdout.
func (t *Terminal) Write(buf []byte) (int, error) {
	return os.Stdout.Write(buf)
}

// GetSize returns a default terminal size on Windows.
func (t *Terminal) GetSize() (width, height int) {
	return 80, 25
}

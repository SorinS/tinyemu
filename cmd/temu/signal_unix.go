//go:build linux || darwin

package main

import (
	"os"
	"os/signal"
	"syscall"

	"github.com/jtolio/tinyemu-go/machine"
)

// setupResizeHandler sets up a SIGWINCH handler to resize the console on terminal resize.
// Reference: tinyemu-2019-12-21/temu.c handles terminal resize via SIGWINCH
func setupResizeHandler(term *Terminal, m machine.Board) {
	if term == nil {
		return
	}

	resizeCh := make(chan os.Signal, 1)
	signal.Notify(resizeCh, syscall.SIGWINCH)
	go func() {
		for range resizeCh {
			if virtConsole := m.Console(); virtConsole != nil {
				w, h := term.GetSize()
				virtConsole.ResizeEvent(w, h)
			}
		}
	}()
}

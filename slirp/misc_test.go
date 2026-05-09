package slirp

import (
	"bytes"
	"os"
	"testing"
	"time"
)

// TestGetTimeMsMonotonic verifies that GetTimeMs returns monotonically
// increasing values.
// Reference: tinyemu-2019-12-21/slirp/misc.c:88-95
func TestGetTimeMsMonotonic(t *testing.T) {
	t1 := GetTimeMs()
	time.Sleep(10 * time.Millisecond)
	t2 := GetTimeMs()

	if t2 <= t1 {
		t.Errorf("GetTimeMs should be monotonic: t1=%d, t2=%d", t1, t2)
	}
}

// TestGetTimeMsResolution verifies that GetTimeMs has at least millisecond
// resolution.
func TestGetTimeMsResolution(t *testing.T) {
	t1 := GetTimeMs()
	time.Sleep(50 * time.Millisecond)
	t2 := GetTimeMs()

	diff := t2 - t1
	// Should be at least 40ms (allowing for timing variance)
	if diff < 40 {
		t.Errorf("GetTimeMs resolution too low: expected ~50ms diff, got %dms", diff)
	}
	// Should be less than 100ms
	if diff > 100 {
		t.Errorf("GetTimeMs resolution too high: expected ~50ms diff, got %dms", diff)
	}
}

// TestGetTimeMsWrap verifies behavior documentation for uint32 wrap.
// The C code returns uint32_t, so it wraps at ~49.7 days.
func TestGetTimeMsWrap(t *testing.T) {
	// This is a documentation test - we can't actually wait 49.7 days
	// Just verify the return type is uint32
	var _ uint32 = GetTimeMs()
}

// TestLprint verifies the Lprint function writes to stdout.
// Reference: tinyemu-2019-12-21/slirp/misc.c:267-274
func TestLprint(t *testing.T) {
	// Capture stdout
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	Lprint("test %s %d\n", "hello", 42)

	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := buf.String()

	expected := "test hello 42\n"
	if output != expected {
		t.Errorf("Lprint output: got %q, want %q", output, expected)
	}
}

// TestForkExec verifies that ForkExec returns 0 (not implemented).
// Reference: tinyemu-2019-12-21/slirp/misc.c:100-105
func TestForkExec(t *testing.T) {
	result := ForkExec()
	if result != 0 {
		t.Errorf("ForkExec should return 0, got %d", result)
	}
}

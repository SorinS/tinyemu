package mem

import (
	"testing"
)

// TestNewIRQSignal tests creating a new IRQ signal.
func TestNewIRQSignal(t *testing.T) {
	called := false
	handler := func(opaque any, irqNum int, level int) {
		called = true
	}

	opaque := "test-opaque"
	irq := NewIRQSignal(handler, opaque, 5)

	if irq == nil {
		t.Fatal("NewIRQSignal returned nil")
	}
	if irq.irqNum != 5 {
		t.Errorf("expected irqNum 5, got %d", irq.irqNum)
	}
	if irq.opaque != opaque {
		t.Errorf("expected opaque %v, got %v", opaque, irq.opaque)
	}
	if irq.setIRQ == nil {
		t.Error("expected setIRQ to be set")
	}

	// Handler should not have been called yet
	if called {
		t.Error("handler should not have been called during construction")
	}
}

// TestIRQSignalSet tests the Set method.
func TestIRQSignalSet(t *testing.T) {
	var lastOpaque any
	var lastIRQNum int
	var lastLevel int
	callCount := 0

	handler := func(opaque any, irqNum int, level int) {
		lastOpaque = opaque
		lastIRQNum = irqNum
		lastLevel = level
		callCount++
	}

	opaque := struct{ name string }{"test"}
	irq := NewIRQSignal(handler, opaque, 7)

	// Test Set with level 1
	irq.Set(1)
	if callCount != 1 {
		t.Errorf("expected 1 call, got %d", callCount)
	}
	if lastOpaque != opaque {
		t.Errorf("expected opaque %v, got %v", opaque, lastOpaque)
	}
	if lastIRQNum != 7 {
		t.Errorf("expected irqNum 7, got %d", lastIRQNum)
	}
	if lastLevel != 1 {
		t.Errorf("expected level 1, got %d", lastLevel)
	}

	// Test Set with level 0
	irq.Set(0)
	if callCount != 2 {
		t.Errorf("expected 2 calls, got %d", callCount)
	}
	if lastLevel != 0 {
		t.Errorf("expected level 0, got %d", lastLevel)
	}

	// Test Set with arbitrary level (PLIC may use other values)
	irq.Set(42)
	if callCount != 3 {
		t.Errorf("expected 3 calls, got %d", callCount)
	}
	if lastLevel != 42 {
		t.Errorf("expected level 42, got %d", lastLevel)
	}
}

// TestIRQSignalRaise tests the Raise method.
func TestIRQSignalRaise(t *testing.T) {
	var lastLevel int
	callCount := 0

	handler := func(opaque any, irqNum int, level int) {
		lastLevel = level
		callCount++
	}

	irq := NewIRQSignal(handler, nil, 3)

	irq.Raise()
	if callCount != 1 {
		t.Errorf("expected 1 call, got %d", callCount)
	}
	if lastLevel != 1 {
		t.Errorf("Raise should call Set(1), got level %d", lastLevel)
	}
}

// TestIRQSignalLower tests the Lower method.
func TestIRQSignalLower(t *testing.T) {
	var lastLevel int
	callCount := 0

	handler := func(opaque any, irqNum int, level int) {
		lastLevel = level
		callCount++
	}

	irq := NewIRQSignal(handler, nil, 3)

	irq.Lower()
	if callCount != 1 {
		t.Errorf("expected 1 call, got %d", callCount)
	}
	if lastLevel != 0 {
		t.Errorf("Lower should call Set(0), got level %d", lastLevel)
	}
}

// TestIRQSignalNilHandler tests that nil handler doesn't panic.
func TestIRQSignalNilHandler(t *testing.T) {
	irq := NewIRQSignal(nil, nil, 1)

	// These should not panic
	irq.Set(1)
	irq.Set(0)
	irq.Raise()
	irq.Lower()
}

// TestIRQSignalIRQNum tests the IRQNum method.
func TestIRQSignalIRQNum(t *testing.T) {
	tests := []int{0, 1, 5, 31, 100}

	for _, num := range tests {
		irq := NewIRQSignal(nil, nil, num)
		if got := irq.IRQNum(); got != num {
			t.Errorf("IRQNum() = %d, want %d", got, num)
		}
	}
}

// TestIRQSignalOpaqueTypes tests various opaque types.
func TestIRQSignalOpaqueTypes(t *testing.T) {
	var lastOpaque any
	handler := func(opaque any, irqNum int, level int) {
		lastOpaque = opaque
	}

	// Test with nil opaque
	irq1 := NewIRQSignal(handler, nil, 1)
	irq1.Set(1)
	if lastOpaque != nil {
		t.Errorf("expected nil opaque, got %v", lastOpaque)
	}

	// Test with int opaque
	irq2 := NewIRQSignal(handler, 42, 2)
	irq2.Set(1)
	if lastOpaque != 42 {
		t.Errorf("expected int opaque 42, got %v", lastOpaque)
	}

	// Test with pointer opaque
	type Device struct{ id int }
	dev := &Device{id: 123}
	irq3 := NewIRQSignal(handler, dev, 3)
	irq3.Set(1)
	if lastOpaque != dev {
		t.Errorf("expected device pointer, got %v", lastOpaque)
	}
	if d, ok := lastOpaque.(*Device); !ok || d.id != 123 {
		t.Errorf("expected Device{id: 123}, got %v", lastOpaque)
	}
}

// TestIRQSignalRaiseLowerSequence tests a typical raise/lower sequence.
func TestIRQSignalRaiseLowerSequence(t *testing.T) {
	levels := []int{}
	handler := func(opaque any, irqNum int, level int) {
		levels = append(levels, level)
	}

	irq := NewIRQSignal(handler, nil, 1)

	// Typical interrupt sequence: raise, then lower
	irq.Raise()
	irq.Lower()
	irq.Raise()
	irq.Lower()

	expected := []int{1, 0, 1, 0}
	if len(levels) != len(expected) {
		t.Fatalf("expected %d calls, got %d", len(expected), len(levels))
	}
	for i, exp := range expected {
		if levels[i] != exp {
			t.Errorf("call %d: expected level %d, got %d", i, exp, levels[i])
		}
	}
}

package slirp

import (
	"testing"
)

// TestNewMbuf tests mbuf creation.
func TestNewMbuf(t *testing.T) {
	s := NewSlirp()

	m := s.NewMbuf()
	if m == nil {
		t.Fatal("NewMbuf returned nil")
	}

	if m.Size <= 0 {
		t.Errorf("Size = %d, want > 0", m.Size)
	}
	if m.Len != 0 {
		t.Errorf("Len = %d, want 0", m.Len)
	}
	if m.Slirp != s {
		t.Error("Slirp reference not set")
	}
	if len(m.Data) != m.Size {
		t.Errorf("Data len = %d, Size = %d, should match", len(m.Data), m.Size)
	}
}

// TestMGet tests MGet alias.
func TestMGet(t *testing.T) {
	s := NewSlirp()

	m := s.MGet()
	if m == nil {
		t.Fatal("MGet returned nil")
	}
	if m.Size <= 0 {
		t.Errorf("Size = %d, want > 0", m.Size)
	}
}

// TestMbufFree tests mbuf freeing.
func TestMbufFree(t *testing.T) {
	s := NewSlirp()

	m := s.MGet()
	m.Free()

	// Should be able to get the mbuf again from free list
	m2 := s.MGet()
	if m2 == nil {
		t.Fatal("MGet returned nil after free")
	}
}

// TestMbufMFree tests MFree alias.
func TestMbufMFree(t *testing.T) {
	s := NewSlirp()

	m := s.MGet()
	m.MFree()

	// Should not panic
}

// TestMbufMFreem tests MFreem alias.
func TestMbufMFreem(t *testing.T) {
	s := NewSlirp()

	m := s.MGet()
	m.MFreem()

	// Should not panic
}

// TestMbufFreeNil tests freeing nil mbuf.
func TestMbufFreeNil(t *testing.T) {
	var m *Mbuf
	m.Free() // Should not panic
}

// TestMbufInc tests mbuf size increase.
func TestMbufInc(t *testing.T) {
	s := NewSlirp()

	m := s.MGet()
	origSize := m.Size

	// Write some data
	testData := []byte("hello world")
	copy(m.Data, testData)
	m.Len = len(testData)

	// Increase size
	newSize := origSize * 2
	m.Inc(newSize)

	if m.Size < newSize {
		t.Errorf("Size = %d, want >= %d", m.Size, newSize)
	}

	// Data should be preserved
	if string(m.Data[:m.Len]) != string(testData) {
		t.Errorf("Data not preserved after Inc")
	}
}

// TestMbufIncNoOp tests Inc when already large enough.
func TestMbufIncNoOp(t *testing.T) {
	s := NewSlirp()

	m := s.MGet()
	origSize := m.Size

	// Request smaller size - should be no-op
	m.Inc(100)

	if m.Size != origSize {
		t.Errorf("Size changed from %d to %d when it shouldn't", origSize, m.Size)
	}
}

// TestMbufMInc tests MInc alias.
func TestMbufMInc(t *testing.T) {
	s := NewSlirp()

	m := s.MGet()
	m.MInc(m.Size * 2)

	// Should not panic, size should increase
}

// TestMbufCat tests mbuf concatenation.
func TestMbufCat(t *testing.T) {
	s := NewSlirp()

	m1 := s.MGet()
	m2 := s.MGet()

	data1 := []byte("hello ")
	data2 := []byte("world")

	copy(m1.Data, data1)
	m1.Len = len(data1)

	copy(m2.Data, data2)
	m2.Len = len(data2)

	m1.Cat(m2)

	expected := "hello world"
	if string(m1.Data[:m1.Len]) != expected {
		t.Errorf("After Cat: got %q, want %q", string(m1.Data[:m1.Len]), expected)
	}
}

// TestMbufAdj tests mbuf adjustment.
func TestMbufAdj(t *testing.T) {
	s := NewSlirp()

	t.Run("trim from head", func(t *testing.T) {
		m := s.MGet()
		data := []byte("hello world")
		copy(m.Data, data)
		m.Len = len(data)

		m.Adj(6) // trim "hello "

		if string(m.Data[:m.Len]) != "world" {
			t.Errorf("After Adj(6): got %q, want %q", string(m.Data[:m.Len]), "world")
		}
	})

	t.Run("trim from tail", func(t *testing.T) {
		m := s.MGet()
		data := []byte("hello world")
		copy(m.Data, data)
		m.Len = len(data)

		m.Adj(-6) // trim " world"

		if m.Len != 5 {
			t.Errorf("After Adj(-6): Len = %d, want 5", m.Len)
		}
	})

	t.Run("nil mbuf", func(t *testing.T) {
		var m *Mbuf
		m.Adj(5) // Should not panic
	})

	t.Run("trim more than length from head", func(t *testing.T) {
		m := s.MGet()
		m.Len = 5
		m.Adj(10) // more than length
		if m.Len != 0 {
			t.Errorf("After Adj(10) on len=5: Len = %d, want 0", m.Len)
		}
	})

	t.Run("trim more than length from tail", func(t *testing.T) {
		m := s.MGet()
		m.Len = 5
		m.Adj(-10) // more than length
		if m.Len != 0 {
			t.Errorf("After Adj(-10) on len=5: Len = %d, want 0", m.Len)
		}
	})
}

// TestMbufCopy tests mbuf copy.
func TestMbufCopy(t *testing.T) {
	s := NewSlirp()

	t.Run("successful copy", func(t *testing.T) {
		src := s.MGet()
		dst := s.MGet()

		data := []byte("hello world")
		copy(src.Data, data)
		src.Len = len(data)

		// Copy "world" (offset 6, len 5)
		result := dst.Copy(src, 6, 5)
		if result != 0 {
			t.Errorf("Copy returned %d, want 0", result)
		}

		if string(dst.Data[:dst.Len]) != "world" {
			t.Errorf("After Copy: got %q, want %q", string(dst.Data[:dst.Len]), "world")
		}
	})

	t.Run("not enough room", func(t *testing.T) {
		src := s.MGet()
		dst := s.MGet()

		data := []byte("hello")
		copy(src.Data, data)
		src.Len = len(data)

		// Fill dst so there's no room
		dst.Len = dst.Size

		result := dst.Copy(src, 0, 5)
		if result != -1 {
			t.Errorf("Copy returned %d, want -1", result)
		}
	})
}

// TestMbufFreeRoom tests FreeRoom function.
func TestMbufFreeRoom(t *testing.T) {
	s := NewSlirp()

	m := s.MGet()
	m.Len = 100

	expected := m.Size - 100
	if m.FreeRoom() != expected {
		t.Errorf("FreeRoom = %d, want %d", m.FreeRoom(), expected)
	}
}

// TestMbufRoom tests Room function.
func TestMbufRoom(t *testing.T) {
	s := NewSlirp()

	m := s.MGet()
	if m.Room() != m.Size {
		t.Errorf("Room = %d, want %d", m.Room(), m.Size)
	}
}

// TestSlirpMSize tests the mbuf size calculation.
func TestSlirpMSize(t *testing.T) {
	size := SlirpMSize()
	// Should be at least MTU + header overhead
	if size < IFMTU {
		t.Errorf("SlirpMSize = %d, want >= %d", size, IFMTU)
	}
}

// TestMbufThreshold tests mbuf allocation above threshold.
func TestMbufThreshold(t *testing.T) {
	s := NewSlirp()

	// Allocate many mbufs to exceed threshold
	mbufs := make([]*Mbuf, MbufThresh+5)
	for i := range mbufs {
		mbufs[i] = s.MGet()
	}

	// Mbufs above threshold should have MBufDoFree flag
	// The actual flag setting depends on allocation order
	if s.MbufAlloced <= MbufThresh {
		t.Errorf("MbufAlloced = %d, want > %d", s.MbufAlloced, MbufThresh)
	}

	// Free all mbufs
	for _, m := range mbufs {
		m.Free()
	}
}

// TestMbufFreeListReuse tests that freed mbufs are reused.
func TestMbufFreeListReuse(t *testing.T) {
	s := NewSlirp()

	m1 := s.MGet()
	m1.Free()

	initialAlloced := s.MbufAlloced

	m2 := s.MGet()
	_ = m2

	// Should have reused the freed mbuf, not allocated new one
	if s.MbufAlloced != initialAlloced {
		t.Errorf("MbufAlloced = %d, want %d (should reuse freed mbuf)", s.MbufAlloced, initialAlloced)
	}
}

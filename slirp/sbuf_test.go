package slirp

import (
	"bytes"
	"testing"
)

// TestSbufReserve tests SbReserve allocation.
// Reference: tinyemu-2019-12-21/slirp/sbuf.c:35-55
func TestSbufReserve(t *testing.T) {
	var sb SBuf

	// Initial allocation
	sb.SbReserve(100)
	if sb.SbDataLen != 100 {
		t.Errorf("SbDataLen = %d, want 100", sb.SbDataLen)
	}
	if sb.SbCC != 0 {
		t.Errorf("SbCC = %d, want 0", sb.SbCC)
	}
	if sb.SbWPtr != 0 {
		t.Errorf("SbWPtr = %d, want 0", sb.SbWPtr)
	}
	if sb.SbRPtr != 0 {
		t.Errorf("SbRPtr = %d, want 0", sb.SbRPtr)
	}
	if len(sb.SbData) != 100 {
		t.Errorf("len(SbData) = %d, want 100", len(sb.SbData))
	}

	// Re-allocation to same size should not change anything
	sb.SbCC = 50
	sb.SbWPtr = 50
	sb.SbReserve(100)
	if sb.SbCC != 50 {
		t.Errorf("SbCC after same-size reserve = %d, want 50", sb.SbCC)
	}

	// Re-allocation to different size should reset
	sb.SbReserve(200)
	if sb.SbDataLen != 200 {
		t.Errorf("SbDataLen after resize = %d, want 200", sb.SbDataLen)
	}
	if sb.SbCC != 0 {
		t.Errorf("SbCC after resize = %d, want 0", sb.SbCC)
	}
	if sb.SbWPtr != 0 {
		t.Errorf("SbWPtr after resize = %d, want 0", sb.SbWPtr)
	}
}

// TestSbufFree tests SbFree deallocation.
// Reference: tinyemu-2019-12-21/slirp/sbuf.c:13-16
func TestSbufFree(t *testing.T) {
	var sb SBuf
	sb.SbReserve(100)
	sb.SbCC = 50
	sb.SbWPtr = 50

	sb.SbFree()

	if sb.SbData != nil {
		t.Error("SbData should be nil after SbFree")
	}
	if sb.SbCC != 0 {
		t.Errorf("SbCC = %d, want 0 after SbFree", sb.SbCC)
	}
	if sb.SbDataLen != 0 {
		t.Errorf("SbDataLen = %d, want 0 after SbFree", sb.SbDataLen)
	}
}

// TestSbufSpace tests SbSpace calculation.
// Reference: tinyemu-2019-12-21/slirp/sbuf.h:12
func TestSbufSpace(t *testing.T) {
	var sb SBuf
	sb.SbReserve(100)

	if sb.SbSpace() != 100 {
		t.Errorf("SbSpace with empty buffer = %d, want 100", sb.SbSpace())
	}

	sb.SbCC = 30
	if sb.SbSpace() != 70 {
		t.Errorf("SbSpace with 30 bytes = %d, want 70", sb.SbSpace())
	}

	sb.SbCC = 100
	if sb.SbSpace() != 0 {
		t.Errorf("SbSpace with full buffer = %d, want 0", sb.SbSpace())
	}
}

// TestSbufDrop tests SbDrop functionality.
// Reference: tinyemu-2019-12-21/slirp/sbuf.c:19-32
func TestSbufDrop(t *testing.T) {
	var sb SBuf
	sb.SbReserve(100)
	sb.SbCC = 50
	sb.SbWPtr = 50
	sb.SbRPtr = 0

	// Drop 20 bytes
	sb.SbDrop(20)
	if sb.SbCC != 30 {
		t.Errorf("SbCC after drop 20 = %d, want 30", sb.SbCC)
	}
	if sb.SbRPtr != 20 {
		t.Errorf("SbRPtr after drop 20 = %d, want 20", sb.SbRPtr)
	}

	// Drop more than available (should clamp)
	sb.SbDrop(100)
	if sb.SbCC != 0 {
		t.Errorf("SbCC after over-drop = %d, want 0", sb.SbCC)
	}
	if sb.SbRPtr != 50 {
		t.Errorf("SbRPtr after over-drop = %d, want 50", sb.SbRPtr)
	}
}

// TestSbufDropWrap tests SbDrop with wraparound.
// Reference: tinyemu-2019-12-21/slirp/sbuf.c:29-30
func TestSbufDropWrap(t *testing.T) {
	var sb SBuf
	sb.SbReserve(100)
	sb.SbCC = 50
	sb.SbRPtr = 80 // Near end of buffer
	sb.SbWPtr = 30 // Wrapped around

	// Drop 30 bytes - should wrap around
	sb.SbDrop(30)
	if sb.SbCC != 20 {
		t.Errorf("SbCC after wrap drop = %d, want 20", sb.SbCC)
	}
	if sb.SbRPtr != 10 {
		t.Errorf("SbRPtr after wrap drop = %d, want 10 (wrapped)", sb.SbRPtr)
	}
}

// TestSbufFlush tests SbFlush.
// Reference: tinyemu-2019-12-21/slirp/sbuf.h:11
func TestSbufFlush(t *testing.T) {
	var sb SBuf
	sb.SbReserve(100)
	sb.SbCC = 50
	sb.SbWPtr = 50

	sb.SbFlush()

	if sb.SbCC != 0 {
		t.Errorf("SbCC after flush = %d, want 0", sb.SbCC)
	}
}

// TestSbufAppendSb tests sbAppendSb with contiguous space.
// Reference: tinyemu-2019-12-21/slirp/sbuf.c:124-153
func TestSbufAppendSb(t *testing.T) {
	var sb SBuf
	sb.SbReserve(100)

	// Create an mbuf with test data
	m := &Mbuf{
		Data: []byte("Hello, World!"),
		Len:  13,
	}

	sb.sbAppendSb(m)

	if sb.SbCC != 13 {
		t.Errorf("SbCC = %d, want 13", sb.SbCC)
	}
	if sb.SbWPtr != 13 {
		t.Errorf("SbWPtr = %d, want 13", sb.SbWPtr)
	}
	if !bytes.Equal(sb.SbData[:13], []byte("Hello, World!")) {
		t.Errorf("SbData = %q, want %q", sb.SbData[:13], "Hello, World!")
	}
}

// TestSbufAppendSbWrap tests sbAppendSb with wraparound.
// Reference: tinyemu-2019-12-21/slirp/sbuf.c:134-146
func TestSbufAppendSbWrap(t *testing.T) {
	var sb SBuf
	sb.SbReserve(20)

	// Set up state where wptr is near end of buffer
	sb.SbWPtr = 15
	sb.SbRPtr = 5 // 10 bytes already dropped
	sb.SbCC = 10  // 10 bytes in buffer (from pos 5 to 15)

	// Now write 8 more bytes - should wrap
	m := &Mbuf{
		Data: []byte("ABCDEFGH"),
		Len:  8,
	}

	sb.sbAppendSb(m)

	// Should have written 5 bytes at end (15-20) and 3 at beginning (0-3)
	if sb.SbCC != 18 {
		t.Errorf("SbCC = %d, want 18", sb.SbCC)
	}
	if sb.SbWPtr != 3 { // wrapped around
		t.Errorf("SbWPtr = %d, want 3", sb.SbWPtr)
	}

	// Check the data
	if !bytes.Equal(sb.SbData[15:20], []byte("ABCDE")) {
		t.Errorf("SbData[15:20] = %q, want %q", sb.SbData[15:20], "ABCDE")
	}
	if !bytes.Equal(sb.SbData[0:3], []byte("FGH")) {
		t.Errorf("SbData[0:3] = %q, want %q", sb.SbData[0:3], "FGH")
	}
}

// TestSbufAppendSbWPtrBeforeRPtr tests sbAppendSb when wptr < rptr.
// Reference: tinyemu-2019-12-21/slirp/sbuf.c:130-133
func TestSbufAppendSbWPtrBeforeRPtr(t *testing.T) {
	var sb SBuf
	sb.SbReserve(20)

	// Set up state where wptr is before rptr (wrapped state)
	sb.SbWPtr = 5
	sb.SbRPtr = 15
	sb.SbCC = 10 // 10 bytes in buffer (15-20 and 0-5)

	// Write data - should fit between wptr and rptr
	m := &Mbuf{
		Data: []byte("TEST"),
		Len:  4,
	}

	sb.sbAppendSb(m)

	if sb.SbCC != 14 {
		t.Errorf("SbCC = %d, want 14", sb.SbCC)
	}
	if sb.SbWPtr != 9 {
		t.Errorf("SbWPtr = %d, want 9", sb.SbWPtr)
	}
	if !bytes.Equal(sb.SbData[5:9], []byte("TEST")) {
		t.Errorf("SbData[5:9] = %q, want %q", sb.SbData[5:9], "TEST")
	}
}

// TestSbufCopySimple tests SbCopy with contiguous data.
// Reference: tinyemu-2019-12-21/slirp/sbuf.c:161-181
func TestSbufCopySimple(t *testing.T) {
	var sb SBuf
	sb.SbReserve(100)

	// Put data in buffer
	copy(sb.SbData, []byte("Hello, World!"))
	sb.SbCC = 13
	sb.SbWPtr = 13
	sb.SbRPtr = 0

	// Copy all data
	buf := make([]byte, 20)
	sb.SbCopy(0, 13, buf)

	if !bytes.Equal(buf[:13], []byte("Hello, World!")) {
		t.Errorf("SbCopy result = %q, want %q", buf[:13], "Hello, World!")
	}

	// Copy with offset
	sb.SbCopy(7, 5, buf)
	if !bytes.Equal(buf[:5], []byte("World")) {
		t.Errorf("SbCopy with offset = %q, want %q", buf[:5], "World")
	}
}

// TestSbufCopyWrap tests SbCopy with wrapped data.
// Reference: tinyemu-2019-12-21/slirp/sbuf.c:172-179
func TestSbufCopyWrap(t *testing.T) {
	var sb SBuf
	sb.SbReserve(20)

	// Set up wrapped data: "FGHIJ" at positions 0-5, "ABCDE" at positions 15-20
	copy(sb.SbData[15:], []byte("ABCDE"))
	copy(sb.SbData[0:], []byte("FGHIJ"))
	sb.SbRPtr = 15
	sb.SbWPtr = 5
	sb.SbCC = 10

	// Copy all 10 bytes - should get "ABCDEFGHIJ"
	buf := make([]byte, 10)
	sb.SbCopy(0, 10, buf)

	if !bytes.Equal(buf, []byte("ABCDEFGHIJ")) {
		t.Errorf("SbCopy wrapped = %q, want %q", buf, "ABCDEFGHIJ")
	}
}

// TestSbufCopyWrapWithOffset tests SbCopy with wrapped data and offset.
// Reference: tinyemu-2019-12-21/slirp/sbuf.c:165-167
func TestSbufCopyWrapWithOffset(t *testing.T) {
	var sb SBuf
	sb.SbReserve(20)

	// Set up wrapped data
	copy(sb.SbData[15:], []byte("ABCDE"))
	copy(sb.SbData[0:], []byte("FGHIJ"))
	sb.SbRPtr = 15
	sb.SbWPtr = 5
	sb.SbCC = 10

	// Copy from offset 3 (should start at "DE" and wrap to "FGHIJ")
	buf := make([]byte, 7)
	sb.SbCopy(3, 7, buf)

	if !bytes.Equal(buf, []byte("DEFGHIJ")) {
		t.Errorf("SbCopy with offset wrapped = %q, want %q", buf, "DEFGHIJ")
	}
}

// TestSbufCopyLengthClamp tests that SbCopy clamps length to available data.
// Reference: tinyemu-2019-12-21/slirp/sbuf.c:170
func TestSbufCopyLengthClamp(t *testing.T) {
	var sb SBuf
	sb.SbReserve(100)

	copy(sb.SbData, []byte("Short"))
	sb.SbCC = 5
	sb.SbWPtr = 5
	sb.SbRPtr = 0

	// Request more than available - should clamp
	buf := make([]byte, 20)
	sb.SbCopy(0, 20, buf)

	// Should only copy 5 bytes (the actual content)
	if !bytes.Equal(buf[:5], []byte("Short")) {
		t.Errorf("SbCopy clamped = %q, want %q", buf[:5], "Short")
	}
}

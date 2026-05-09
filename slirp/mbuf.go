package slirp

// Mbuf constants
// Reference: tinyemu-2019-12-21/slirp/mbuf.h
const (
	// MinCSize is the amount to increase mbuf if too small
	MinCSize = 4096

	// Mbuf flags
	MBufExt      = 0x01 // m_ext points to more (malloced) data
	MBufFreeList = 0x02 // mbuf is on free list
	MBufUsedList = 0x04 // mbuf is on used list (for dtom())
	MBufDoFree   = 0x08 // when m_free is called, free() it

	// Interface constants
	// Reference: tinyemu-2019-12-21/slirp/if.h
	IFMTU        = 1500
	IFMRU        = 1500
	IFMaxLinkHdr = 2 + 14 + 40 // 2 for alignment, 14 for ethernet, 40 for TCP/IP

	// MbufThresh is the threshold above which mbufs are marked for freeing
	MbufThresh = 30
)

// SlirpMSize calculates the size of an mbuf including header space.
// Reference: tinyemu-2019-12-21/slirp/mbuf.c:26
func SlirpMSize() int {
	// SLIRP_MSIZE = IF_MTU + IF_MAXLINKHDR + offsetof(struct mbuf, m_dat) + 6
	// In Go, we use a simpler calculation since our Mbuf struct is different
	return IFMTU + IFMaxLinkHdr + 64 + 6 // 64 is approximate header overhead
}

// Mbuf represents a message buffer for network data.
// The design matches C mbuf semantics:
//   - Dat is the backing buffer (equivalent to m_dat/m_ext)
//   - Data is a view starting at Offset: Dat[Offset:]
//   - Offset tracks where valid data starts within Dat (like m_data - m_dat)
//   - Len is the length of valid data (like m_len)
//   - Size is the total buffer size (like m_size)
//
// This allows adjusting the data pointer without copying:
//   - Trim from head: Offset += n, Len -= n, Data = Dat[Offset:]
//   - Trim from tail: Len -= n
//   - Prepend: Offset -= n (if room), Len += n, Data = Dat[Offset:]
//
// Reference: tinyemu-2019-12-21/slirp/mbuf.h:85-92
type Mbuf struct {
	// Linked list pointers (from m_hdr)
	Next    *Mbuf // next mbuf in list
	Prev    *Mbuf // previous mbuf in list
	NextPkt *Mbuf // next packet in queue
	PrevPkt *Mbuf // previous packet

	// Flags and size
	Flags int // misc flags
	Size  int // total size of backing buffer (len(Dat))

	// Socket association
	So *Socket // associated socket

	// Data management - matches C m_dat/m_data/m_len semantics
	Dat    []byte // backing buffer (equivalent to m_dat/m_ext in C)
	Data   []byte // view of buffer from Offset: Dat[Offset:]
	Offset int    // offset of valid data start within Dat (m_data - m_dat)
	Len    int    // length of valid data (m_len)

	// Slirp context
	Slirp *Slirp // managing slirp instance
}

// NewMbuf creates a new mbuf with the default size.
// This is the Go equivalent of m_get().
// Reference: tinyemu-2019-12-21/slirp/mbuf.c:43-76
func (s *Slirp) NewMbuf() *Mbuf {
	var m *Mbuf
	flags := 0

	// Check if there's an mbuf on the free list
	if s.MFreeList.Next == &s.MFreeList {
		// Allocate new mbuf
		dat := make([]byte, SlirpMSize())
		m = &Mbuf{
			Dat:   dat,
			Data:  dat, // Data starts at beginning of Dat
			Slirp: s,
		}
		s.MbufAlloced++
		if s.MbufAlloced > MbufThresh {
			flags = MBufDoFree
		}
	} else {
		// Get from free list
		m = s.MFreeList.Next
		mbufRemque(m)
	}

	// Insert in used list
	mbufInsque(m, &s.MUsedList)
	m.Flags = flags | MBufUsedList

	// Initialize - match C: m->m_data = m->m_dat; m->m_len = 0;
	m.Size = len(m.Dat)
	m.Offset = 0
	m.Data = m.Dat // Data = Dat[0:]
	m.Len = 0
	m.NextPkt = nil
	m.PrevPkt = nil

	return m
}

// MGet is an alias for NewMbuf for API compatibility with C code.
// Reference: tinyemu-2019-12-21/slirp/mbuf.c:43-76
func (s *Slirp) MGet() *Mbuf {
	return s.NewMbuf()
}

// Free frees the mbuf.
// Reference: tinyemu-2019-12-21/slirp/mbuf.c:78-105
func (m *Mbuf) Free() {
	if m == nil {
		return
	}

	// Remove from used list
	if m.Flags&MBufUsedList != 0 {
		mbufRemque(m)
	}

	// If it's MBufDoFree, actually free it (in Go, just let GC handle it)
	if m.Flags&MBufDoFree != 0 {
		m.Slirp.MbufAlloced--
		// Let GC handle the actual memory
		return
	}

	// Put on free list if not already there
	if m.Flags&MBufFreeList == 0 {
		mbufInsque(m, &m.Slirp.MFreeList)
		m.Flags = MBufFreeList
	}
}

// MFree is an alias for Free (m_free and m_freem are the same in slirp).
// Reference: tinyemu-2019-12-21/slirp/mbuf.h:36
func (m *Mbuf) MFree() {
	m.Free()
}

// MFreem is an alias for Free (m_free and m_freem are the same in slirp).
// Reference: tinyemu-2019-12-21/slirp/mbuf.h:36
func (m *Mbuf) MFreem() {
	m.Free()
}

// Inc increases the mbuf size to at least 'size' bytes.
// Preserves the offset of data within the buffer, matching C semantics:
//
//	datasize = m->m_data - m->m_dat;  // save offset
//	memcpy(dat, m->m_dat, m->m_size); // copy ENTIRE buffer
//	m->m_data = m->m_ext + datasize;  // restore offset
//
// Reference: tinyemu-2019-12-21/slirp/mbuf.c:129-154
func (m *Mbuf) Inc(size int) {
	if m.Size >= size {
		return
	}

	// Allocate new buffer and copy ENTIRE old buffer (preserves offset)
	// C: memcpy(dat, m->m_dat, m->m_size);
	newDat := make([]byte, size)
	copy(newDat, m.Dat[:m.Size])

	// Update pointers, preserving offset
	// C: m->m_data = m->m_ext + datasize;
	m.Dat = newDat
	m.Data = m.Dat[m.Offset:]
	m.Size = size
}

// MInc is an alias for Inc.
func (m *Mbuf) MInc(size int) {
	m.Inc(size)
}

// Cat appends mbuf n to mbuf m.
// Reference: tinyemu-2019-12-21/slirp/mbuf.c:112-125
func (m *Mbuf) Cat(n *Mbuf) {
	// If there's no room, realloc
	if m.FreeRoom() < n.Len {
		m.Inc(m.Size + MinCSize)
	}

	copy(m.Data[m.Len:], n.Data[:n.Len])
	m.Len += n.Len

	n.Free()
}

// Adj adjusts the mbuf by trimming len bytes.
// Positive len trims from head (advances data pointer), negative from tail.
// Unlike the old implementation that copied data, this just adjusts the
// offset pointer, matching C semantics: m->m_data += len; m->m_len -= len;
// Reference: tinyemu-2019-12-21/slirp/mbuf.c:158-172
func (m *Mbuf) Adj(len int) {
	if m == nil {
		return
	}
	if len >= 0 {
		// Trim from head - just adjust offset, don't copy
		// C: m->m_data += len; m->m_len -= len;
		if len <= m.Len {
			m.Offset += len
			m.Len -= len
			m.Data = m.Dat[m.Offset:]
		} else {
			m.Offset += m.Len
			m.Len = 0
			m.Data = m.Dat[m.Offset:]
		}
	} else {
		// Trim from tail
		// C: m->m_len -= len; (len is already negative)
		len = -len
		if len <= m.Len {
			m.Len -= len
		} else {
			m.Len = 0
		}
	}
}

// Copy copies len bytes from mbuf m starting at offset off to mbuf n.
// Reference: tinyemu-2019-12-21/slirp/mbuf.c:178-187
func (n *Mbuf) Copy(m *Mbuf, off, length int) int {
	if n.FreeRoom() < length {
		return -1
	}

	copy(n.Data[n.Len:], m.Data[off:off+length])
	n.Len += length
	return 0
}

// FreeRoom returns the free space in the mbuf (from end of valid data to end of buffer).
// C: M_FREEROOM(m) = M_ROOM(m) - m->m_len
//
//	M_ROOM(m) = (m->m_dat + m->m_size) - m->m_data = Size - Offset
//
// So FreeRoom = Size - Offset - Len
// Reference: tinyemu-2019-12-21/slirp/mbuf.h:82
func (m *Mbuf) FreeRoom() int {
	return m.Size - m.Offset - m.Len
}

// Room returns the room in the mbuf from the data start to end of buffer.
// C: M_ROOM(m) = (m->m_dat + m->m_size) - m->m_data = Size - Offset
// Reference: tinyemu-2019-12-21/slirp/mbuf.h:74-77
func (m *Mbuf) Room() int {
	return m.Size - m.Offset
}

// SetDataOffset sets the data start offset within the backing buffer.
// This updates both Offset and Data to maintain the invariant Data = Dat[Offset:].
// Equivalent to C's m_data = m_dat + offset.
func (m *Mbuf) SetDataOffset(offset int) {
	m.Offset = offset
	m.Data = m.Dat[offset:]
}

// ResetData resets the data pointer to the beginning of the backing buffer.
// This is equivalent to C's m_data = m_dat.
func (m *Mbuf) ResetData() {
	m.Offset = 0
	m.Data = m.Dat
}

// Prepend moves the data pointer backward by n bytes, expanding the valid data region.
// This is the inverse of Adj for positive values - it "un-trims" from the head.
// The trimmed data is still in the buffer and becomes valid again.
// Returns true if successful, false if there's not enough room to prepend.
// Equivalent to C's: m->m_data -= n; m->m_len += n;
func (m *Mbuf) Prepend(n int) bool {
	if n > m.Offset {
		return false // Can't prepend more than we have trimmed
	}
	m.Offset -= n
	m.Len += n
	m.Data = m.Dat[m.Offset:]
	return true
}

// SetBuffer replaces the mbuf's backing buffer with a new one.
// This should be used when code needs to completely replace the buffer,
// rather than directly assigning to m.Data which would break the Dat/Data invariant.
// The offset is reset to 0, and Len is left unchanged (caller should set it).
func (m *Mbuf) SetBuffer(data []byte) {
	m.Dat = data
	m.Data = data
	m.Offset = 0
	m.Size = len(data)
}

// mbufInsque inserts element after head in the mbuf list.
// This is similar to Insque but works with Mbuf pointers.
func mbufInsque(element, head *Mbuf) {
	element.Next = head.Next
	head.Next = element
	element.Prev = head
	if element.Next != nil {
		element.Next.Prev = element
	}
}

// mbufRemque removes element from the mbuf list.
func mbufRemque(element *Mbuf) {
	if element.Next != nil {
		element.Next.Prev = element.Prev
	}
	if element.Prev != nil {
		element.Prev.Next = element.Next
	}
	element.Prev = nil
}

// MInit initializes mbuf lists for a Slirp instance.
// Reference: tinyemu-2019-12-21/slirp/mbuf.c:28-33
func (s *Slirp) MInit() {
	s.MFreeList.Next = &s.MFreeList
	s.MFreeList.Prev = &s.MFreeList
	s.MUsedList.Next = &s.MUsedList
	s.MUsedList.Prev = &s.MUsedList
}

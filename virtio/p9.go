// Package virtio provides VirtIO device emulation.
// This file implements the VirtIO 9P filesystem device.
//
// Reference: TinyEMU virtio.c lines 1696-2649

package virtio

import (
	"encoding/binary"
	"sync"

	"github.com/sorins/tinyemu-go/mem"
	"github.com/sorins/tinyemu-go/p9"
)

// VirtIO 9P feature bits
const (
	Virtio9PMountTag = 1 << 0 // Device provides mount tag
)

// P9Device is a VirtIO 9P filesystem device.
type P9Device struct {
	dev      *Device
	fs       p9.FSDevice
	mountTag string
	msize    uint32

	mu      sync.Mutex
	fids    map[uint32]p9.File
	nextFid uint32
}

// NewP9Device creates a new VirtIO 9P filesystem device.
// The mountTag is the tag used to mount the filesystem in the guest.
// Reference: tinyemu-2019-12-21/virtio.c:2624-2649 (virtio_9p_init)
func NewP9Device(memMap *mem.PhysMemoryMap, addr uint64, irq *mem.IRQSignal,
	fs p9.FSDevice, mountTag string) (*P9Device, error) {

	pd := &P9Device{
		fs:       fs,
		mountTag: mountTag,
		msize:    8192,
		fids:     make(map[uint32]p9.File),
	}

	// Config space: 2-byte length + mount tag
	configSize := uint32(2 + len(mountTag))
	var err error
	pd.dev, err = NewDevice(memMap, addr, irq, DeviceID9P, configSize, pd.recvRequest)
	if err != nil {
		return nil, err
	}

	// Set device features
	pd.dev.Features = Virtio9PMountTag

	// Set mount tag in config space
	config := pd.dev.ConfigSpace[:]
	binary.LittleEndian.PutUint16(config[0:2], uint16(len(mountTag)))
	copy(config[2:], mountTag)

	return pd, nil
}

// Device returns the underlying VirtIO device.
func (pd *P9Device) Device() *Device {
	return pd.dev
}

// MountTag returns the mount tag.
func (pd *P9Device) MountTag() string {
	return pd.mountTag
}

// Close closes the 9P device and all open files.
func (pd *P9Device) Close() error {
	pd.mu.Lock()
	defer pd.mu.Unlock()

	for fid, f := range pd.fids {
		pd.fs.Clunk(f)
		delete(pd.fids, fid)
	}

	return nil
}

// recvRequest handles incoming 9P requests.
// Reference: tinyemu-2019-12-21/virtio.c:2042-2083 (virtio_9p_recv_request setup)
func (pd *P9Device) recvRequest(dev *Device, queueIdx int, descIdx int, readSize int, writeSize int) int {
	// Reference: tinyemu-2019-12-21/virtio.c:2054-2055
	if queueIdx != 0 {
		return 0
	}

	// Note: C has req_in_progress check here (line 2057-2058) for async ops.
	// Go is synchronous, so this check is not needed.

	// Read 9P message header (7 bytes: size[4] + type[1] + tag[2])
	// Reference: tinyemu-2019-12-21/virtio.c:2060-2069
	var header [p9.HeaderSize]byte
	if err := dev.MemcpyFromQueue(header[:], queueIdx, descIdx, 0, p9.HeaderSize); err != nil {
		// C sends protocol error with tag=0 on header read failure
		// Reference: tinyemu-2019-12-21/virtio.c:2062-2064
		pd.sendError(dev, queueIdx, descIdx, 0, p9.EPROTO)
		return 0
	}

	h, err := p9.ParseHeader(header[:])
	if err != nil {
		pd.sendError(dev, queueIdx, descIdx, 0, p9.EPROTO)
		return 0
	}

	// Read full message body
	bodySize := int(h.Size) - p9.HeaderSize
	body := make([]byte, bodySize)
	if bodySize > 0 {
		if err := dev.MemcpyFromQueue(body, queueIdx, descIdx, p9.HeaderSize, bodySize); err != nil {
			pd.sendError(dev, queueIdx, descIdx, h.Tag, p9.EPROTO)
			return 0
		}
	}

	pd.handleMessage(dev, queueIdx, descIdx, h.Type, h.Tag, body)
	return 0
}

// handleMessage dispatches 9P messages to appropriate handlers.
func (pd *P9Device) handleMessage(dev *Device, queueIdx, descIdx int, msgType uint8, tag uint16, body []byte) {
	dec := p9.NewDecoder(body)

	switch msgType {
	case p9.Tversion:
		pd.handleVersion(dev, queueIdx, descIdx, tag, dec)

	case p9.Tattach:
		pd.handleAttach(dev, queueIdx, descIdx, tag, dec)

	case p9.Twalk:
		pd.handleWalk(dev, queueIdx, descIdx, tag, dec)

	case p9.Tlopen:
		pd.handleLopen(dev, queueIdx, descIdx, tag, dec)

	case p9.Tlcreate:
		pd.handleLcreate(dev, queueIdx, descIdx, tag, dec)

	case p9.Tread:
		pd.handleRead(dev, queueIdx, descIdx, tag, dec)

	case p9.Twrite:
		pd.handleWrite(dev, queueIdx, descIdx, tag, dec)

	case p9.Tclunk:
		pd.handleClunk(dev, queueIdx, descIdx, tag, dec)

	case p9.Tstatfs:
		pd.handleStatfs(dev, queueIdx, descIdx, tag, dec)

	case p9.Tgetattr:
		pd.handleGetattr(dev, queueIdx, descIdx, tag, dec)

	case p9.Tsetattr:
		pd.handleSetattr(dev, queueIdx, descIdx, tag, dec)

	case p9.Treaddir:
		pd.handleReaddir(dev, queueIdx, descIdx, tag, dec)

	case p9.Tmkdir:
		pd.handleMkdir(dev, queueIdx, descIdx, tag, dec)

	case p9.Tunlinkat:
		pd.handleUnlinkat(dev, queueIdx, descIdx, tag, dec)

	case p9.Trenameat:
		pd.handleRenameat(dev, queueIdx, descIdx, tag, dec)

	case p9.Tsymlink:
		pd.handleSymlink(dev, queueIdx, descIdx, tag, dec)

	case p9.Tmknod:
		pd.handleMknod(dev, queueIdx, descIdx, tag, dec)

	case p9.Treadlink:
		pd.handleReadlink(dev, queueIdx, descIdx, tag, dec)

	case p9.Tlink:
		pd.handleLink(dev, queueIdx, descIdx, tag, dec)

	case p9.Tfsync:
		pd.handleFsync(dev, queueIdx, descIdx, tag, dec)

	case p9.Tflush:
		pd.handleFlush(dev, queueIdx, descIdx, tag, dec)

	case p9.Tlock:
		pd.handleLock(dev, queueIdx, descIdx, tag, dec)

	case p9.Tgetlock:
		pd.handleGetlock(dev, queueIdx, descIdx, tag, dec)

	default:
		pd.sendError(dev, queueIdx, descIdx, tag, p9.ENOTSUP)
	}
}

// sendReply sends a 9P reply message.
// Reference: tinyemu-2019-12-21/virtio.c:1967-1990
func (pd *P9Device) sendReply(dev *Device, queueIdx, descIdx int, msgType uint8, tag uint16, body []byte) {
	msgSize := p9.HeaderSize + len(body)
	msg := make([]byte, msgSize)

	binary.LittleEndian.PutUint32(msg[0:4], uint32(msgSize))
	msg[4] = msgType + 1 // Response type is request type + 1
	binary.LittleEndian.PutUint16(msg[5:7], tag)
	if len(body) > 0 {
		copy(msg[p9.HeaderSize:], body)
	}

	dev.MemcpyToQueue(queueIdx, descIdx, 0, msg, msgSize)
	dev.ConsumeDesc(queueIdx, descIdx, msgSize)
}

// sendError sends a 9P error response.
// Uses Rlerror (type 7) for 9P2000.L compatibility.
// Reference: tinyemu-2019-12-21/virtio.c:1992-2000
func (pd *P9Device) sendError(dev *Device, queueIdx, descIdx int, tag uint16, ecode uint32) {
	body := make([]byte, 4)
	binary.LittleEndian.PutUint32(body, ecode)

	msgSize := p9.HeaderSize + 4
	msg := make([]byte, msgSize)

	binary.LittleEndian.PutUint32(msg[0:4], uint32(msgSize))
	msg[4] = p9.Rlerror
	binary.LittleEndian.PutUint16(msg[5:7], tag)
	copy(msg[p9.HeaderSize:], body)

	dev.MemcpyToQueue(queueIdx, descIdx, 0, msg, msgSize)
	dev.ConsumeDesc(queueIdx, descIdx, msgSize)
}

// getFid returns the file associated with a fid.
// Reference: tinyemu-2019-12-21/virtio.c:1676-1684 (fid_find)
func (pd *P9Device) getFid(fid uint32) (p9.File, bool) {
	pd.mu.Lock()
	defer pd.mu.Unlock()
	f, ok := pd.fids[fid]
	return f, ok
}

// setFid sets the file for a fid.
// If the fid already exists, the old file is closed first.
// Reference: tinyemu-2019-12-21/virtio.c:1698-1712 (fid_set)
func (pd *P9Device) setFid(fid uint32, f p9.File) {
	pd.mu.Lock()
	defer pd.mu.Unlock()
	if oldF, ok := pd.fids[fid]; ok {
		pd.fs.Clunk(oldF)
	}
	pd.fids[fid] = f
}

// deleteFid removes and returns the file for a fid.
// Note: Unlike C's fid_delete, this returns the file without closing it,
// allowing the caller to close it after the lock is released.
// Reference: tinyemu-2019-12-21/virtio.c:1686-1696 (fid_delete)
func (pd *P9Device) deleteFid(fid uint32) (p9.File, bool) {
	pd.mu.Lock()
	defer pd.mu.Unlock()
	f, ok := pd.fids[fid]
	if ok {
		delete(pd.fids, fid)
	}
	return f, ok
}

// Message handlers

// handleVersion handles the 9P version negotiation.
// Reference: tinyemu-2019-12-21/virtio.c:2455-2468 (case 100)
func (pd *P9Device) handleVersion(dev *Device, queueIdx, descIdx int, tag uint16, dec *p9.Decoder) {
	msg, err := p9.ParseTversion(dec)
	if err != nil {
		pd.sendError(dev, queueIdx, descIdx, tag, p9.EPROTO)
		return
	}

	// Accept client's msize (matching C behavior: s->msize = msize)
	pd.msize = msg.Msize

	// Send response
	buf := make([]byte, 256)
	enc := p9.NewEncoder(buf)
	p9.MarshalRversion(enc, p9.RversionMsg{Msize: pd.msize, Version: "9P2000.L"})

	pd.sendReply(dev, queueIdx, descIdx, p9.Tversion, tag, enc.Bytes())
}

// handleAttach handles the 9P attach request.
// Reference: tinyemu-2019-12-21/virtio.c:2469-2488 (case 104)
func (pd *P9Device) handleAttach(dev *Device, queueIdx, descIdx int, tag uint16, dec *p9.Decoder) {
	msg, err := p9.ParseTattach(dec)
	if err != nil {
		pd.sendError(dev, queueIdx, descIdx, tag, p9.EPROTO)
		return
	}

	// Attach to filesystem
	f, qid, err := pd.fs.Attach(msg.NUname, msg.Uname, msg.Aname)
	if err != nil {
		pd.sendError(dev, queueIdx, descIdx, tag, p9.ErrorCode(err))
		return
	}

	// Store fid
	pd.setFid(msg.Fid, f)

	// Send response
	buf := make([]byte, 32)
	enc := p9.NewEncoder(buf)
	p9.MarshalRattach(enc, p9.RattachMsg{QID: qid})

	pd.sendReply(dev, queueIdx, descIdx, p9.Tattach, tag, enc.Bytes())
}

// handleWalk handles the 9P walk request.
// Reference: tinyemu-2019-12-21/virtio.c:2499-2542 (case 110)
func (pd *P9Device) handleWalk(dev *Device, queueIdx, descIdx int, tag uint16, dec *p9.Decoder) {
	msg, err := p9.ParseTwalk(dec)
	if err != nil {
		pd.sendError(dev, queueIdx, descIdx, tag, p9.EPROTO)
		return
	}

	f, ok := pd.getFid(msg.Fid)
	if !ok {
		pd.sendError(dev, queueIdx, descIdx, tag, p9.EPROTO)
		return
	}

	// Walk
	newF, qids, err := pd.fs.Walk(f, msg.Names)
	if err != nil {
		pd.sendError(dev, queueIdx, descIdx, tag, p9.ErrorCode(err))
		return
	}

	// Store new fid
	pd.setFid(msg.NewFid, newF)

	// Send response
	buf := make([]byte, 2+len(qids)*13)
	enc := p9.NewEncoder(buf)
	p9.MarshalRwalk(enc, p9.RwalkMsg{QIDs: qids})

	pd.sendReply(dev, queueIdx, descIdx, p9.Twalk, tag, enc.Bytes())
}

// handleLopen handles the 9P lopen operation.
// Reference: tinyemu-2019-12-21/virtio.c:2104-2129
// Note: C implementation has async support via fs_open callback; Go is synchronous.
func (pd *P9Device) handleLopen(dev *Device, queueIdx, descIdx int, tag uint16, dec *p9.Decoder) {
	msg, err := p9.ParseTlopen(dec)
	if err != nil {
		pd.sendError(dev, queueIdx, descIdx, tag, p9.EPROTO)
		return
	}

	f, ok := pd.getFid(msg.Fid)
	if !ok {
		pd.sendError(dev, queueIdx, descIdx, tag, p9.EPROTO)
		return
	}

	// Open
	qid, iounit, err := pd.fs.Open(f, msg.Flags)
	if err != nil {
		pd.sendError(dev, queueIdx, descIdx, tag, p9.ErrorCode(err))
		return
	}

	// If iounit is 0, use msize - 24 (header overhead)
	if iounit == 0 {
		iounit = pd.msize - 24
	}

	// Send response
	buf := make([]byte, 32)
	enc := p9.NewEncoder(buf)
	p9.MarshalRlopen(enc, p9.RlopenMsg{QID: qid, IOUnit: iounit})

	pd.sendReply(dev, queueIdx, descIdx, p9.Tlopen, tag, enc.Bytes())
}

// handleLcreate handles the 9P lcreate operation.
// Reference: tinyemu-2019-12-21/virtio.c:2130-2153
func (pd *P9Device) handleLcreate(dev *Device, queueIdx, descIdx int, tag uint16, dec *p9.Decoder) {
	msg, err := p9.ParseTlcreate(dec)
	if err != nil {
		pd.sendError(dev, queueIdx, descIdx, tag, p9.EPROTO)
		return
	}

	f, ok := pd.getFid(msg.Fid)
	if !ok {
		pd.sendError(dev, queueIdx, descIdx, tag, p9.EPROTO)
		return
	}

	// Create
	qid, iounit, err := pd.fs.Create(f, msg.Name, msg.Flags, msg.Mode, msg.GID)
	if err != nil {
		pd.sendError(dev, queueIdx, descIdx, tag, p9.ErrorCode(err))
		return
	}

	// C code always uses msize - 24 for iounit in lcreate
	if iounit == 0 {
		iounit = pd.msize - 24
	}

	// Send response
	buf := make([]byte, 32)
	enc := p9.NewEncoder(buf)
	p9.MarshalRlopen(enc, p9.RlopenMsg{QID: qid, IOUnit: iounit})

	pd.sendReply(dev, queueIdx, descIdx, p9.Tlcreate, tag, enc.Bytes())
}

func (pd *P9Device) handleRead(dev *Device, queueIdx, descIdx int, tag uint16, dec *p9.Decoder) {
	msg, err := p9.ParseTread(dec)
	if err != nil {
		pd.sendError(dev, queueIdx, descIdx, tag, p9.EPROTO)
		return
	}

	f, ok := pd.getFid(msg.Fid)
	if !ok {
		pd.sendError(dev, queueIdx, descIdx, tag, p9.EPROTO)
		return
	}

	// Read
	data, err := pd.fs.Read(f, msg.Offset, msg.Count)
	if err != nil {
		pd.sendError(dev, queueIdx, descIdx, tag, p9.ErrorCode(err))
		return
	}

	// Send response
	buf := make([]byte, 4+len(data))
	enc := p9.NewEncoder(buf)
	p9.MarshalRread(enc, data)

	pd.sendReply(dev, queueIdx, descIdx, p9.Tread, tag, enc.Bytes())
}

func (pd *P9Device) handleWrite(dev *Device, queueIdx, descIdx int, tag uint16, dec *p9.Decoder) {
	msg, err := p9.ParseTwrite(dec)
	if err != nil {
		pd.sendError(dev, queueIdx, descIdx, tag, p9.EPROTO)
		return
	}

	f, ok := pd.getFid(msg.Fid)
	if !ok {
		pd.sendError(dev, queueIdx, descIdx, tag, p9.EPROTO)
		return
	}

	// Write
	count, err := pd.fs.Write(f, msg.Offset, msg.Data)
	if err != nil {
		pd.sendError(dev, queueIdx, descIdx, tag, p9.ErrorCode(err))
		return
	}

	// Send response
	buf := make([]byte, 4)
	enc := p9.NewEncoder(buf)
	p9.MarshalRwrite(enc, count)

	pd.sendReply(dev, queueIdx, descIdx, p9.Twrite, tag, enc.Bytes())
}

func (pd *P9Device) handleClunk(dev *Device, queueIdx, descIdx int, tag uint16, dec *p9.Decoder) {
	msg, err := p9.ParseTclunk(dec)
	if err != nil {
		pd.sendError(dev, queueIdx, descIdx, tag, p9.EPROTO)
		return
	}

	f, ok := pd.deleteFid(msg.Fid)
	if !ok {
		pd.sendError(dev, queueIdx, descIdx, tag, p9.EPROTO)
		return
	}

	// Clunk
	pd.fs.Clunk(f)

	// Send empty response
	pd.sendReply(dev, queueIdx, descIdx, p9.Tclunk, tag, nil)
}

// handleStatfs handles the 9P statfs request.
// Reference: tinyemu-2019-12-21/virtio.c:2084-2103 (case 8 statfs)
func (pd *P9Device) handleStatfs(dev *Device, queueIdx, descIdx int, tag uint16, dec *p9.Decoder) {
	// Statfs - note: C does not parse fid, statfs is global
	st, err := pd.fs.Statfs()
	if err != nil {
		pd.sendError(dev, queueIdx, descIdx, tag, p9.ErrorCode(err))
		return
	}

	// Send response
	// C format: "wwddddddw" = type[4], bsize[4], blocks[8], bfree[8], bavail[8],
	// files[8], ffree[8], fsid[8], namelen[4]
	// Note: C hardcodes type=0, fsid=0, namelen=256 in virtio.c:2089-2099
	// The FSDevice implementation should return appropriate values.
	buf := make([]byte, 64)
	enc := p9.NewEncoder(buf)
	p9.MarshalRstatfs(enc, st)

	pd.sendReply(dev, queueIdx, descIdx, p9.Tstatfs, tag, enc.Bytes())
}

func (pd *P9Device) handleGetattr(dev *Device, queueIdx, descIdx int, tag uint16, dec *p9.Decoder) {
	msg, err := p9.ParseTgetattr(dec)
	if err != nil {
		pd.sendError(dev, queueIdx, descIdx, tag, p9.EPROTO)
		return
	}

	f, ok := pd.getFid(msg.Fid)
	if !ok {
		pd.sendError(dev, queueIdx, descIdx, tag, p9.EPROTO)
		return
	}

	// Getattr
	stat, valid, err := pd.fs.Getattr(f, msg.ReqMask)
	if err != nil {
		pd.sendError(dev, queueIdx, descIdx, tag, p9.ErrorCode(err))
		return
	}

	// Send response
	buf := make([]byte, 256)
	enc := p9.NewEncoder(buf)
	p9.MarshalRgetattr(enc, p9.RgetattrMsg{Valid: valid, Stat: stat})

	pd.sendReply(dev, queueIdx, descIdx, p9.Tgetattr, tag, enc.Bytes())
}

func (pd *P9Device) handleSetattr(dev *Device, queueIdx, descIdx int, tag uint16, dec *p9.Decoder) {
	msg, err := p9.ParseTsetattr(dec)
	if err != nil {
		pd.sendError(dev, queueIdx, descIdx, tag, p9.EPROTO)
		return
	}

	f, ok := pd.getFid(msg.Fid)
	if !ok {
		pd.sendError(dev, queueIdx, descIdx, tag, p9.EPROTO)
		return
	}

	// Setattr
	err = pd.fs.Setattr(f, msg.Valid, msg.Mode, msg.UID, msg.GID, msg.Size,
		msg.AtimeSec, msg.AtimeNsec, msg.MtimeSec, msg.MtimeNsec)
	if err != nil {
		pd.sendError(dev, queueIdx, descIdx, tag, p9.ErrorCode(err))
		return
	}

	// Send empty response
	pd.sendReply(dev, queueIdx, descIdx, p9.Tsetattr, tag, nil)
}

func (pd *P9Device) handleReaddir(dev *Device, queueIdx, descIdx int, tag uint16, dec *p9.Decoder) {
	msg, err := p9.ParseTreaddir(dec)
	if err != nil {
		pd.sendError(dev, queueIdx, descIdx, tag, p9.EPROTO)
		return
	}

	f, ok := pd.getFid(msg.Fid)
	if !ok {
		pd.sendError(dev, queueIdx, descIdx, tag, p9.EPROTO)
		return
	}

	// Readdir
	data, err := pd.fs.Readdir(f, msg.Offset, msg.Count)
	if err != nil {
		pd.sendError(dev, queueIdx, descIdx, tag, p9.ErrorCode(err))
		return
	}

	// Send response (same format as Rread)
	buf := make([]byte, 4+len(data))
	enc := p9.NewEncoder(buf)
	p9.MarshalRread(enc, data)

	pd.sendReply(dev, queueIdx, descIdx, p9.Treaddir, tag, enc.Bytes())
}

func (pd *P9Device) handleMkdir(dev *Device, queueIdx, descIdx int, tag uint16, dec *p9.Decoder) {
	msg, err := p9.ParseTmkdir(dec)
	if err != nil {
		pd.sendError(dev, queueIdx, descIdx, tag, p9.EPROTO)
		return
	}

	f, ok := pd.getFid(msg.DFid)
	if !ok {
		pd.sendError(dev, queueIdx, descIdx, tag, p9.EPROTO)
		return
	}

	// Mkdir
	qid, err := pd.fs.Mkdir(f, msg.Name, msg.Mode, msg.GID)
	if err != nil {
		pd.sendError(dev, queueIdx, descIdx, tag, p9.ErrorCode(err))
		return
	}

	// Send response
	buf := make([]byte, 16)
	enc := p9.NewEncoder(buf)
	p9.MarshalRmkdir(enc, qid)

	pd.sendReply(dev, queueIdx, descIdx, p9.Tmkdir, tag, enc.Bytes())
}

func (pd *P9Device) handleUnlinkat(dev *Device, queueIdx, descIdx int, tag uint16, dec *p9.Decoder) {
	msg, err := p9.ParseTunlinkat(dec)
	if err != nil {
		pd.sendError(dev, queueIdx, descIdx, tag, p9.EPROTO)
		return
	}

	f, ok := pd.getFid(msg.DFid)
	if !ok {
		pd.sendError(dev, queueIdx, descIdx, tag, p9.EPROTO)
		return
	}

	// Unlinkat
	err = pd.fs.Unlinkat(f, msg.Name, msg.Flags)
	if err != nil {
		pd.sendError(dev, queueIdx, descIdx, tag, p9.ErrorCode(err))
		return
	}

	// Send empty response
	pd.sendReply(dev, queueIdx, descIdx, p9.Tunlinkat, tag, nil)
}

func (pd *P9Device) handleRenameat(dev *Device, queueIdx, descIdx int, tag uint16, dec *p9.Decoder) {
	msg, err := p9.ParseTrenameat(dec)
	if err != nil {
		pd.sendError(dev, queueIdx, descIdx, tag, p9.EPROTO)
		return
	}

	oldF, ok := pd.getFid(msg.OldDFid)
	if !ok {
		pd.sendError(dev, queueIdx, descIdx, tag, p9.EPROTO)
		return
	}

	newF, ok := pd.getFid(msg.NewDFid)
	if !ok {
		pd.sendError(dev, queueIdx, descIdx, tag, p9.EPROTO)
		return
	}

	// Renameat
	err = pd.fs.Renameat(oldF, msg.OldName, newF, msg.NewName)
	if err != nil {
		pd.sendError(dev, queueIdx, descIdx, tag, p9.ErrorCode(err))
		return
	}

	// Send empty response
	pd.sendReply(dev, queueIdx, descIdx, p9.Trenameat, tag, nil)
}

// handleSymlink handles the 9P symlink operation.
// Reference: tinyemu-2019-12-21/virtio.c:2154-2178
func (pd *P9Device) handleSymlink(dev *Device, queueIdx, descIdx int, tag uint16, dec *p9.Decoder) {
	msg, err := p9.ParseTsymlink(dec)
	if err != nil {
		pd.sendError(dev, queueIdx, descIdx, tag, p9.EPROTO)
		return
	}

	f, ok := pd.getFid(msg.Fid)
	if !ok {
		pd.sendError(dev, queueIdx, descIdx, tag, p9.EPROTO)
		return
	}

	// Symlink
	qid, err := pd.fs.Symlink(f, msg.Name, msg.Target, msg.GID)
	if err != nil {
		pd.sendError(dev, queueIdx, descIdx, tag, p9.ErrorCode(err))
		return
	}

	// Send response with QID
	buf := make([]byte, 16)
	enc := p9.NewEncoder(buf)
	enc.PutQID(qid)

	pd.sendReply(dev, queueIdx, descIdx, p9.Tsymlink, tag, enc.Bytes())
}

// handleMknod handles the 9P mknod operation.
// Reference: tinyemu-2019-12-21/virtio.c:2179-2202
func (pd *P9Device) handleMknod(dev *Device, queueIdx, descIdx int, tag uint16, dec *p9.Decoder) {
	msg, err := p9.ParseTmknod(dec)
	if err != nil {
		pd.sendError(dev, queueIdx, descIdx, tag, p9.EPROTO)
		return
	}

	f, ok := pd.getFid(msg.DFid)
	if !ok {
		pd.sendError(dev, queueIdx, descIdx, tag, p9.EPROTO)
		return
	}

	// Mknod
	qid, err := pd.fs.Mknod(f, msg.Name, msg.Mode, msg.Major, msg.Minor, msg.GID)
	if err != nil {
		pd.sendError(dev, queueIdx, descIdx, tag, p9.ErrorCode(err))
		return
	}

	// Send response with QID
	buf := make([]byte, 16)
	enc := p9.NewEncoder(buf)
	enc.PutQID(qid)

	pd.sendReply(dev, queueIdx, descIdx, p9.Tmknod, tag, enc.Bytes())
}

// handleReadlink handles the 9P readlink operation.
// Reference: tinyemu-2019-12-21/virtio.c:2203-2223
// Note: C uses 1024-byte buffer; Go uses dynamic string.
func (pd *P9Device) handleReadlink(dev *Device, queueIdx, descIdx int, tag uint16, dec *p9.Decoder) {
	msg, err := p9.ParseTreadlink(dec)
	if err != nil {
		pd.sendError(dev, queueIdx, descIdx, tag, p9.EPROTO)
		return
	}

	f, ok := pd.getFid(msg.Fid)
	if !ok {
		pd.sendError(dev, queueIdx, descIdx, tag, p9.EPROTO)
		return
	}

	// Readlink
	target, err := pd.fs.Readlink(f)
	if err != nil {
		pd.sendError(dev, queueIdx, descIdx, tag, p9.ErrorCode(err))
		return
	}

	// Send response with target string
	buf := make([]byte, 2+len(target))
	enc := p9.NewEncoder(buf)
	p9.MarshalRreadlink(enc, target)

	pd.sendReply(dev, queueIdx, descIdx, p9.Treadlink, tag, enc.Bytes())
}

func (pd *P9Device) handleLink(dev *Device, queueIdx, descIdx int, tag uint16, dec *p9.Decoder) {
	msg, err := p9.ParseTlink(dec)
	if err != nil {
		pd.sendError(dev, queueIdx, descIdx, tag, p9.EPROTO)
		return
	}

	dfid, ok := pd.getFid(msg.DFid)
	if !ok {
		pd.sendError(dev, queueIdx, descIdx, tag, p9.EPROTO)
		return
	}

	fid, ok := pd.getFid(msg.Fid)
	if !ok {
		pd.sendError(dev, queueIdx, descIdx, tag, p9.EPROTO)
		return
	}

	// Link
	err = pd.fs.Link(dfid, fid, msg.Name)
	if err != nil {
		pd.sendError(dev, queueIdx, descIdx, tag, p9.ErrorCode(err))
		return
	}

	// Send empty response
	pd.sendReply(dev, queueIdx, descIdx, p9.Tlink, tag, nil)
}

// handleFsync handles fsync requests.
// Reference: tinyemu-2019-12-21/virtio.c:2308-2317
// Note: C TinyEMU ignores fsync - it just parses the fid and sends an empty reply.
func (pd *P9Device) handleFsync(dev *Device, queueIdx, descIdx int, tag uint16, dec *p9.Decoder) {
	_, err := p9.ParseTfsync(dec)
	if err != nil {
		pd.sendError(dev, queueIdx, descIdx, tag, p9.EPROTO)
		return
	}

	// C TinyEMU ignores fsync - just send empty response
	// Reference: tinyemu-2019-12-21/virtio.c:2314 "/* ignored */"
	pd.sendReply(dev, queueIdx, descIdx, p9.Tfsync, tag, nil)
}

// handleFlush handles the 9P flush request.
// Reference: tinyemu-2019-12-21/virtio.c:2489-2498 (case 108)
func (pd *P9Device) handleFlush(dev *Device, queueIdx, descIdx int, tag uint16, dec *p9.Decoder) {
	// Parse oldtag (matching C behavior - parse even though ignored)
	_, err := p9.ParseTflush(dec)
	if err != nil {
		pd.sendError(dev, queueIdx, descIdx, tag, p9.EPROTO)
		return
	}
	// Ignored - we don't have async operations
	pd.sendReply(dev, queueIdx, descIdx, p9.Tflush, tag, nil)
}

func (pd *P9Device) handleLock(dev *Device, queueIdx, descIdx int, tag uint16, dec *p9.Decoder) {
	msg, err := p9.ParseTlock(dec)
	if err != nil {
		pd.sendError(dev, queueIdx, descIdx, tag, p9.EPROTO)
		return
	}

	f, ok := pd.getFid(msg.Fid)
	if !ok {
		pd.sendError(dev, queueIdx, descIdx, tag, p9.EPROTO)
		return
	}

	lock := &p9.Lock{
		Type:     msg.Type,
		Flags:    msg.Flags,
		Start:    msg.Start,
		Length:   msg.Length,
		ProcID:   msg.ProcID,
		ClientID: msg.ClientID,
	}

	status, err := pd.fs.Lock(f, lock)
	if err != nil {
		pd.sendError(dev, queueIdx, descIdx, tag, p9.ErrorCode(err))
		return
	}

	// Send response
	buf := make([]byte, 1)
	enc := p9.NewEncoder(buf)
	p9.MarshalRlock(enc, status)

	pd.sendReply(dev, queueIdx, descIdx, p9.Tlock, tag, enc.Bytes())
}

func (pd *P9Device) handleGetlock(dev *Device, queueIdx, descIdx int, tag uint16, dec *p9.Decoder) {
	msg, err := p9.ParseTgetlock(dec)
	if err != nil {
		pd.sendError(dev, queueIdx, descIdx, tag, p9.EPROTO)
		return
	}

	f, ok := pd.getFid(msg.Fid)
	if !ok {
		pd.sendError(dev, queueIdx, descIdx, tag, p9.EPROTO)
		return
	}

	lock := &p9.Lock{
		Type:     msg.Type,
		Start:    msg.Start,
		Length:   msg.Length,
		ProcID:   msg.ProcID,
		ClientID: msg.ClientID,
	}

	result, err := pd.fs.Getlock(f, lock)
	if err != nil {
		pd.sendError(dev, queueIdx, descIdx, tag, p9.ErrorCode(err))
		return
	}

	// Send response
	buf := make([]byte, 256)
	enc := p9.NewEncoder(buf)
	p9.MarshalRgetlock(enc, p9.RgetlockMsg{
		Type:     result.Type,
		Start:    result.Start,
		Length:   result.Length,
		ProcID:   result.ProcID,
		ClientID: result.ClientID,
	})

	pd.sendReply(dev, queueIdx, descIdx, p9.Tgetlock, tag, enc.Bytes())
}

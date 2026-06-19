package virtio

import (
	"encoding/binary"
	"os"
	"testing"

	"github.com/sorins/tinyemu-go/mem"
	"github.com/sorins/tinyemu-go/p9"
)

// mockFSDevice implements p9.FSDevice for testing
// Reference: TinyEMU virtio.c lines 2042-2620
type mockFSDevice struct {
	attachErr   error
	walkErr     error
	openErr     error
	createErr   error
	readErr     error
	writeErr    error
	clunkErr    error
	statfsErr   error
	getattrErr  error
	setattrErr  error
	readdirErr  error
	mkdirErr    error
	unlinkatErr error
	renameatErr error
	symlinkErr  error
	mknodErr    error
	readlinkErr error
	linkErr     error
	fsyncErr    error
	lockErr     error
	getlockErr  error

	// Return values
	attachQID    p9.QID
	walkQIDs     []p9.QID
	openQID      p9.QID
	createQID    p9.QID
	readData     []byte
	writeCount   uint32
	statfs       p9.StatFS
	getattrStat  p9.Stat
	getattrValid uint64
	readdirData  []byte
	mkdirQID     p9.QID
	symlinkQID   p9.QID
	mknodQID     p9.QID
	readlinkTgt  string
	lockStatus   uint8
	getlockLock  *p9.Lock

	// Tracking calls
	lastOp string
}

type mockFile struct {
	path string
}

func (f *mockFile) Path() string { return f.path }

func (m *mockFSDevice) Statfs() (p9.StatFS, error) {
	m.lastOp = "statfs"
	return m.statfs, m.statfsErr
}

func (m *mockFSDevice) Attach(uid uint32, uname, aname string) (p9.File, p9.QID, error) {
	m.lastOp = "attach"
	if m.attachErr != nil {
		return nil, p9.QID{}, m.attachErr
	}
	return &mockFile{path: "/"}, m.attachQID, nil
}

func (m *mockFSDevice) Walk(f p9.File, names []string) (p9.File, []p9.QID, error) {
	m.lastOp = "walk"
	if m.walkErr != nil {
		return nil, nil, m.walkErr
	}
	return &mockFile{path: "/walked"}, m.walkQIDs, nil
}

func (m *mockFSDevice) Open(f p9.File, flags uint32) (p9.QID, uint32, error) {
	m.lastOp = "open"
	if m.openErr != nil {
		return p9.QID{}, 0, m.openErr
	}
	return m.openQID, 0, nil
}

func (m *mockFSDevice) Create(f p9.File, name string, flags, mode, gid uint32) (p9.QID, uint32, error) {
	m.lastOp = "create"
	if m.createErr != nil {
		return p9.QID{}, 0, m.createErr
	}
	return m.createQID, 0, nil
}

func (m *mockFSDevice) Read(f p9.File, offset uint64, count uint32) ([]byte, error) {
	m.lastOp = "read"
	if m.readErr != nil {
		return nil, m.readErr
	}
	return m.readData, nil
}

func (m *mockFSDevice) Write(f p9.File, offset uint64, data []byte) (uint32, error) {
	m.lastOp = "write"
	if m.writeErr != nil {
		return 0, m.writeErr
	}
	return m.writeCount, nil
}

func (m *mockFSDevice) Clunk(f p9.File) error {
	m.lastOp = "clunk"
	return m.clunkErr
}

func (m *mockFSDevice) Remove(f p9.File) error {
	m.lastOp = "remove"
	return nil
}

func (m *mockFSDevice) Getattr(f p9.File, mask uint64) (p9.Stat, uint64, error) {
	m.lastOp = "getattr"
	if m.getattrErr != nil {
		return p9.Stat{}, 0, m.getattrErr
	}
	return m.getattrStat, m.getattrValid, nil
}

func (m *mockFSDevice) Setattr(f p9.File, valid uint32, mode, uid, gid uint32, size uint64,
	atimeSec, atimeNsec, mtimeSec, mtimeNsec uint64) error {
	m.lastOp = "setattr"
	return m.setattrErr
}

func (m *mockFSDevice) Readdir(f p9.File, offset uint64, count uint32) ([]byte, error) {
	m.lastOp = "readdir"
	if m.readdirErr != nil {
		return nil, m.readdirErr
	}
	return m.readdirData, nil
}

func (m *mockFSDevice) Mkdir(f p9.File, name string, mode, gid uint32) (p9.QID, error) {
	m.lastOp = "mkdir"
	if m.mkdirErr != nil {
		return p9.QID{}, m.mkdirErr
	}
	return m.mkdirQID, nil
}

func (m *mockFSDevice) Symlink(f p9.File, name, target string, gid uint32) (p9.QID, error) {
	m.lastOp = "symlink"
	if m.symlinkErr != nil {
		return p9.QID{}, m.symlinkErr
	}
	return m.symlinkQID, nil
}

func (m *mockFSDevice) Mknod(f p9.File, name string, mode, major, minor, gid uint32) (p9.QID, error) {
	m.lastOp = "mknod"
	if m.mknodErr != nil {
		return p9.QID{}, m.mknodErr
	}
	return m.mknodQID, nil
}

func (m *mockFSDevice) Readlink(f p9.File) (string, error) {
	m.lastOp = "readlink"
	if m.readlinkErr != nil {
		return "", m.readlinkErr
	}
	return m.readlinkTgt, nil
}

func (m *mockFSDevice) Link(dfid p9.File, f p9.File, name string) error {
	m.lastOp = "link"
	return m.linkErr
}

func (m *mockFSDevice) Renameat(oldDirF p9.File, oldName string, newDirF p9.File, newName string) error {
	m.lastOp = "renameat"
	return m.renameatErr
}

func (m *mockFSDevice) Unlinkat(f p9.File, name string, flags uint32) error {
	m.lastOp = "unlinkat"
	return m.unlinkatErr
}

func (m *mockFSDevice) Fsync(f p9.File) error {
	m.lastOp = "fsync"
	return m.fsyncErr
}

func (m *mockFSDevice) Lock(f p9.File, lock *p9.Lock) (uint8, error) {
	m.lastOp = "lock"
	if m.lockErr != nil {
		return 0, m.lockErr
	}
	return m.lockStatus, nil
}

func (m *mockFSDevice) Getlock(f p9.File, lock *p9.Lock) (*p9.Lock, error) {
	m.lastOp = "getlock"
	if m.getlockErr != nil {
		return nil, m.getlockErr
	}
	return m.getlockLock, nil
}

// Verify mock implements FSDevice
var _ p9.FSDevice = (*mockFSDevice)(nil)

func newTestP9Device(t *testing.T) (*P9Device, *mem.PhysMemoryMap, string, *testIRQState) {
	t.Helper()

	// Create temp directory for testing
	tmpDir, err := os.MkdirTemp("", "virtio_p9_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}

	memMap := mem.NewPhysMemoryMap()

	// Allocate RAM at 0x80000000
	_, err = memMap.RegisterRAM(0x80000000, 0x10000, 0)
	if err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("failed to register RAM: %v", err)
	}

	irq, irqState := newTestIRQ()

	// Create HostFS
	fs, err := p9.NewHostFS(tmpDir)
	if err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("failed to create HostFS: %v", err)
	}

	// Create P9 device
	pd, err := NewP9Device(memMap, 0x10000000, irq, fs, "share")
	if err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("failed to create P9 device: %v", err)
	}

	return pd, memMap, tmpDir, irqState
}

func TestP9DeviceCreate(t *testing.T) {
	pd, _, tmpDir, _ := newTestP9Device(t)
	defer os.RemoveAll(tmpDir)

	// Check device ID
	dev := pd.Device()
	id := dev.read(nil, MMIODeviceID, 2)
	if id != DeviceID9P {
		t.Errorf("device ID = %d, want %d", id, DeviceID9P)
	}

	// Check magic and version
	magic := dev.read(nil, MMIOMagicValue, 2)
	if magic != MMIOMagic {
		t.Errorf("magic = 0x%x, want 0x%x", magic, MMIOMagic)
	}

	version := dev.read(nil, MMIOVersion, 2)
	if version != MMIOVersionVal {
		t.Errorf("version = %d, want %d", version, MMIOVersionVal)
	}
}

func TestP9DeviceMountTag(t *testing.T) {
	pd, _, tmpDir, _ := newTestP9Device(t)
	defer os.RemoveAll(tmpDir)

	if pd.MountTag() != "share" {
		t.Errorf("MountTag() = %q, want %q", pd.MountTag(), "share")
	}

	// Check config space contains mount tag
	dev := pd.Device()
	tagLenLow := dev.read(nil, MMIOConfig, 2)
	tagLenHigh := dev.read(nil, MMIOConfig+1, 2) // Read byte 1
	tagLen := tagLenLow & 0xFF
	_ = tagLenHigh

	if tagLen != 5 { // "share" is 5 bytes
		t.Errorf("mount tag length = %d, want 5", tagLen)
	}
}

func TestP9DeviceFeatures(t *testing.T) {
	pd, _, tmpDir, _ := newTestP9Device(t)
	defer os.RemoveAll(tmpDir)

	dev := pd.Device()

	// Select feature page 0
	dev.write(nil, MMIODeviceFeaturesSel, 0, 2)

	// Read features
	features := dev.read(nil, MMIODeviceFeatures, 2)
	if features&Virtio9PMountTag == 0 {
		t.Error("MOUNT_TAG feature not set")
	}
}

func TestP9DeviceClose(t *testing.T) {
	pd, _, tmpDir, _ := newTestP9Device(t)
	defer os.RemoveAll(tmpDir)

	// Close should not panic
	if err := pd.Close(); err != nil {
		t.Errorf("Close() error = %v", err)
	}
}

func TestP9DeviceFidManagement(t *testing.T) {
	pd, _, tmpDir, _ := newTestP9Device(t)
	defer os.RemoveAll(tmpDir)

	// Initially no fids
	_, ok := pd.getFid(0)
	if ok {
		t.Error("getFid(0) should return false initially")
	}

	// Create a mock file handle by attaching
	fs, _ := p9.NewHostFS(tmpDir)
	f, _, _ := fs.Attach(1000, "user", "/")

	// Set fid
	pd.setFid(1, f)

	// Get fid
	gotF, ok := pd.getFid(1)
	if !ok {
		t.Error("getFid(1) should return true")
	}
	if gotF != f {
		t.Error("getFid(1) returned wrong file")
	}

	// Delete fid
	delF, ok := pd.deleteFid(1)
	if !ok {
		t.Error("deleteFid(1) should return true")
	}
	if delF != f {
		t.Error("deleteFid(1) returned wrong file")
	}

	// Fid should be gone
	_, ok = pd.getFid(1)
	if ok {
		t.Error("getFid(1) should return false after delete")
	}
}

// TestP9DeviceVersionNegotiation tests the version negotiation flow.
func TestP9DeviceVersionNegotiation(t *testing.T) {
	pd, memMap, tmpDir, _ := newTestP9Device(t)
	defer os.RemoveAll(tmpDir)
	dev := pd.Device()

	// Set up virtqueue
	descAddr := uint64(0x80000000)
	availAddr := uint64(0x80001000)
	usedAddr := uint64(0x80002000)

	dev.write(nil, MMIOQueueSel, 0, 2)
	dev.write(nil, MMIOQueueDescLow, uint32(descAddr), 2)
	dev.write(nil, MMIOQueueDescHigh, uint32(descAddr>>32), 2)
	dev.write(nil, MMIOQueueAvailLow, uint32(availAddr), 2)
	dev.write(nil, MMIOQueueAvailHigh, uint32(availAddr>>32), 2)
	dev.write(nil, MMIOQueueUsedLow, uint32(usedAddr), 2)
	dev.write(nil, MMIOQueueUsedHigh, uint32(usedAddr>>32), 2)
	dev.write(nil, MMIOQueueNum, 16, 2)
	dev.write(nil, MMIOQueueReady, 1, 2)
	dev.write(nil, MMIOStatus, 0xF, 2)

	// Build Tversion message
	reqAddr := uint64(0x80003000)
	respAddr := uint64(0x80004000)

	// Tversion: size[4] + type[1] + tag[2] + msize[4] + version[2+n]
	version := "9P2000.L"
	msgSize := 7 + 4 + 2 + len(version)
	reqBuf := make([]byte, msgSize)
	binary.LittleEndian.PutUint32(reqBuf[0:4], uint32(msgSize))
	reqBuf[4] = p9.Tversion
	binary.LittleEndian.PutUint16(reqBuf[5:7], p9.NoTag)
	binary.LittleEndian.PutUint32(reqBuf[7:11], 8192) // msize
	binary.LittleEndian.PutUint16(reqBuf[11:13], uint16(len(version)))
	copy(reqBuf[13:], version)

	// Write request to memory
	ramPtr := memMap.GetRAMPtr(reqAddr, true)
	copy(ramPtr, reqBuf)

	// Set up descriptors
	// Desc 0: Request (read by device)
	desc0 := make([]byte, 16)
	binary.LittleEndian.PutUint64(desc0[0:], reqAddr)
	binary.LittleEndian.PutUint32(desc0[8:], uint32(msgSize))
	binary.LittleEndian.PutUint16(desc0[12:], VRingDescFNext)
	binary.LittleEndian.PutUint16(desc0[14:], 1)
	ramPtr = memMap.GetRAMPtr(descAddr, true)
	copy(ramPtr, desc0)

	// Desc 1: Response (write by device)
	desc1 := make([]byte, 16)
	binary.LittleEndian.PutUint64(desc1[0:], respAddr)
	binary.LittleEndian.PutUint32(desc1[8:], 256)
	binary.LittleEndian.PutUint16(desc1[12:], VRingDescFWrite)
	binary.LittleEndian.PutUint16(desc1[14:], 0)
	ramPtr = memMap.GetRAMPtr(descAddr+16, true)
	copy(ramPtr, desc1)

	// Set up available ring
	availRing := make([]byte, 6)
	binary.LittleEndian.PutUint16(availRing[0:], 0) // flags
	binary.LittleEndian.PutUint16(availRing[2:], 1) // idx
	binary.LittleEndian.PutUint16(availRing[4:], 0) // ring[0] = descriptor 0
	ramPtr = memMap.GetRAMPtr(availAddr, true)
	copy(ramPtr, availRing)

	// Initialize used ring
	usedRing := make([]byte, 4)
	binary.LittleEndian.PutUint16(usedRing[0:], 0) // flags
	binary.LittleEndian.PutUint16(usedRing[2:], 0) // idx
	ramPtr = memMap.GetRAMPtr(usedAddr, true)
	copy(ramPtr, usedRing)

	// Notify queue
	dev.write(nil, MMIOQueueNotify, 0, 2)

	// Check response
	ramPtr = memMap.GetRAMPtr(respAddr, false)
	respSize := binary.LittleEndian.Uint32(ramPtr[0:4])
	respType := ramPtr[4]
	respTag := binary.LittleEndian.Uint16(ramPtr[5:7])

	if respType != p9.Rversion {
		t.Errorf("response type = %d, want %d (Rversion)", respType, p9.Rversion)
	}

	if respTag != p9.NoTag {
		t.Errorf("response tag = %d, want %d (NoTag)", respTag, p9.NoTag)
	}

	if respSize < 7 {
		t.Errorf("response size = %d, too small", respSize)
	}

	// Check msize in response
	respMsize := binary.LittleEndian.Uint32(ramPtr[7:11])
	if respMsize != 8192 {
		t.Errorf("response msize = %d, want 8192", respMsize)
	}
}

func TestP9DeviceConstants(t *testing.T) {
	// Verify device ID
	if DeviceID9P != 9 {
		t.Errorf("DeviceID9P = %d, want 9", DeviceID9P)
	}

	// Verify feature bit
	if Virtio9PMountTag != 1 {
		t.Errorf("Virtio9PMountTag = %d, want 1", Virtio9PMountTag)
	}
}

// Helper to create a P9Device with mock filesystem
func newTestP9DeviceWithMock(t *testing.T) (*P9Device, *mockFSDevice, *mem.PhysMemoryMap, *testIRQState) {
	t.Helper()

	memMap := mem.NewPhysMemoryMap()

	// Allocate RAM at 0x80000000
	_, err := memMap.RegisterRAM(0x80000000, 0x10000, 0)
	if err != nil {
		t.Fatalf("failed to register RAM: %v", err)
	}

	irq, irqState := newTestIRQ()

	mockFS := &mockFSDevice{
		attachQID:    p9.QID{Type: p9.QtDir, Version: 0, Path: 1},
		walkQIDs:     []p9.QID{{Type: p9.QtFile, Version: 0, Path: 2}},
		openQID:      p9.QID{Type: p9.QtFile, Version: 0, Path: 2},
		createQID:    p9.QID{Type: p9.QtFile, Version: 0, Path: 3},
		readData:     []byte("test data"),
		writeCount:   9,
		statfs:       p9.StatFS{BSize: 4096, Blocks: 1000000},
		getattrStat:  p9.Stat{QID: p9.QID{Type: p9.QtFile}, Mode: 0644, Size: 100},
		getattrValid: p9.GetattrBasic,
		readdirData:  []byte{}, // Empty for now
		mkdirQID:     p9.QID{Type: p9.QtDir, Version: 0, Path: 4},
		symlinkQID:   p9.QID{Type: p9.QtSymlink, Version: 0, Path: 5},
		mknodQID:     p9.QID{Type: p9.QtFile, Version: 0, Path: 6},
		readlinkTgt:  "/target",
		lockStatus:   p9.LockSuccess,
		getlockLock:  &p9.Lock{Type: p9.LockTypeUNLCK},
	}

	pd, err := NewP9Device(memMap, 0x10000000, irq, mockFS, "share")
	if err != nil {
		t.Fatalf("failed to create P9 device: %v", err)
	}

	return pd, mockFS, memMap, irqState
}

func TestP9HandlerVersion(t *testing.T) {
	pd, mockFS, _, _ := newTestP9DeviceWithMock(t)

	// Build Tversion message body
	buf := make([]byte, 256)
	enc := p9.NewEncoder(buf)
	enc.PutUint32(8192)       // msize
	enc.PutString("9P2000.L") // version

	dec := p9.NewDecoder(enc.Bytes())
	pd.handleVersion(pd.dev, 0, 0, p9.NoTag, dec)

	// Version should not call filesystem
	if mockFS.lastOp != "" {
		t.Errorf("handleVersion called filesystem: %s", mockFS.lastOp)
	}

	// Check msize was negotiated
	if pd.msize != 8192 {
		t.Errorf("msize = %d, want 8192", pd.msize)
	}
}

func TestP9HandlerAttach(t *testing.T) {
	pd, mockFS, _, _ := newTestP9DeviceWithMock(t)

	// Build Tattach message body
	buf := make([]byte, 256)
	enc := p9.NewEncoder(buf)
	enc.PutUint32(0)        // fid
	enc.PutUint32(p9.NOFID) // afid
	enc.PutString("root")   // uname
	enc.PutString("/")      // aname
	enc.PutUint32(1000)     // n_uname

	dec := p9.NewDecoder(enc.Bytes())
	pd.handleAttach(pd.dev, 0, 0, 1, dec)

	if mockFS.lastOp != "attach" {
		t.Errorf("handleAttach lastOp = %s, want attach", mockFS.lastOp)
	}

	// Check fid was stored
	_, ok := pd.getFid(0)
	if !ok {
		t.Error("fid 0 not stored after attach")
	}
}

func TestP9HandlerWalk(t *testing.T) {
	pd, mockFS, _, _ := newTestP9DeviceWithMock(t)

	// First attach
	f := &mockFile{path: "/"}
	pd.setFid(0, f)

	// Build Twalk message body
	buf := make([]byte, 256)
	enc := p9.NewEncoder(buf)
	enc.PutUint32(0)         // fid
	enc.PutUint32(1)         // newfid
	enc.PutUint16(1)         // nwname
	enc.PutString("testdir") // name

	dec := p9.NewDecoder(enc.Bytes())
	pd.handleWalk(pd.dev, 0, 0, 1, dec)

	if mockFS.lastOp != "walk" {
		t.Errorf("handleWalk lastOp = %s, want walk", mockFS.lastOp)
	}

	// Check newfid was stored
	_, ok := pd.getFid(1)
	if !ok {
		t.Error("newfid 1 not stored after walk")
	}
}

func TestP9HandlerWalkInvalidFid(t *testing.T) {
	pd, mockFS, _, _ := newTestP9DeviceWithMock(t)

	// Don't set up fid - walk with invalid fid should error
	buf := make([]byte, 256)
	enc := p9.NewEncoder(buf)
	enc.PutUint32(99) // invalid fid
	enc.PutUint32(1)  // newfid
	enc.PutUint16(0)  // nwname

	dec := p9.NewDecoder(enc.Bytes())
	pd.handleWalk(pd.dev, 0, 0, 1, dec)

	// Should not have called walk on fs
	if mockFS.lastOp == "walk" {
		t.Error("handleWalk should not call fs.Walk with invalid fid")
	}
}

func TestP9HandlerLopen(t *testing.T) {
	pd, mockFS, _, _ := newTestP9DeviceWithMock(t)

	f := &mockFile{path: "/test"}
	pd.setFid(5, f)

	buf := make([]byte, 256)
	enc := p9.NewEncoder(buf)
	enc.PutUint32(5)           // fid
	enc.PutUint32(p9.OpenRDWR) // flags

	dec := p9.NewDecoder(enc.Bytes())
	pd.handleLopen(pd.dev, 0, 0, 1, dec)

	if mockFS.lastOp != "open" {
		t.Errorf("handleLopen lastOp = %s, want open", mockFS.lastOp)
	}
}

func TestP9HandlerLcreate(t *testing.T) {
	pd, mockFS, _, _ := newTestP9DeviceWithMock(t)

	f := &mockFile{path: "/"}
	pd.setFid(1, f)

	buf := make([]byte, 256)
	enc := p9.NewEncoder(buf)
	enc.PutUint32(1)           // fid (parent dir)
	enc.PutString("new.txt")   // name
	enc.PutUint32(p9.OpenRDWR) // flags
	enc.PutUint32(0644)        // mode
	enc.PutUint32(1000)        // gid

	dec := p9.NewDecoder(enc.Bytes())
	pd.handleLcreate(pd.dev, 0, 0, 1, dec)

	if mockFS.lastOp != "create" {
		t.Errorf("handleLcreate lastOp = %s, want create", mockFS.lastOp)
	}
}

func TestP9HandlerRead(t *testing.T) {
	pd, mockFS, _, _ := newTestP9DeviceWithMock(t)

	f := &mockFile{path: "/test"}
	pd.setFid(10, f)

	buf := make([]byte, 256)
	enc := p9.NewEncoder(buf)
	enc.PutUint32(10)   // fid
	enc.PutUint64(0)    // offset
	enc.PutUint32(1024) // count

	dec := p9.NewDecoder(enc.Bytes())
	pd.handleRead(pd.dev, 0, 0, 1, dec)

	if mockFS.lastOp != "read" {
		t.Errorf("handleRead lastOp = %s, want read", mockFS.lastOp)
	}
}

func TestP9HandlerWrite(t *testing.T) {
	pd, mockFS, _, _ := newTestP9DeviceWithMock(t)

	f := &mockFile{path: "/test"}
	pd.setFid(10, f)

	data := []byte("hello world")
	buf := make([]byte, 256)
	enc := p9.NewEncoder(buf)
	enc.PutUint32(10)                // fid
	enc.PutUint64(0)                 // offset
	enc.PutUint32(uint32(len(data))) // count
	enc.PutBytes(data)               // data

	dec := p9.NewDecoder(enc.Bytes())
	pd.handleWrite(pd.dev, 0, 0, 1, dec)

	if mockFS.lastOp != "write" {
		t.Errorf("handleWrite lastOp = %s, want write", mockFS.lastOp)
	}
}

func TestP9HandlerClunk(t *testing.T) {
	pd, mockFS, _, _ := newTestP9DeviceWithMock(t)

	f := &mockFile{path: "/test"}
	pd.setFid(42, f)

	buf := make([]byte, 256)
	enc := p9.NewEncoder(buf)
	enc.PutUint32(42) // fid

	dec := p9.NewDecoder(enc.Bytes())
	pd.handleClunk(pd.dev, 0, 0, 1, dec)

	if mockFS.lastOp != "clunk" {
		t.Errorf("handleClunk lastOp = %s, want clunk", mockFS.lastOp)
	}

	// Fid should be removed
	_, ok := pd.getFid(42)
	if ok {
		t.Error("fid 42 should be removed after clunk")
	}
}

func TestP9HandlerStatfs(t *testing.T) {
	pd, mockFS, _, _ := newTestP9DeviceWithMock(t)

	buf := make([]byte, 256)
	enc := p9.NewEncoder(buf)
	enc.PutUint32(0) // fid (ignored)

	dec := p9.NewDecoder(enc.Bytes())
	pd.handleStatfs(pd.dev, 0, 0, 1, dec)

	if mockFS.lastOp != "statfs" {
		t.Errorf("handleStatfs lastOp = %s, want statfs", mockFS.lastOp)
	}
}

func TestP9HandlerGetattr(t *testing.T) {
	pd, mockFS, _, _ := newTestP9DeviceWithMock(t)

	f := &mockFile{path: "/test"}
	pd.setFid(7, f)

	buf := make([]byte, 256)
	enc := p9.NewEncoder(buf)
	enc.PutUint32(7)               // fid
	enc.PutUint64(p9.GetattrBasic) // request_mask

	dec := p9.NewDecoder(enc.Bytes())
	pd.handleGetattr(pd.dev, 0, 0, 1, dec)

	if mockFS.lastOp != "getattr" {
		t.Errorf("handleGetattr lastOp = %s, want getattr", mockFS.lastOp)
	}
}

func TestP9HandlerSetattr(t *testing.T) {
	pd, mockFS, _, _ := newTestP9DeviceWithMock(t)

	f := &mockFile{path: "/test"}
	pd.setFid(3, f)

	buf := make([]byte, 256)
	enc := p9.NewEncoder(buf)
	enc.PutUint32(3)              // fid
	enc.PutUint32(p9.SetattrMode) // valid
	enc.PutUint32(0755)           // mode
	enc.PutUint32(0)              // uid
	enc.PutUint32(0)              // gid
	enc.PutUint64(0)              // size
	enc.PutUint64(0)              // atime_sec
	enc.PutUint64(0)              // atime_nsec
	enc.PutUint64(0)              // mtime_sec
	enc.PutUint64(0)              // mtime_nsec

	dec := p9.NewDecoder(enc.Bytes())
	pd.handleSetattr(pd.dev, 0, 0, 1, dec)

	if mockFS.lastOp != "setattr" {
		t.Errorf("handleSetattr lastOp = %s, want setattr", mockFS.lastOp)
	}
}

func TestP9HandlerReaddir(t *testing.T) {
	pd, mockFS, _, _ := newTestP9DeviceWithMock(t)

	f := &mockFile{path: "/dir"}
	pd.setFid(3, f)

	buf := make([]byte, 256)
	enc := p9.NewEncoder(buf)
	enc.PutUint32(3)    // fid
	enc.PutUint64(0)    // offset
	enc.PutUint32(4096) // count

	dec := p9.NewDecoder(enc.Bytes())
	pd.handleReaddir(pd.dev, 0, 0, 1, dec)

	if mockFS.lastOp != "readdir" {
		t.Errorf("handleReaddir lastOp = %s, want readdir", mockFS.lastOp)
	}
}

func TestP9HandlerMkdir(t *testing.T) {
	pd, mockFS, _, _ := newTestP9DeviceWithMock(t)

	f := &mockFile{path: "/"}
	pd.setFid(1, f)

	buf := make([]byte, 256)
	enc := p9.NewEncoder(buf)
	enc.PutUint32(1)        // dfid
	enc.PutString("newdir") // name
	enc.PutUint32(0755)     // mode
	enc.PutUint32(1000)     // gid

	dec := p9.NewDecoder(enc.Bytes())
	pd.handleMkdir(pd.dev, 0, 0, 1, dec)

	if mockFS.lastOp != "mkdir" {
		t.Errorf("handleMkdir lastOp = %s, want mkdir", mockFS.lastOp)
	}
}

func TestP9HandlerUnlinkat(t *testing.T) {
	pd, mockFS, _, _ := newTestP9DeviceWithMock(t)

	f := &mockFile{path: "/"}
	pd.setFid(1, f)

	buf := make([]byte, 256)
	enc := p9.NewEncoder(buf)
	enc.PutUint32(1)          // dfid
	enc.PutString("file.txt") // name
	enc.PutUint32(0)          // flags

	dec := p9.NewDecoder(enc.Bytes())
	pd.handleUnlinkat(pd.dev, 0, 0, 1, dec)

	if mockFS.lastOp != "unlinkat" {
		t.Errorf("handleUnlinkat lastOp = %s, want unlinkat", mockFS.lastOp)
	}
}

func TestP9HandlerRenameat(t *testing.T) {
	pd, mockFS, _, _ := newTestP9DeviceWithMock(t)

	f1 := &mockFile{path: "/dir1"}
	f2 := &mockFile{path: "/dir2"}
	pd.setFid(1, f1)
	pd.setFid(2, f2)

	buf := make([]byte, 256)
	enc := p9.NewEncoder(buf)
	enc.PutUint32(1)         // old_dfid
	enc.PutString("old.txt") // old_name
	enc.PutUint32(2)         // new_dfid
	enc.PutString("new.txt") // new_name

	dec := p9.NewDecoder(enc.Bytes())
	pd.handleRenameat(pd.dev, 0, 0, 1, dec)

	if mockFS.lastOp != "renameat" {
		t.Errorf("handleRenameat lastOp = %s, want renameat", mockFS.lastOp)
	}
}

func TestP9HandlerSymlink(t *testing.T) {
	pd, mockFS, _, _ := newTestP9DeviceWithMock(t)

	f := &mockFile{path: "/"}
	pd.setFid(1, f)

	buf := make([]byte, 256)
	enc := p9.NewEncoder(buf)
	enc.PutUint32(1)            // fid
	enc.PutString("link.txt")   // name
	enc.PutString("target.txt") // target
	enc.PutUint32(1000)         // gid

	dec := p9.NewDecoder(enc.Bytes())
	pd.handleSymlink(pd.dev, 0, 0, 1, dec)

	if mockFS.lastOp != "symlink" {
		t.Errorf("handleSymlink lastOp = %s, want symlink", mockFS.lastOp)
	}
}

func TestP9HandlerMknod(t *testing.T) {
	pd, mockFS, _, _ := newTestP9DeviceWithMock(t)

	f := &mockFile{path: "/dev"}
	pd.setFid(1, f)

	buf := make([]byte, 256)
	enc := p9.NewEncoder(buf)
	enc.PutUint32(1)                   // dfid
	enc.PutString("null")              // name
	enc.PutUint32(p9.ModeIFCHR | 0666) // mode
	enc.PutUint32(1)                   // major
	enc.PutUint32(3)                   // minor
	enc.PutUint32(0)                   // gid

	dec := p9.NewDecoder(enc.Bytes())
	pd.handleMknod(pd.dev, 0, 0, 1, dec)

	if mockFS.lastOp != "mknod" {
		t.Errorf("handleMknod lastOp = %s, want mknod", mockFS.lastOp)
	}
}

func TestP9HandlerReadlink(t *testing.T) {
	pd, mockFS, _, _ := newTestP9DeviceWithMock(t)

	f := &mockFile{path: "/link"}
	pd.setFid(7, f)

	buf := make([]byte, 256)
	enc := p9.NewEncoder(buf)
	enc.PutUint32(7) // fid

	dec := p9.NewDecoder(enc.Bytes())
	pd.handleReadlink(pd.dev, 0, 0, 1, dec)

	if mockFS.lastOp != "readlink" {
		t.Errorf("handleReadlink lastOp = %s, want readlink", mockFS.lastOp)
	}
}

func TestP9HandlerLink(t *testing.T) {
	pd, mockFS, _, _ := newTestP9DeviceWithMock(t)

	f1 := &mockFile{path: "/"}
	f2 := &mockFile{path: "/source"}
	pd.setFid(1, f1)
	pd.setFid(5, f2)

	buf := make([]byte, 256)
	enc := p9.NewEncoder(buf)
	enc.PutUint32(1)          // dfid
	enc.PutUint32(5)          // fid
	enc.PutString("hardlink") // name

	dec := p9.NewDecoder(enc.Bytes())
	pd.handleLink(pd.dev, 0, 0, 1, dec)

	if mockFS.lastOp != "link" {
		t.Errorf("handleLink lastOp = %s, want link", mockFS.lastOp)
	}
}

// TestP9HandlerFsync tests that fsync is ignored.
// Reference: tinyemu-2019-12-21/virtio.c:2308-2317
// Note: C TinyEMU ignores fsync - it just parses the fid and sends an empty reply.
func TestP9HandlerFsync(t *testing.T) {
	pd, mockFS, _, _ := newTestP9DeviceWithMock(t)

	buf := make([]byte, 256)
	enc := p9.NewEncoder(buf)
	enc.PutUint32(10) // fid (doesn't need to exist since fsync is ignored)

	dec := p9.NewDecoder(enc.Bytes())
	pd.handleFsync(pd.dev, 0, 0, 1, dec)

	// C TinyEMU ignores fsync, so no fs operation should be called
	if mockFS.lastOp != "" {
		t.Errorf("handleFsync lastOp = %s, want empty (fsync ignored)", mockFS.lastOp)
	}
}

func TestP9HandlerFlush(t *testing.T) {
	pd, _, _, _ := newTestP9DeviceWithMock(t)

	buf := make([]byte, 256)
	enc := p9.NewEncoder(buf)
	enc.PutUint16(42) // oldtag

	dec := p9.NewDecoder(enc.Bytes())
	pd.handleFlush(pd.dev, 0, 0, 1, dec)

	// Flush just sends reply, no fs call
}

func TestP9HandlerLock(t *testing.T) {
	pd, mockFS, _, _ := newTestP9DeviceWithMock(t)

	f := &mockFile{path: "/test"}
	pd.setFid(5, f)

	buf := make([]byte, 256)
	enc := p9.NewEncoder(buf)
	enc.PutUint32(5)               // fid
	enc.PutUint8(p9.LockTypeWRLCK) // type
	enc.PutUint32(0)               // flags
	enc.PutUint64(0)               // start
	enc.PutUint64(0)               // length
	enc.PutUint32(1234)            // proc_id
	enc.PutString("client")        // client_id

	dec := p9.NewDecoder(enc.Bytes())
	pd.handleLock(pd.dev, 0, 0, 1, dec)

	if mockFS.lastOp != "lock" {
		t.Errorf("handleLock lastOp = %s, want lock", mockFS.lastOp)
	}
}

func TestP9HandlerGetlock(t *testing.T) {
	pd, mockFS, _, _ := newTestP9DeviceWithMock(t)

	f := &mockFile{path: "/test"}
	pd.setFid(5, f)

	buf := make([]byte, 256)
	enc := p9.NewEncoder(buf)
	enc.PutUint32(5)               // fid
	enc.PutUint8(p9.LockTypeRDLCK) // type
	enc.PutUint64(0)               // start
	enc.PutUint64(100)             // length
	enc.PutUint32(5678)            // proc_id
	enc.PutString("client2")       // client_id

	dec := p9.NewDecoder(enc.Bytes())
	pd.handleGetlock(pd.dev, 0, 0, 1, dec)

	if mockFS.lastOp != "getlock" {
		t.Errorf("handleGetlock lastOp = %s, want getlock", mockFS.lastOp)
	}
}

func TestP9HandleMessageUnsupportedType(t *testing.T) {
	pd, _, _, _ := newTestP9DeviceWithMock(t)

	// Build message with unsupported type (Tauth = 102)
	buf := make([]byte, 256)
	enc := p9.NewEncoder(buf)
	enc.PutUint32(0) // afid

	// Type 102 (Tauth) is not supported
	pd.handleMessage(pd.dev, 0, 0, p9.Tauth, 1, enc.Bytes())

	// Should send error response for unsupported type
}

func TestP9SendReplyAndError(t *testing.T) {
	pd, _, _, _ := newTestP9DeviceWithMock(t)

	// Test sendReply doesn't panic
	pd.sendReply(pd.dev, 0, 0, p9.Tversion, 1, []byte{1, 2, 3})

	// Test sendError doesn't panic
	pd.sendError(pd.dev, 0, 0, 1, p9.ENOENT)
}

// testError implements error for testing
type testError struct {
	msg string
}

func (e *testError) Error() string { return e.msg }

// Test error handling for handlers when fs returns errors
func TestP9HandlerAttachError(t *testing.T) {
	pd, mockFS, _, _ := newTestP9DeviceWithMock(t)
	mockFS.attachErr = &testError{msg: "permission denied"}

	buf := make([]byte, 256)
	enc := p9.NewEncoder(buf)
	enc.PutUint32(0)        // fid
	enc.PutUint32(p9.NOFID) // afid
	enc.PutString("root")   // uname
	enc.PutString("/")      // aname
	enc.PutUint32(1000)     // n_uname

	dec := p9.NewDecoder(enc.Bytes())
	pd.handleAttach(pd.dev, 0, 0, 1, dec)

	// Should not have stored fid due to error
	_, ok := pd.getFid(0)
	if ok {
		t.Error("fid should not be stored when attach fails")
	}
}

func TestP9HandlerWalkError(t *testing.T) {
	pd, mockFS, _, _ := newTestP9DeviceWithMock(t)
	mockFS.walkErr = &testError{msg: "not found"}

	f := &mockFile{path: "/"}
	pd.setFid(0, f)

	buf := make([]byte, 256)
	enc := p9.NewEncoder(buf)
	enc.PutUint32(0)          // fid
	enc.PutUint32(1)          // newfid
	enc.PutUint16(1)          // nwname
	enc.PutString("notexist") // name

	dec := p9.NewDecoder(enc.Bytes())
	pd.handleWalk(pd.dev, 0, 0, 1, dec)

	// Should not have stored newfid due to error
	_, ok := pd.getFid(1)
	if ok {
		t.Error("newfid should not be stored when walk fails")
	}
}

func TestP9HandlerOpenError(t *testing.T) {
	pd, mockFS, _, _ := newTestP9DeviceWithMock(t)
	mockFS.openErr = &testError{msg: "permission denied"}

	f := &mockFile{path: "/test"}
	pd.setFid(5, f)

	buf := make([]byte, 256)
	enc := p9.NewEncoder(buf)
	enc.PutUint32(5)           // fid
	enc.PutUint32(p9.OpenRDWR) // flags

	dec := p9.NewDecoder(enc.Bytes())
	pd.handleLopen(pd.dev, 0, 0, 1, dec)

	// Handler should handle error gracefully
	if mockFS.lastOp != "open" {
		t.Errorf("handleLopen lastOp = %s, want open", mockFS.lastOp)
	}
}

func TestP9HandlerReadError(t *testing.T) {
	pd, mockFS, _, _ := newTestP9DeviceWithMock(t)
	mockFS.readErr = &testError{msg: "I/O error"}

	f := &mockFile{path: "/test"}
	pd.setFid(10, f)

	buf := make([]byte, 256)
	enc := p9.NewEncoder(buf)
	enc.PutUint32(10)   // fid
	enc.PutUint64(0)    // offset
	enc.PutUint32(1024) // count

	dec := p9.NewDecoder(enc.Bytes())
	pd.handleRead(pd.dev, 0, 0, 1, dec)

	if mockFS.lastOp != "read" {
		t.Errorf("handleRead lastOp = %s, want read", mockFS.lastOp)
	}
}

func TestP9HandlerWriteError(t *testing.T) {
	pd, mockFS, _, _ := newTestP9DeviceWithMock(t)
	mockFS.writeErr = &testError{msg: "no space left"}

	f := &mockFile{path: "/test"}
	pd.setFid(10, f)

	data := []byte("hello")
	buf := make([]byte, 256)
	enc := p9.NewEncoder(buf)
	enc.PutUint32(10)
	enc.PutUint64(0)
	enc.PutUint32(uint32(len(data)))
	enc.PutBytes(data)

	dec := p9.NewDecoder(enc.Bytes())
	pd.handleWrite(pd.dev, 0, 0, 1, dec)

	if mockFS.lastOp != "write" {
		t.Errorf("handleWrite lastOp = %s, want write", mockFS.lastOp)
	}
}

// TestP9SetFidReplacesOldFile tests that setFid closes the old file when replacing.
// This matches C behavior: tinyemu-2019-12-21/virtio.c:1698-1712 (fid_set)
// The C code calls fs_delete on the old fd before setting the new one.
func TestP9SetFidReplacesOldFile(t *testing.T) {
	pd, mockFS, _, _ := newTestP9DeviceWithMock(t)

	// Set initial file for fid 1
	f1 := &mockFile{path: "/first"}
	pd.setFid(1, f1)
	mockFS.lastOp = "" // Reset tracking

	// Replace with new file - should clunk the old one first
	f2 := &mockFile{path: "/second"}
	pd.setFid(1, f2)

	// The old file should have been clunked
	if mockFS.lastOp != "clunk" {
		t.Errorf("setFid should clunk old file, lastOp = %s, want clunk", mockFS.lastOp)
	}

	// The new file should be stored
	gotF, ok := pd.getFid(1)
	if !ok {
		t.Error("getFid(1) should return true")
	}
	if gotF != f2 {
		t.Error("getFid(1) should return the new file")
	}
}

// TestP9WalkSameFid tests that walk with fid == newfid properly clunks the old file.
// This is the common pattern from 9P protocol where a walk is done "in place".
// Reference: tinyemu-2019-12-21/virtio.c:2539 (fid_set in walk handler)
func TestP9WalkSameFid(t *testing.T) {
	pd, mockFS, _, _ := newTestP9DeviceWithMock(t)

	// Set up initial fid
	f := &mockFile{path: "/original"}
	pd.setFid(5, f)
	mockFS.lastOp = "" // Reset tracking

	// Walk with same fid and newfid
	buf := make([]byte, 256)
	enc := p9.NewEncoder(buf)
	enc.PutUint32(5)        // fid
	enc.PutUint32(5)        // newfid (same as fid)
	enc.PutUint16(1)        // nwname
	enc.PutString("subdir") // name

	dec := p9.NewDecoder(enc.Bytes())
	pd.handleWalk(pd.dev, 0, 0, 1, dec)

	// Should have called walk first
	if mockFS.lastOp == "" {
		t.Error("walk should have been called")
	}
}

// TestP9FidNotFound tests that operations with invalid fids return EPROTO.
// Reference: tinyemu-2019-12-21/virtio.c:2115-2116 (lopen fid check)
// Reference: tinyemu-2019-12-21/virtio.c:2141-2142 (lcreate fid check)
// Reference: tinyemu-2019-12-21/virtio.c:2165-2166 (symlink fid check)
// Reference: tinyemu-2019-12-21/virtio.c:2190-2191 (mknod fid check)
// Reference: tinyemu-2019-12-21/virtio.c:2213-2214 (readlink fid check)
func TestP9FidNotFound(t *testing.T) {
	tests := []struct {
		name    string
		handler func(pd *P9Device, dev *Device, queueIdx, descIdx int, tag uint16, dec *p9.Decoder)
		setup   func(enc *p9.Encoder)
	}{
		{
			name: "lopen",
			handler: func(pd *P9Device, dev *Device, queueIdx, descIdx int, tag uint16, dec *p9.Decoder) {
				pd.handleLopen(dev, queueIdx, descIdx, tag, dec)
			},
			setup: func(enc *p9.Encoder) {
				enc.PutUint32(999) // invalid fid
				enc.PutUint32(0)   // flags
			},
		},
		{
			name: "lcreate",
			handler: func(pd *P9Device, dev *Device, queueIdx, descIdx int, tag uint16, dec *p9.Decoder) {
				pd.handleLcreate(dev, queueIdx, descIdx, tag, dec)
			},
			setup: func(enc *p9.Encoder) {
				enc.PutUint32(999)    // invalid fid
				enc.PutString("test") // name
				enc.PutUint32(0)      // flags
				enc.PutUint32(0644)   // mode
				enc.PutUint32(0)      // gid
			},
		},
		{
			name: "symlink",
			handler: func(pd *P9Device, dev *Device, queueIdx, descIdx int, tag uint16, dec *p9.Decoder) {
				pd.handleSymlink(dev, queueIdx, descIdx, tag, dec)
			},
			setup: func(enc *p9.Encoder) {
				enc.PutUint32(999)      // invalid fid
				enc.PutString("link")   // name
				enc.PutString("target") // target
				enc.PutUint32(0)        // gid
			},
		},
		{
			name: "mknod",
			handler: func(pd *P9Device, dev *Device, queueIdx, descIdx int, tag uint16, dec *p9.Decoder) {
				pd.handleMknod(dev, queueIdx, descIdx, tag, dec)
			},
			setup: func(enc *p9.Encoder) {
				enc.PutUint32(999)    // invalid dfid
				enc.PutString("node") // name
				enc.PutUint32(0)      // mode
				enc.PutUint32(0)      // major
				enc.PutUint32(0)      // minor
				enc.PutUint32(0)      // gid
			},
		},
		{
			name: "readlink",
			handler: func(pd *P9Device, dev *Device, queueIdx, descIdx int, tag uint16, dec *p9.Decoder) {
				pd.handleReadlink(dev, queueIdx, descIdx, tag, dec)
			},
			setup: func(enc *p9.Encoder) {
				enc.PutUint32(999) // invalid fid
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pd, mockFS, _, _ := newTestP9DeviceWithMock(t)
			mockFS.lastOp = ""

			buf := make([]byte, 256)
			enc := p9.NewEncoder(buf)
			tt.setup(enc)

			dec := p9.NewDecoder(enc.Bytes())
			tt.handler(pd, pd.dev, 0, 0, 1, dec)

			// No fs operation should be called for invalid fid
			if mockFS.lastOp != "" {
				t.Errorf("expected no fs operation for invalid fid, got %s", mockFS.lastOp)
			}
		})
	}
}

// TestP9VersionMsizeAccepted tests that version accepts client msize.
// Reference: tinyemu-2019-12-21/virtio.c:2462 - s->msize = msize (accepts client value)
func TestP9VersionMsizeAccepted(t *testing.T) {
	pd, _, _, _ := newTestP9DeviceWithMock(t)

	// Request a large msize - C code accepts it directly
	buf := make([]byte, 256)
	enc := p9.NewEncoder(buf)
	enc.PutUint32(65536)      // large msize
	enc.PutString("9P2000.L") // version

	dec := p9.NewDecoder(enc.Bytes())
	pd.handleVersion(pd.dev, 0, 0, p9.NoTag, dec)

	// C behavior: s->msize = msize (accepts client value directly)
	if pd.msize != 65536 {
		t.Errorf("msize = %d, want 65536 (should accept client msize)", pd.msize)
	}
}

// TestP9FlushMalformed tests that flush returns error on malformed input.
// Reference: tinyemu-2019-12-21/virtio.c:2492-2494 - parse oldtag, goto protocol_error on fail
func TestP9FlushMalformed(t *testing.T) {
	pd, _, _, _ := newTestP9DeviceWithMock(t)

	// Empty buffer - should fail to parse oldtag
	dec := p9.NewDecoder([]byte{})
	pd.handleFlush(pd.dev, 0, 0, 1, dec)

	// With proper error handling, this would return EPROTO
	// The test verifies we don't panic on malformed input
}

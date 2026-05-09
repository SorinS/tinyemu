package p9

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestParseHeader(t *testing.T) {
	tests := []struct {
		name    string
		data    []byte
		want    Header
		wantErr bool
	}{
		{
			name: "valid header",
			data: []byte{0x13, 0x00, 0x00, 0x00, Tversion, 0xFF, 0xFF},
			want: Header{Size: 19, Type: Tversion, Tag: NoTag},
		},
		{
			name: "Rattach response",
			data: []byte{0x14, 0x00, 0x00, 0x00, Rattach, 0x01, 0x00},
			want: Header{Size: 20, Type: Rattach, Tag: 1},
		},
		{
			name:    "too short",
			data:    []byte{0x13, 0x00, 0x00, 0x00, Tversion},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseHeader(tt.data)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseHeader() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("ParseHeader() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestMarshalHeader(t *testing.T) {
	buf := make([]byte, HeaderSize)
	h := Header{Size: 19, Type: Rversion, Tag: NoTag}
	MarshalHeader(buf, h)

	want := []byte{0x13, 0x00, 0x00, 0x00, Rversion, 0xFF, 0xFF}
	if !bytes.Equal(buf, want) {
		t.Errorf("MarshalHeader() = %v, want %v", buf, want)
	}
}

func TestEncoderDecoder(t *testing.T) {
	buf := make([]byte, 256)
	enc := NewEncoder(buf)

	// Write various types
	enc.PutUint8(0x42)
	enc.PutUint16(0x1234)
	enc.PutUint32(0xDEADBEEF)
	enc.PutUint64(0x123456789ABCDEF0)
	enc.PutString("hello")
	enc.PutQID(QID{Type: QtDir, Version: 1, Path: 12345})

	// Read back
	dec := NewDecoder(enc.Bytes())

	b, _ := dec.GetUint8()
	if b != 0x42 {
		t.Errorf("GetUint8() = 0x%02x, want 0x42", b)
	}

	h, _ := dec.GetUint16()
	if h != 0x1234 {
		t.Errorf("GetUint16() = 0x%04x, want 0x1234", h)
	}

	w, _ := dec.GetUint32()
	if w != 0xDEADBEEF {
		t.Errorf("GetUint32() = 0x%08x, want 0xDEADBEEF", w)
	}

	d, _ := dec.GetUint64()
	if d != 0x123456789ABCDEF0 {
		t.Errorf("GetUint64() = 0x%016x, want 0x123456789ABCDEF0", d)
	}

	s, _ := dec.GetString()
	if s != "hello" {
		t.Errorf("GetString() = %q, want %q", s, "hello")
	}

	qid, _ := dec.GetQID()
	if qid.Type != QtDir || qid.Version != 1 || qid.Path != 12345 {
		t.Errorf("GetQID() = %+v, want {Type: %d, Version: 1, Path: 12345}", qid, QtDir)
	}
}

func TestParseTversion(t *testing.T) {
	// Build a Tversion message body: msize(4) + version string(2+n)
	buf := make([]byte, 256)
	enc := NewEncoder(buf)
	enc.PutUint32(8192)
	enc.PutString("9P2000.L")

	dec := NewDecoder(enc.Bytes())
	msg, err := ParseTversion(dec)
	if err != nil {
		t.Fatalf("ParseTversion() error = %v", err)
	}

	if msg.Msize != 8192 {
		t.Errorf("Msize = %d, want 8192", msg.Msize)
	}
	if msg.Version != "9P2000.L" {
		t.Errorf("Version = %q, want %q", msg.Version, "9P2000.L")
	}
}

func TestMarshalRversion(t *testing.T) {
	buf := make([]byte, 256)
	enc := NewEncoder(buf)

	msg := RversionMsg{Msize: 8192, Version: "9P2000.L"}
	if err := MarshalRversion(enc, msg); err != nil {
		t.Fatalf("MarshalRversion() error = %v", err)
	}

	// Verify
	dec := NewDecoder(enc.Bytes())
	msize, _ := dec.GetUint32()
	version, _ := dec.GetString()

	if msize != 8192 {
		t.Errorf("Msize = %d, want 8192", msize)
	}
	if version != "9P2000.L" {
		t.Errorf("Version = %q, want %q", version, "9P2000.L")
	}
}

func TestParseTattach(t *testing.T) {
	buf := make([]byte, 256)
	enc := NewEncoder(buf)
	enc.PutUint32(0)      // fid
	enc.PutUint32(NOFID)  // afid
	enc.PutString("root") // uname
	enc.PutString("/")    // aname
	enc.PutUint32(1000)   // n_uname (9P2000.L extension)

	dec := NewDecoder(enc.Bytes())
	msg, err := ParseTattach(dec)
	if err != nil {
		t.Fatalf("ParseTattach() error = %v", err)
	}

	if msg.Fid != 0 {
		t.Errorf("Fid = %d, want 0", msg.Fid)
	}
	if msg.AFid != NOFID {
		t.Errorf("AFid = %d, want NOFID", msg.AFid)
	}
	if msg.Uname != "root" {
		t.Errorf("Uname = %q, want %q", msg.Uname, "root")
	}
	if msg.Aname != "/" {
		t.Errorf("Aname = %q, want %q", msg.Aname, "/")
	}
	if msg.NUname != 1000 {
		t.Errorf("NUname = %d, want 1000", msg.NUname)
	}
}

func TestMarshalRattach(t *testing.T) {
	buf := make([]byte, 256)
	enc := NewEncoder(buf)

	qid := QID{Type: QtDir, Version: 0, Path: 1}
	if err := MarshalRattach(enc, RattachMsg{QID: qid}); err != nil {
		t.Fatalf("MarshalRattach() error = %v", err)
	}

	dec := NewDecoder(enc.Bytes())
	got, _ := dec.GetQID()

	if got != qid {
		t.Errorf("QID = %+v, want %+v", got, qid)
	}
}

func TestParseTwalk(t *testing.T) {
	buf := make([]byte, 256)
	enc := NewEncoder(buf)
	enc.PutUint32(1) // fid
	enc.PutUint32(2) // newfid
	enc.PutUint16(3) // nwname
	enc.PutString("usr")
	enc.PutString("local")
	enc.PutString("bin")

	dec := NewDecoder(enc.Bytes())
	msg, err := ParseTwalk(dec)
	if err != nil {
		t.Fatalf("ParseTwalk() error = %v", err)
	}

	if msg.Fid != 1 {
		t.Errorf("Fid = %d, want 1", msg.Fid)
	}
	if msg.NewFid != 2 {
		t.Errorf("NewFid = %d, want 2", msg.NewFid)
	}
	if len(msg.Names) != 3 {
		t.Fatalf("len(Names) = %d, want 3", len(msg.Names))
	}
	expected := []string{"usr", "local", "bin"}
	for i, name := range msg.Names {
		if name != expected[i] {
			t.Errorf("Names[%d] = %q, want %q", i, name, expected[i])
		}
	}
}

func TestMarshalRwalk(t *testing.T) {
	buf := make([]byte, 256)
	enc := NewEncoder(buf)

	qids := []QID{
		{Type: QtDir, Version: 0, Path: 1},
		{Type: QtDir, Version: 0, Path: 2},
		{Type: QtDir, Version: 0, Path: 3},
	}
	if err := MarshalRwalk(enc, RwalkMsg{QIDs: qids}); err != nil {
		t.Fatalf("MarshalRwalk() error = %v", err)
	}

	dec := NewDecoder(enc.Bytes())
	nwqid, _ := dec.GetUint16()
	if nwqid != 3 {
		t.Errorf("nwqid = %d, want 3", nwqid)
	}

	for i := 0; i < 3; i++ {
		qid, _ := dec.GetQID()
		if qid != qids[i] {
			t.Errorf("qid[%d] = %+v, want %+v", i, qid, qids[i])
		}
	}
}

func TestParseTlopen(t *testing.T) {
	buf := make([]byte, 256)
	enc := NewEncoder(buf)
	enc.PutUint32(5)        // fid
	enc.PutUint32(OpenRDWR) // flags

	dec := NewDecoder(enc.Bytes())
	msg, err := ParseTlopen(dec)
	if err != nil {
		t.Fatalf("ParseTlopen() error = %v", err)
	}

	if msg.Fid != 5 {
		t.Errorf("Fid = %d, want 5", msg.Fid)
	}
	if msg.Flags != OpenRDWR {
		t.Errorf("Flags = %d, want %d", msg.Flags, OpenRDWR)
	}
}

func TestMarshalRlopen(t *testing.T) {
	buf := make([]byte, 256)
	enc := NewEncoder(buf)

	msg := RlopenMsg{
		QID:    QID{Type: QtFile, Version: 1, Path: 100},
		IOUnit: 4096,
	}
	if err := MarshalRlopen(enc, msg); err != nil {
		t.Fatalf("MarshalRlopen() error = %v", err)
	}

	dec := NewDecoder(enc.Bytes())
	qid, _ := dec.GetQID()
	iounit, _ := dec.GetUint32()

	if qid != msg.QID {
		t.Errorf("QID = %+v, want %+v", qid, msg.QID)
	}
	if iounit != 4096 {
		t.Errorf("IOUnit = %d, want 4096", iounit)
	}
}

func TestParseTread(t *testing.T) {
	buf := make([]byte, 256)
	enc := NewEncoder(buf)
	enc.PutUint32(10)   // fid
	enc.PutUint64(0)    // offset
	enc.PutUint32(4096) // count

	dec := NewDecoder(enc.Bytes())
	msg, err := ParseTread(dec)
	if err != nil {
		t.Fatalf("ParseTread() error = %v", err)
	}

	if msg.Fid != 10 {
		t.Errorf("Fid = %d, want 10", msg.Fid)
	}
	if msg.Offset != 0 {
		t.Errorf("Offset = %d, want 0", msg.Offset)
	}
	if msg.Count != 4096 {
		t.Errorf("Count = %d, want 4096", msg.Count)
	}
}

func TestMarshalRread(t *testing.T) {
	buf := make([]byte, 256)
	enc := NewEncoder(buf)

	data := []byte("Hello, World!")
	if err := MarshalRread(enc, data); err != nil {
		t.Fatalf("MarshalRread() error = %v", err)
	}

	dec := NewDecoder(enc.Bytes())
	count, _ := dec.GetUint32()
	if count != uint32(len(data)) {
		t.Errorf("count = %d, want %d", count, len(data))
	}

	gotData, _ := dec.GetBytes(int(count))
	if !bytes.Equal(gotData, data) {
		t.Errorf("data = %q, want %q", gotData, data)
	}
}

func TestParseTwrite(t *testing.T) {
	buf := make([]byte, 256)
	enc := NewEncoder(buf)
	data := []byte("test data")
	enc.PutUint32(15)  // fid
	enc.PutUint64(100) // offset
	enc.PutUint32(uint32(len(data)))
	enc.PutBytes(data)

	dec := NewDecoder(enc.Bytes())
	msg, err := ParseTwrite(dec)
	if err != nil {
		t.Fatalf("ParseTwrite() error = %v", err)
	}

	if msg.Fid != 15 {
		t.Errorf("Fid = %d, want 15", msg.Fid)
	}
	if msg.Offset != 100 {
		t.Errorf("Offset = %d, want 100", msg.Offset)
	}
	if !bytes.Equal(msg.Data, data) {
		t.Errorf("Data = %q, want %q", msg.Data, data)
	}
}

func TestParseTclunk(t *testing.T) {
	buf := make([]byte, 256)
	enc := NewEncoder(buf)
	enc.PutUint32(42)

	dec := NewDecoder(enc.Bytes())
	msg, err := ParseTclunk(dec)
	if err != nil {
		t.Fatalf("ParseTclunk() error = %v", err)
	}

	if msg.Fid != 42 {
		t.Errorf("Fid = %d, want 42", msg.Fid)
	}
}

func TestParseTgetattr(t *testing.T) {
	buf := make([]byte, 256)
	enc := NewEncoder(buf)
	enc.PutUint32(7)            // fid
	enc.PutUint64(GetattrBasic) // request_mask

	dec := NewDecoder(enc.Bytes())
	msg, err := ParseTgetattr(dec)
	if err != nil {
		t.Fatalf("ParseTgetattr() error = %v", err)
	}

	if msg.Fid != 7 {
		t.Errorf("Fid = %d, want 7", msg.Fid)
	}
	if msg.ReqMask != GetattrBasic {
		t.Errorf("ReqMask = %d, want %d", msg.ReqMask, GetattrBasic)
	}
}

func TestMarshalRerror(t *testing.T) {
	buf := make([]byte, 256)
	enc := NewEncoder(buf)

	if err := MarshalRerror(enc, ENOENT); err != nil {
		t.Fatalf("MarshalRerror() error = %v", err)
	}

	dec := NewDecoder(enc.Bytes())
	ecode, _ := dec.GetUint32()

	if ecode != ENOENT {
		t.Errorf("ecode = %d, want %d", ecode, ENOENT)
	}
}

func TestParseTreaddir(t *testing.T) {
	buf := make([]byte, 256)
	enc := NewEncoder(buf)
	enc.PutUint32(3)    // fid
	enc.PutUint64(0)    // offset
	enc.PutUint32(8192) // count

	dec := NewDecoder(enc.Bytes())
	msg, err := ParseTreaddir(dec)
	if err != nil {
		t.Fatalf("ParseTreaddir() error = %v", err)
	}

	if msg.Fid != 3 {
		t.Errorf("Fid = %d, want 3", msg.Fid)
	}
	if msg.Offset != 0 {
		t.Errorf("Offset = %d, want 0", msg.Offset)
	}
	if msg.Count != 8192 {
		t.Errorf("Count = %d, want 8192", msg.Count)
	}
}

func TestMarshalDirent(t *testing.T) {
	buf := make([]byte, 256)
	enc := NewEncoder(buf)

	dirent := Dirent{
		QID:    QID{Type: QtFile, Version: 0, Path: 123},
		Offset: 24,
		Type:   QtFile,
		Name:   "test.txt",
	}
	if err := MarshalDirent(enc, dirent); err != nil {
		t.Fatalf("MarshalDirent() error = %v", err)
	}

	dec := NewDecoder(enc.Bytes())
	qid, _ := dec.GetQID()
	offset, _ := dec.GetUint64()
	typ, _ := dec.GetUint8()
	name, _ := dec.GetString()

	if qid != dirent.QID {
		t.Errorf("QID = %+v, want %+v", qid, dirent.QID)
	}
	if offset != 24 {
		t.Errorf("Offset = %d, want 24", offset)
	}
	if typ != QtFile {
		t.Errorf("Type = %d, want %d", typ, QtFile)
	}
	if name != "test.txt" {
		t.Errorf("Name = %q, want %q", name, "test.txt")
	}
}

func TestDirentSize(t *testing.T) {
	// QID (13) + offset (8) + type (1) + strlen (2) + name
	name := "test.txt"
	expected := 13 + 8 + 1 + 2 + len(name)
	if got := DirentSize(name); got != expected {
		t.Errorf("DirentSize(%q) = %d, want %d", name, got, expected)
	}
}

func TestMarshalRstatfs(t *testing.T) {
	buf := make([]byte, 256)
	enc := NewEncoder(buf)

	st := StatFS{
		Type:    0,
		BSize:   4096,
		Blocks:  1000000,
		BFree:   500000,
		BAvail:  450000,
		Files:   100000,
		FFree:   50000,
		FSid:    0,
		NameLen: 256,
	}
	if err := MarshalRstatfs(enc, st); err != nil {
		t.Fatalf("MarshalRstatfs() error = %v", err)
	}

	dec := NewDecoder(enc.Bytes())
	typ, _ := dec.GetUint32()
	bsize, _ := dec.GetUint32()
	blocks, _ := dec.GetUint64()
	bfree, _ := dec.GetUint64()
	bavail, _ := dec.GetUint64()
	files, _ := dec.GetUint64()
	ffree, _ := dec.GetUint64()
	fsid, _ := dec.GetUint64()
	namelen, _ := dec.GetUint32()

	if typ != 0 || bsize != 4096 || blocks != 1000000 || bfree != 500000 ||
		bavail != 450000 || files != 100000 || ffree != 50000 || fsid != 0 || namelen != 256 {
		t.Error("StatFS values don't match")
	}
}

func TestParseTmkdir(t *testing.T) {
	buf := make([]byte, 256)
	enc := NewEncoder(buf)
	enc.PutUint32(1)        // dfid
	enc.PutString("newdir") // name
	enc.PutUint32(0755)     // mode
	enc.PutUint32(1000)     // gid

	dec := NewDecoder(enc.Bytes())
	msg, err := ParseTmkdir(dec)
	if err != nil {
		t.Fatalf("ParseTmkdir() error = %v", err)
	}

	if msg.DFid != 1 {
		t.Errorf("DFid = %d, want 1", msg.DFid)
	}
	if msg.Name != "newdir" {
		t.Errorf("Name = %q, want %q", msg.Name, "newdir")
	}
	if msg.Mode != 0755 {
		t.Errorf("Mode = %o, want 0755", msg.Mode)
	}
	if msg.GID != 1000 {
		t.Errorf("GID = %d, want 1000", msg.GID)
	}
}

func TestParseTunlinkat(t *testing.T) {
	buf := make([]byte, 256)
	enc := NewEncoder(buf)
	enc.PutUint32(1)         // dfid
	enc.PutString("oldfile") // name
	enc.PutUint32(0)         // flags

	dec := NewDecoder(enc.Bytes())
	msg, err := ParseTunlinkat(dec)
	if err != nil {
		t.Fatalf("ParseTunlinkat() error = %v", err)
	}

	if msg.DFid != 1 {
		t.Errorf("DFid = %d, want 1", msg.DFid)
	}
	if msg.Name != "oldfile" {
		t.Errorf("Name = %q, want %q", msg.Name, "oldfile")
	}
	if msg.Flags != 0 {
		t.Errorf("Flags = %d, want 0", msg.Flags)
	}
}

func TestParseTrenameat(t *testing.T) {
	buf := make([]byte, 256)
	enc := NewEncoder(buf)
	enc.PutUint32(1)         // old_dfid
	enc.PutString("old.txt") // old_name
	enc.PutUint32(2)         // new_dfid
	enc.PutString("new.txt") // new_name

	dec := NewDecoder(enc.Bytes())
	msg, err := ParseTrenameat(dec)
	if err != nil {
		t.Fatalf("ParseTrenameat() error = %v", err)
	}

	if msg.OldDFid != 1 {
		t.Errorf("OldDFid = %d, want 1", msg.OldDFid)
	}
	if msg.OldName != "old.txt" {
		t.Errorf("OldName = %q, want %q", msg.OldName, "old.txt")
	}
	if msg.NewDFid != 2 {
		t.Errorf("NewDFid = %d, want 2", msg.NewDFid)
	}
	if msg.NewName != "new.txt" {
		t.Errorf("NewName = %q, want %q", msg.NewName, "new.txt")
	}
}

func TestReadWriteMessage(t *testing.T) {
	// Create a message
	body := make([]byte, 12)
	binary.LittleEndian.PutUint32(body[0:4], 8192)
	binary.LittleEndian.PutUint16(body[4:6], 8)
	copy(body[6:], "9P2000.L")

	var buf bytes.Buffer
	if err := WriteMessage(&buf, Tversion, NoTag, body); err != nil {
		t.Fatalf("WriteMessage() error = %v", err)
	}

	// Read it back
	msg, err := ReadMessage(&buf, DefaultMsize)
	if err != nil {
		t.Fatalf("ReadMessage() error = %v", err)
	}

	header, _ := ParseHeader(msg)
	if header.Type != Tversion {
		t.Errorf("Type = %d, want %d", header.Type, Tversion)
	}
	if header.Tag != NoTag {
		t.Errorf("Tag = %d, want %d", header.Tag, NoTag)
	}
	if int(header.Size) != HeaderSize+len(body) {
		t.Errorf("Size = %d, want %d", header.Size, HeaderSize+len(body))
	}
}

func TestMessageTypeName(t *testing.T) {
	tests := []struct {
		typ  uint8
		want string
	}{
		{Tversion, "Tversion"},
		{Rversion, "Rversion"},
		{Tattach, "Tattach"},
		{Rattach, "Rattach"},
		{Twalk, "Twalk"},
		{Rwalk, "Rwalk"},
		{Tlopen, "Tlopen"},
		{Rlopen, "Rlopen"},
		{Tread, "Tread"},
		{Rread, "Rread"},
		{Twrite, "Twrite"},
		{Rwrite, "Rwrite"},
		{Tclunk, "Tclunk"},
		{Rclunk, "Rclunk"},
		{Rerror, "Rerror"},
		{Tstatfs, "Tstatfs"},
		{Rstatfs, "Rstatfs"},
		{Tgetattr, "Tgetattr"},
		{Rgetattr, "Rgetattr"},
		{Tsetattr, "Tsetattr"},
		{Rsetattr, "Rsetattr"},
		{Treaddir, "Treaddir"},
		{Rreaddir, "Rreaddir"},
		{Tmkdir, "Tmkdir"},
		{Rmkdir, "Rmkdir"},
		{Tunlinkat, "Tunlinkat"},
		{Runlinkat, "Runlinkat"},
		{0, "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := MessageTypeName(tt.typ); got != tt.want {
				t.Errorf("MessageTypeName(%d) = %q, want %q", tt.typ, got, tt.want)
			}
		})
	}
}

func TestEncoderBufferOverflow(t *testing.T) {
	buf := make([]byte, 4)
	enc := NewEncoder(buf)

	// This should succeed
	if err := enc.PutUint32(123); err != nil {
		t.Errorf("PutUint32() error = %v", err)
	}

	// This should fail (buffer full)
	if err := enc.PutUint8(1); err != ErrMessageTooLarge {
		t.Errorf("PutUint8() error = %v, want ErrMessageTooLarge", err)
	}
}

func TestDecoderBufferUnderflow(t *testing.T) {
	buf := make([]byte, 4)
	binary.LittleEndian.PutUint32(buf, 12345)
	dec := NewDecoder(buf)

	// This should succeed
	if _, err := dec.GetUint32(); err != nil {
		t.Errorf("GetUint32() error = %v", err)
	}

	// This should fail (buffer empty)
	if _, err := dec.GetUint8(); err != ErrMessageTooShort {
		t.Errorf("GetUint8() error = %v, want ErrMessageTooShort", err)
	}
}

func TestStatMarshaling(t *testing.T) {
	buf := make([]byte, 512)
	enc := NewEncoder(buf)

	st := Stat{
		QID:       QID{Type: QtFile, Version: 1, Path: 100},
		Mode:      ModeIFREG | 0644,
		UID:       1000,
		GID:       1000,
		NLink:     1,
		RDev:      0,
		Size:      4096,
		BlkSize:   512,
		Blocks:    8,
		AtimeSec:  1609459200,
		AtimeNsec: 123456789,
		MtimeSec:  1609459300,
		MtimeNsec: 987654321,
		CtimeSec:  1609459100,
		CtimeNsec: 111222333,
	}

	if err := enc.PutStat(st); err != nil {
		t.Fatalf("PutStat() error = %v", err)
	}

	dec := NewDecoder(enc.Bytes())
	got, err := dec.GetStat()
	if err != nil {
		t.Fatalf("GetStat() error = %v", err)
	}

	if got != st {
		t.Errorf("GetStat() = %+v, want %+v", got, st)
	}
}

// Additional message parsing/marshaling tests for improved coverage
// Reference: TinyEMU virtio.c lines 2042-2620, fs.h lines 25-194

func TestParseTlcreate(t *testing.T) {
	buf := make([]byte, 256)
	enc := NewEncoder(buf)
	enc.PutUint32(5)         // fid
	enc.PutString("newfile") // name
	enc.PutUint32(OpenRDWR)  // flags
	enc.PutUint32(0644)      // mode
	enc.PutUint32(1000)      // gid

	dec := NewDecoder(enc.Bytes())
	msg, err := ParseTlcreate(dec)
	if err != nil {
		t.Fatalf("ParseTlcreate() error = %v", err)
	}

	if msg.Fid != 5 {
		t.Errorf("Fid = %d, want 5", msg.Fid)
	}
	if msg.Name != "newfile" {
		t.Errorf("Name = %q, want %q", msg.Name, "newfile")
	}
	if msg.Flags != OpenRDWR {
		t.Errorf("Flags = %d, want %d", msg.Flags, OpenRDWR)
	}
	if msg.Mode != 0644 {
		t.Errorf("Mode = %o, want 0644", msg.Mode)
	}
	if msg.GID != 1000 {
		t.Errorf("GID = %d, want 1000", msg.GID)
	}
}

func TestParseTsymlink(t *testing.T) {
	buf := make([]byte, 256)
	enc := NewEncoder(buf)
	enc.PutUint32(1)            // fid
	enc.PutString("link.txt")   // name
	enc.PutString("target.txt") // target
	enc.PutUint32(1000)         // gid

	dec := NewDecoder(enc.Bytes())
	msg, err := ParseTsymlink(dec)
	if err != nil {
		t.Fatalf("ParseTsymlink() error = %v", err)
	}

	if msg.Fid != 1 {
		t.Errorf("Fid = %d, want 1", msg.Fid)
	}
	if msg.Name != "link.txt" {
		t.Errorf("Name = %q, want %q", msg.Name, "link.txt")
	}
	if msg.Target != "target.txt" {
		t.Errorf("Target = %q, want %q", msg.Target, "target.txt")
	}
	if msg.GID != 1000 {
		t.Errorf("GID = %d, want 1000", msg.GID)
	}
}

func TestParseTmknod(t *testing.T) {
	buf := make([]byte, 256)
	enc := NewEncoder(buf)
	enc.PutUint32(1)                // dfid
	enc.PutString("dev")            // name
	enc.PutUint32(ModeIFCHR | 0666) // mode (character device)
	enc.PutUint32(1)                // major
	enc.PutUint32(3)                // minor (null device)
	enc.PutUint32(0)                // gid

	dec := NewDecoder(enc.Bytes())
	msg, err := ParseTmknod(dec)
	if err != nil {
		t.Fatalf("ParseTmknod() error = %v", err)
	}

	if msg.DFid != 1 {
		t.Errorf("DFid = %d, want 1", msg.DFid)
	}
	if msg.Name != "dev" {
		t.Errorf("Name = %q, want %q", msg.Name, "dev")
	}
	if msg.Mode != ModeIFCHR|0666 {
		t.Errorf("Mode = %o, want %o", msg.Mode, ModeIFCHR|0666)
	}
	if msg.Major != 1 {
		t.Errorf("Major = %d, want 1", msg.Major)
	}
	if msg.Minor != 3 {
		t.Errorf("Minor = %d, want 3", msg.Minor)
	}
	if msg.GID != 0 {
		t.Errorf("GID = %d, want 0", msg.GID)
	}
}

func TestParseTreadlink(t *testing.T) {
	buf := make([]byte, 256)
	enc := NewEncoder(buf)
	enc.PutUint32(7) // fid

	dec := NewDecoder(enc.Bytes())
	msg, err := ParseTreadlink(dec)
	if err != nil {
		t.Fatalf("ParseTreadlink() error = %v", err)
	}

	if msg.Fid != 7 {
		t.Errorf("Fid = %d, want 7", msg.Fid)
	}
}

func TestParseTlink(t *testing.T) {
	buf := make([]byte, 256)
	enc := NewEncoder(buf)
	enc.PutUint32(1)          // dfid
	enc.PutUint32(5)          // fid
	enc.PutString("hardlink") // name

	dec := NewDecoder(enc.Bytes())
	msg, err := ParseTlink(dec)
	if err != nil {
		t.Fatalf("ParseTlink() error = %v", err)
	}

	if msg.DFid != 1 {
		t.Errorf("DFid = %d, want 1", msg.DFid)
	}
	if msg.Fid != 5 {
		t.Errorf("Fid = %d, want 5", msg.Fid)
	}
	if msg.Name != "hardlink" {
		t.Errorf("Name = %q, want %q", msg.Name, "hardlink")
	}
}

func TestParseTfsync(t *testing.T) {
	buf := make([]byte, 256)
	enc := NewEncoder(buf)
	enc.PutUint32(10) // fid

	dec := NewDecoder(enc.Bytes())
	msg, err := ParseTfsync(dec)
	if err != nil {
		t.Fatalf("ParseTfsync() error = %v", err)
	}

	if msg.Fid != 10 {
		t.Errorf("Fid = %d, want 10", msg.Fid)
	}
}

func TestParseTflush(t *testing.T) {
	buf := make([]byte, 256)
	enc := NewEncoder(buf)
	enc.PutUint16(42) // oldtag

	dec := NewDecoder(enc.Bytes())
	msg, err := ParseTflush(dec)
	if err != nil {
		t.Fatalf("ParseTflush() error = %v", err)
	}

	if msg.OldTag != 42 {
		t.Errorf("OldTag = %d, want 42", msg.OldTag)
	}
}

func TestParseTlock(t *testing.T) {
	buf := make([]byte, 256)
	enc := NewEncoder(buf)
	enc.PutUint32(5)              // fid
	enc.PutUint8(LockTypeWRLCK)   // type
	enc.PutUint32(LockFlagsBlock) // flags
	enc.PutUint64(0)              // start
	enc.PutUint64(0)              // length (whole file)
	enc.PutUint32(1234)           // proc_id
	enc.PutString("localhost")    // client_id

	dec := NewDecoder(enc.Bytes())
	msg, err := ParseTlock(dec)
	if err != nil {
		t.Fatalf("ParseTlock() error = %v", err)
	}

	if msg.Fid != 5 {
		t.Errorf("Fid = %d, want 5", msg.Fid)
	}
	if msg.Type != LockTypeWRLCK {
		t.Errorf("Type = %d, want %d", msg.Type, LockTypeWRLCK)
	}
	if msg.Flags != LockFlagsBlock {
		t.Errorf("Flags = %d, want %d", msg.Flags, LockFlagsBlock)
	}
	if msg.Start != 0 {
		t.Errorf("Start = %d, want 0", msg.Start)
	}
	if msg.Length != 0 {
		t.Errorf("Length = %d, want 0", msg.Length)
	}
	if msg.ProcID != 1234 {
		t.Errorf("ProcID = %d, want 1234", msg.ProcID)
	}
	if msg.ClientID != "localhost" {
		t.Errorf("ClientID = %q, want %q", msg.ClientID, "localhost")
	}
}

func TestParseTgetlock(t *testing.T) {
	buf := make([]byte, 256)
	enc := NewEncoder(buf)
	enc.PutUint32(5)            // fid
	enc.PutUint8(LockTypeRDLCK) // type
	enc.PutUint64(100)          // start
	enc.PutUint64(200)          // length
	enc.PutUint32(5678)         // proc_id
	enc.PutString("client1")    // client_id

	dec := NewDecoder(enc.Bytes())
	msg, err := ParseTgetlock(dec)
	if err != nil {
		t.Fatalf("ParseTgetlock() error = %v", err)
	}

	if msg.Fid != 5 {
		t.Errorf("Fid = %d, want 5", msg.Fid)
	}
	if msg.Type != LockTypeRDLCK {
		t.Errorf("Type = %d, want %d", msg.Type, LockTypeRDLCK)
	}
	if msg.Start != 100 {
		t.Errorf("Start = %d, want 100", msg.Start)
	}
	if msg.Length != 200 {
		t.Errorf("Length = %d, want 200", msg.Length)
	}
	if msg.ProcID != 5678 {
		t.Errorf("ProcID = %d, want 5678", msg.ProcID)
	}
	if msg.ClientID != "client1" {
		t.Errorf("ClientID = %q, want %q", msg.ClientID, "client1")
	}
}

func TestParseTsetattr(t *testing.T) {
	buf := make([]byte, 256)
	enc := NewEncoder(buf)
	enc.PutUint32(3)                         // fid
	enc.PutUint32(SetattrMode | SetattrSize) // valid
	enc.PutUint32(0755)                      // mode
	enc.PutUint32(1000)                      // uid
	enc.PutUint32(1000)                      // gid
	enc.PutUint64(4096)                      // size
	enc.PutUint64(1609459200)                // atime_sec
	enc.PutUint64(0)                         // atime_nsec
	enc.PutUint64(1609459300)                // mtime_sec
	enc.PutUint64(0)                         // mtime_nsec

	dec := NewDecoder(enc.Bytes())
	msg, err := ParseTsetattr(dec)
	if err != nil {
		t.Fatalf("ParseTsetattr() error = %v", err)
	}

	if msg.Fid != 3 {
		t.Errorf("Fid = %d, want 3", msg.Fid)
	}
	if msg.Valid != SetattrMode|SetattrSize {
		t.Errorf("Valid = %d, want %d", msg.Valid, SetattrMode|SetattrSize)
	}
	if msg.Mode != 0755 {
		t.Errorf("Mode = %o, want 0755", msg.Mode)
	}
	if msg.Size != 4096 {
		t.Errorf("Size = %d, want 4096", msg.Size)
	}
}

func TestMarshalRgetattr(t *testing.T) {
	buf := make([]byte, 256)
	enc := NewEncoder(buf)

	stat := Stat{
		QID:       QID{Type: QtFile, Version: 1, Path: 100},
		Mode:      ModeIFREG | 0644,
		UID:       1000,
		GID:       1000,
		NLink:     1,
		RDev:      0,
		Size:      4096,
		BlkSize:   512,
		Blocks:    8,
		AtimeSec:  1609459200,
		AtimeNsec: 123456789,
		MtimeSec:  1609459300,
		MtimeNsec: 987654321,
		CtimeSec:  1609459100,
		CtimeNsec: 111222333,
	}
	msg := RgetattrMsg{Valid: GetattrBasic, Stat: stat}

	if err := MarshalRgetattr(enc, msg); err != nil {
		t.Fatalf("MarshalRgetattr() error = %v", err)
	}

	dec := NewDecoder(enc.Bytes())
	valid, _ := dec.GetUint64()
	gotStat, _ := dec.GetStat()

	if valid != GetattrBasic {
		t.Errorf("Valid = %d, want %d", valid, GetattrBasic)
	}
	if gotStat != stat {
		t.Errorf("Stat = %+v, want %+v", gotStat, stat)
	}
}

func TestMarshalRlock(t *testing.T) {
	buf := make([]byte, 256)
	enc := NewEncoder(buf)

	if err := MarshalRlock(enc, LockSuccess); err != nil {
		t.Fatalf("MarshalRlock() error = %v", err)
	}

	dec := NewDecoder(enc.Bytes())
	status, _ := dec.GetUint8()

	if status != LockSuccess {
		t.Errorf("status = %d, want %d", status, LockSuccess)
	}
}

func TestMarshalRgetlock(t *testing.T) {
	buf := make([]byte, 256)
	enc := NewEncoder(buf)

	msg := RgetlockMsg{
		Type:     LockTypeUNLCK,
		Start:    0,
		Length:   0,
		ProcID:   0,
		ClientID: "",
	}

	if err := MarshalRgetlock(enc, msg); err != nil {
		t.Fatalf("MarshalRgetlock() error = %v", err)
	}

	dec := NewDecoder(enc.Bytes())
	typ, _ := dec.GetUint8()
	start, _ := dec.GetUint64()
	length, _ := dec.GetUint64()
	procID, _ := dec.GetUint32()
	clientID, _ := dec.GetString()

	if typ != LockTypeUNLCK {
		t.Errorf("Type = %d, want %d", typ, LockTypeUNLCK)
	}
	if start != 0 || length != 0 || procID != 0 || clientID != "" {
		t.Errorf("lock fields don't match")
	}
}

func TestMarshalRreadlink(t *testing.T) {
	buf := make([]byte, 256)
	enc := NewEncoder(buf)

	target := "/usr/lib/libfoo.so"
	if err := MarshalRreadlink(enc, target); err != nil {
		t.Fatalf("MarshalRreadlink() error = %v", err)
	}

	dec := NewDecoder(enc.Bytes())
	got, _ := dec.GetString()

	if got != target {
		t.Errorf("target = %q, want %q", got, target)
	}
}

func TestMarshalRmkdir(t *testing.T) {
	buf := make([]byte, 256)
	enc := NewEncoder(buf)

	qid := QID{Type: QtDir, Version: 0, Path: 12345}
	if err := MarshalRmkdir(enc, qid); err != nil {
		t.Fatalf("MarshalRmkdir() error = %v", err)
	}

	dec := NewDecoder(enc.Bytes())
	got, _ := dec.GetQID()

	if got != qid {
		t.Errorf("QID = %+v, want %+v", got, qid)
	}
}

func TestMarshalRwrite(t *testing.T) {
	buf := make([]byte, 256)
	enc := NewEncoder(buf)

	if err := MarshalRwrite(enc, 1024); err != nil {
		t.Fatalf("MarshalRwrite() error = %v", err)
	}

	dec := NewDecoder(enc.Bytes())
	count, _ := dec.GetUint32()

	if count != 1024 {
		t.Errorf("count = %d, want 1024", count)
	}
}

// Test error handling for decoder underflow
func TestDecoderUnderflowErrors(t *testing.T) {
	tests := []struct {
		name  string
		parse func(*Decoder) error
		buf   []byte
	}{
		{
			name:  "ParseTversion short buffer",
			parse: func(d *Decoder) error { _, err := ParseTversion(d); return err },
			buf:   []byte{0x00}, // too short for msize
		},
		{
			name:  "ParseTattach short buffer",
			parse: func(d *Decoder) error { _, err := ParseTattach(d); return err },
			buf:   []byte{0x00, 0x00, 0x00}, // too short
		},
		{
			name:  "ParseTwalk short buffer",
			parse: func(d *Decoder) error { _, err := ParseTwalk(d); return err },
			buf:   []byte{0x00}, // too short
		},
		{
			name:  "ParseTlopen short buffer",
			parse: func(d *Decoder) error { _, err := ParseTlopen(d); return err },
			buf:   []byte{0x00, 0x00, 0x00}, // too short
		},
		{
			name:  "ParseTread short buffer",
			parse: func(d *Decoder) error { _, err := ParseTread(d); return err },
			buf:   []byte{0x00}, // too short
		},
		{
			name:  "ParseTwrite short buffer",
			parse: func(d *Decoder) error { _, err := ParseTwrite(d); return err },
			buf:   []byte{0x00}, // too short
		},
		{
			name:  "ParseTclunk short buffer",
			parse: func(d *Decoder) error { _, err := ParseTclunk(d); return err },
			buf:   []byte{0x00, 0x00}, // too short
		},
		{
			name:  "ParseTgetattr short buffer",
			parse: func(d *Decoder) error { _, err := ParseTgetattr(d); return err },
			buf:   []byte{0x00}, // too short
		},
		{
			name:  "ParseTsetattr short buffer",
			parse: func(d *Decoder) error { _, err := ParseTsetattr(d); return err },
			buf:   []byte{0x00}, // too short
		},
		{
			name:  "ParseTstatfs short buffer",
			parse: func(d *Decoder) error { _, err := ParseTstatfs(d); return err },
			buf:   []byte{0x00, 0x00}, // too short
		},
		{
			name:  "ParseTreaddir short buffer",
			parse: func(d *Decoder) error { _, err := ParseTreaddir(d); return err },
			buf:   []byte{0x00}, // too short
		},
		{
			name:  "ParseTmkdir short buffer",
			parse: func(d *Decoder) error { _, err := ParseTmkdir(d); return err },
			buf:   []byte{0x00}, // too short
		},
		{
			name:  "ParseTunlinkat short buffer",
			parse: func(d *Decoder) error { _, err := ParseTunlinkat(d); return err },
			buf:   []byte{0x00}, // too short
		},
		{
			name:  "ParseTrenameat short buffer",
			parse: func(d *Decoder) error { _, err := ParseTrenameat(d); return err },
			buf:   []byte{0x00}, // too short
		},
		{
			name:  "ParseTlcreate short buffer",
			parse: func(d *Decoder) error { _, err := ParseTlcreate(d); return err },
			buf:   []byte{0x00}, // too short
		},
		{
			name:  "ParseTsymlink short buffer",
			parse: func(d *Decoder) error { _, err := ParseTsymlink(d); return err },
			buf:   []byte{0x00}, // too short
		},
		{
			name:  "ParseTmknod short buffer",
			parse: func(d *Decoder) error { _, err := ParseTmknod(d); return err },
			buf:   []byte{0x00}, // too short
		},
		{
			name:  "ParseTreadlink short buffer",
			parse: func(d *Decoder) error { _, err := ParseTreadlink(d); return err },
			buf:   []byte{0x00, 0x00}, // too short
		},
		{
			name:  "ParseTlink short buffer",
			parse: func(d *Decoder) error { _, err := ParseTlink(d); return err },
			buf:   []byte{0x00}, // too short
		},
		{
			name:  "ParseTfsync short buffer",
			parse: func(d *Decoder) error { _, err := ParseTfsync(d); return err },
			buf:   []byte{0x00, 0x00}, // too short
		},
		{
			name:  "ParseTflush short buffer",
			parse: func(d *Decoder) error { _, err := ParseTflush(d); return err },
			buf:   []byte{0x00}, // too short
		},
		{
			name:  "ParseTlock short buffer",
			parse: func(d *Decoder) error { _, err := ParseTlock(d); return err },
			buf:   []byte{0x00}, // too short
		},
		{
			name:  "ParseTgetlock short buffer",
			parse: func(d *Decoder) error { _, err := ParseTgetlock(d); return err },
			buf:   []byte{0x00}, // too short
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dec := NewDecoder(tt.buf)
			err := tt.parse(dec)
			if err == nil {
				t.Errorf("%s should fail with short buffer", tt.name)
			}
		})
	}
}

// Test encoder overflow errors
func TestEncoderOverflowErrors(t *testing.T) {
	// Very small buffer to trigger overflow
	buf := make([]byte, 2)
	enc := NewEncoder(buf)

	// First write succeeds
	if err := enc.PutUint16(42); err != nil {
		t.Errorf("PutUint16 should succeed: %v", err)
	}

	// These should all fail
	if err := enc.PutUint8(1); err != ErrMessageTooLarge {
		t.Errorf("PutUint8 should fail with ErrMessageTooLarge")
	}
	if err := enc.PutUint16(1); err != ErrMessageTooLarge {
		t.Errorf("PutUint16 should fail with ErrMessageTooLarge")
	}
	if err := enc.PutUint32(1); err != ErrMessageTooLarge {
		t.Errorf("PutUint32 should fail with ErrMessageTooLarge")
	}
	if err := enc.PutUint64(1); err != ErrMessageTooLarge {
		t.Errorf("PutUint64 should fail with ErrMessageTooLarge")
	}
	if err := enc.PutString("test"); err != ErrMessageTooLarge {
		t.Errorf("PutString should fail with ErrMessageTooLarge")
	}
	if err := enc.PutBytes([]byte{1, 2, 3}); err != ErrMessageTooLarge {
		t.Errorf("PutBytes should fail with ErrMessageTooLarge")
	}
	if err := enc.PutQID(QID{}); err != ErrMessageTooLarge {
		t.Errorf("PutQID should fail with ErrMessageTooLarge")
	}
}

// Test encoder/decoder Reset methods
func TestEncoderReset(t *testing.T) {
	buf := make([]byte, 100)
	enc := NewEncoder(buf)

	enc.PutUint32(12345)
	if enc.Len() != 4 {
		t.Errorf("Len() = %d, want 4", enc.Len())
	}

	enc.Reset()
	if enc.Len() != 0 {
		t.Errorf("After Reset(), Len() = %d, want 0", enc.Len())
	}
}

func TestDecoderReset(t *testing.T) {
	buf1 := []byte{1, 2, 3, 4}
	buf2 := []byte{5, 6, 7, 8}
	dec := NewDecoder(buf1)

	dec.GetUint16()
	if dec.Remaining() != 2 {
		t.Errorf("Remaining() = %d, want 2", dec.Remaining())
	}

	dec.Reset(buf2)
	if dec.Remaining() != 4 {
		t.Errorf("After Reset(), Remaining() = %d, want 4", dec.Remaining())
	}

	val, _ := dec.GetUint8()
	if val != 5 {
		t.Errorf("After Reset(), first byte = %d, want 5", val)
	}
}

// Test decoder Skip method
func TestDecoderSkip(t *testing.T) {
	buf := []byte{1, 2, 3, 4, 5, 6, 7, 8}
	dec := NewDecoder(buf)

	if err := dec.Skip(3); err != nil {
		t.Errorf("Skip(3) error = %v", err)
	}
	if dec.Remaining() != 5 {
		t.Errorf("After Skip(3), Remaining() = %d, want 5", dec.Remaining())
	}

	// Skip past end should fail
	if err := dec.Skip(10); err != ErrMessageTooShort {
		t.Errorf("Skip(10) should fail with ErrMessageTooShort")
	}
}

// Test ReadMessage edge cases
func TestReadMessageErrors(t *testing.T) {
	// Message with size less than header
	smallMsg := make([]byte, 7)
	binary.LittleEndian.PutUint32(smallMsg[0:4], 5) // size = 5 < HeaderSize
	smallMsg[4] = Tversion
	binary.LittleEndian.PutUint16(smallMsg[5:7], NoTag)

	buf := bytes.NewReader(smallMsg)
	_, err := ReadMessage(buf, DefaultMsize)
	if err != ErrInvalidMessage {
		t.Errorf("ReadMessage with small size should return ErrInvalidMessage, got %v", err)
	}

	// Message exceeding max size
	bigMsg := make([]byte, 7)
	binary.LittleEndian.PutUint32(bigMsg[0:4], DefaultMsize+100)
	bigMsg[4] = Tversion
	binary.LittleEndian.PutUint16(bigMsg[5:7], NoTag)

	buf = bytes.NewReader(bigMsg)
	_, err = ReadMessage(buf, DefaultMsize)
	if err != ErrMessageTooLarge {
		t.Errorf("ReadMessage exceeding max size should return ErrMessageTooLarge, got %v", err)
	}
}

// Test PutString with string too long
func TestPutStringTooLong(t *testing.T) {
	buf := make([]byte, 100000)
	enc := NewEncoder(buf)

	// Create a string longer than 65535 bytes
	longStr := make([]byte, 70000)
	for i := range longStr {
		longStr[i] = 'a'
	}

	err := enc.PutString(string(longStr))
	if err == nil {
		t.Error("PutString with string > 65535 bytes should fail")
	}
}

// Test all additional message type names
func TestMessageTypeNameAdditional(t *testing.T) {
	tests := []struct {
		typ  uint8
		want string
	}{
		{Tflush, "Tflush"},
		{Rflush, "Rflush"},
		{Tlcreate, "Tlcreate"},
		{Rlcreate, "Rlcreate"},
		{Tsymlink, "Tsymlink"},
		{Rsymlink, "Rsymlink"},
		{Tmknod, "Tmknod"},
		{Rmknod, "Rmknod"},
		{Trename, "unknown"}, // Not in switch
		{Treadlink, "Treadlink"},
		{Rreadlink, "Rreadlink"},
		{Txattrwalk, "Txattrwalk"},
		{Rxattrwalk, "Rxattrwalk"},
		{Tfsync, "Tfsync"},
		{Rfsync, "Rfsync"},
		{Tlock, "Tlock"},
		{Rlock, "Rlock"},
		{Tgetlock, "Tgetlock"},
		{Rgetlock, "Rgetlock"},
		{Tlink, "Tlink"},
		{Rlink, "Rlink"},
		{Trenameat, "Trenameat"},
		{Rrenameat, "Rrenameat"},
		{Tremove, "unknown"}, // Not in switch
		{Tstat, "unknown"},   // Not in switch
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := MessageTypeName(tt.typ); got != tt.want {
				t.Errorf("MessageTypeName(%d) = %q, want %q", tt.typ, got, tt.want)
			}
		})
	}
}

// Test ParseTattach without NUname (9P2000 style)
func TestParseTattachWithoutNUname(t *testing.T) {
	buf := make([]byte, 256)
	enc := NewEncoder(buf)
	enc.PutUint32(0)      // fid
	enc.PutUint32(NOFID)  // afid
	enc.PutString("root") // uname
	enc.PutString("/")    // aname
	// No n_uname - 9P2000 style

	dec := NewDecoder(enc.Bytes())
	msg, err := ParseTattach(dec)
	if err != nil {
		t.Fatalf("ParseTattach() error = %v", err)
	}

	if msg.NUname != 0 {
		t.Errorf("NUname = %d, want 0 (not present)", msg.NUname)
	}
}

// Test empty Twalk (clone operation)
func TestParseTwalkEmpty(t *testing.T) {
	buf := make([]byte, 256)
	enc := NewEncoder(buf)
	enc.PutUint32(1) // fid
	enc.PutUint32(2) // newfid
	enc.PutUint16(0) // nwname = 0 (clone)

	dec := NewDecoder(enc.Bytes())
	msg, err := ParseTwalk(dec)
	if err != nil {
		t.Fatalf("ParseTwalk() error = %v", err)
	}

	if msg.Fid != 1 {
		t.Errorf("Fid = %d, want 1", msg.Fid)
	}
	if msg.NewFid != 2 {
		t.Errorf("NewFid = %d, want 2", msg.NewFid)
	}
	if len(msg.Names) != 0 {
		t.Errorf("len(Names) = %d, want 0", len(msg.Names))
	}
}

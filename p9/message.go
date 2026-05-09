package p9

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// Message header size: 4 (size) + 1 (type) + 2 (tag)
const HeaderSize = 7

// Maximum message size (default, can be negotiated)
const DefaultMsize = 8192

// NoTag is a special tag used for Tversion/Rversion messages
const NoTag = 0xFFFF

// ErrMessageTooShort indicates the message buffer is too short.
var ErrMessageTooShort = errors.New("message too short")

// ErrMessageTooLarge indicates the message exceeds the maximum size.
var ErrMessageTooLarge = errors.New("message too large")

// ErrInvalidMessage indicates the message format is invalid.
var ErrInvalidMessage = errors.New("invalid message format")

// Header represents a 9P message header.
type Header struct {
	Size uint32 // Total message size including header
	Type uint8  // Message type
	Tag  uint16 // Request tag for matching responses
}

// ParseHeader parses a 9P message header from a byte slice.
func ParseHeader(b []byte) (Header, error) {
	if len(b) < HeaderSize {
		return Header{}, ErrMessageTooShort
	}
	return Header{
		Size: binary.LittleEndian.Uint32(b[0:4]),
		Type: b[4],
		Tag:  binary.LittleEndian.Uint16(b[5:7]),
	}, nil
}

// MarshalHeader writes a 9P message header to a byte slice.
func MarshalHeader(b []byte, h Header) {
	binary.LittleEndian.PutUint32(b[0:4], h.Size)
	b[4] = h.Type
	binary.LittleEndian.PutUint16(b[5:7], h.Tag)
}

// Encoder handles encoding of 9P messages.
type Encoder struct {
	buf []byte
	pos int
}

// NewEncoder creates a new encoder with the given buffer.
func NewEncoder(buf []byte) *Encoder {
	return &Encoder{buf: buf, pos: 0}
}

// Reset resets the encoder to write from the beginning.
func (e *Encoder) Reset() {
	e.pos = 0
}

// Bytes returns the encoded bytes.
func (e *Encoder) Bytes() []byte {
	return e.buf[:e.pos]
}

// Len returns the number of bytes written.
func (e *Encoder) Len() int {
	return e.pos
}

// PutUint8 writes a uint8.
func (e *Encoder) PutUint8(v uint8) error {
	if e.pos+1 > len(e.buf) {
		return ErrMessageTooLarge
	}
	e.buf[e.pos] = v
	e.pos++
	return nil
}

// PutUint16 writes a little-endian uint16.
func (e *Encoder) PutUint16(v uint16) error {
	if e.pos+2 > len(e.buf) {
		return ErrMessageTooLarge
	}
	binary.LittleEndian.PutUint16(e.buf[e.pos:], v)
	e.pos += 2
	return nil
}

// PutUint32 writes a little-endian uint32.
func (e *Encoder) PutUint32(v uint32) error {
	if e.pos+4 > len(e.buf) {
		return ErrMessageTooLarge
	}
	binary.LittleEndian.PutUint32(e.buf[e.pos:], v)
	e.pos += 4
	return nil
}

// PutUint64 writes a little-endian uint64.
func (e *Encoder) PutUint64(v uint64) error {
	if e.pos+8 > len(e.buf) {
		return ErrMessageTooLarge
	}
	binary.LittleEndian.PutUint64(e.buf[e.pos:], v)
	e.pos += 8
	return nil
}

// PutString writes a string (2-byte length prefix + data).
func (e *Encoder) PutString(s string) error {
	if len(s) > 65535 {
		return errors.New("string too long")
	}
	if e.pos+2+len(s) > len(e.buf) {
		return ErrMessageTooLarge
	}
	binary.LittleEndian.PutUint16(e.buf[e.pos:], uint16(len(s)))
	e.pos += 2
	copy(e.buf[e.pos:], s)
	e.pos += len(s)
	return nil
}

// PutBytes writes raw bytes.
func (e *Encoder) PutBytes(b []byte) error {
	if e.pos+len(b) > len(e.buf) {
		return ErrMessageTooLarge
	}
	copy(e.buf[e.pos:], b)
	e.pos += len(b)
	return nil
}

// PutQID writes a QID (1 + 4 + 8 = 13 bytes).
func (e *Encoder) PutQID(q QID) error {
	if e.pos+13 > len(e.buf) {
		return ErrMessageTooLarge
	}
	e.buf[e.pos] = q.Type
	binary.LittleEndian.PutUint32(e.buf[e.pos+1:], q.Version)
	binary.LittleEndian.PutUint64(e.buf[e.pos+5:], q.Path)
	e.pos += 13
	return nil
}

// PutStat writes a Stat structure for Rgetattr response.
// Reference: tinyemu-2019-12-21/virtio.c:2241-2251 (getattr marshalling)
// Note: nsec fields are marshalled as uint64 on the wire (matching C behavior),
// and trailing zeros are added for btime_sec, btime_nsec, gen, data_version.
func (e *Encoder) PutStat(st Stat) error {
	if err := e.PutQID(st.QID); err != nil {
		return err
	}
	if err := e.PutUint32(st.Mode); err != nil {
		return err
	}
	if err := e.PutUint32(st.UID); err != nil {
		return err
	}
	if err := e.PutUint32(st.GID); err != nil {
		return err
	}
	if err := e.PutUint64(st.NLink); err != nil {
		return err
	}
	if err := e.PutUint64(st.RDev); err != nil {
		return err
	}
	if err := e.PutUint64(st.Size); err != nil {
		return err
	}
	if err := e.PutUint64(st.BlkSize); err != nil {
		return err
	}
	if err := e.PutUint64(st.Blocks); err != nil {
		return err
	}
	if err := e.PutUint64(st.AtimeSec); err != nil {
		return err
	}
	// nsec fields are uint64 on the wire (C casts uint32 to uint64)
	if err := e.PutUint64(uint64(st.AtimeNsec)); err != nil {
		return err
	}
	if err := e.PutUint64(st.MtimeSec); err != nil {
		return err
	}
	if err := e.PutUint64(uint64(st.MtimeNsec)); err != nil {
		return err
	}
	if err := e.PutUint64(st.CtimeSec); err != nil {
		return err
	}
	if err := e.PutUint64(uint64(st.CtimeNsec)); err != nil {
		return err
	}
	// Trailing zeros: btime_sec, btime_nsec, gen, data_version
	if err := e.PutUint64(0); err != nil {
		return err
	}
	if err := e.PutUint64(0); err != nil {
		return err
	}
	if err := e.PutUint64(0); err != nil {
		return err
	}
	return e.PutUint64(0)
}

// Decoder handles decoding of 9P messages.
type Decoder struct {
	buf []byte
	pos int
}

// NewDecoder creates a new decoder for the given buffer.
func NewDecoder(buf []byte) *Decoder {
	return &Decoder{buf: buf, pos: 0}
}

// Reset resets the decoder to read from the beginning.
func (d *Decoder) Reset(buf []byte) {
	d.buf = buf
	d.pos = 0
}

// Remaining returns the number of bytes remaining.
func (d *Decoder) Remaining() int {
	return len(d.buf) - d.pos
}

// Skip skips n bytes.
func (d *Decoder) Skip(n int) error {
	if d.pos+n > len(d.buf) {
		return ErrMessageTooShort
	}
	d.pos += n
	return nil
}

// GetUint8 reads a uint8.
func (d *Decoder) GetUint8() (uint8, error) {
	if d.pos+1 > len(d.buf) {
		return 0, ErrMessageTooShort
	}
	v := d.buf[d.pos]
	d.pos++
	return v, nil
}

// GetUint16 reads a little-endian uint16.
func (d *Decoder) GetUint16() (uint16, error) {
	if d.pos+2 > len(d.buf) {
		return 0, ErrMessageTooShort
	}
	v := binary.LittleEndian.Uint16(d.buf[d.pos:])
	d.pos += 2
	return v, nil
}

// GetUint32 reads a little-endian uint32.
func (d *Decoder) GetUint32() (uint32, error) {
	if d.pos+4 > len(d.buf) {
		return 0, ErrMessageTooShort
	}
	v := binary.LittleEndian.Uint32(d.buf[d.pos:])
	d.pos += 4
	return v, nil
}

// GetUint64 reads a little-endian uint64.
func (d *Decoder) GetUint64() (uint64, error) {
	if d.pos+8 > len(d.buf) {
		return 0, ErrMessageTooShort
	}
	v := binary.LittleEndian.Uint64(d.buf[d.pos:])
	d.pos += 8
	return v, nil
}

// GetString reads a string (2-byte length prefix + data).
func (d *Decoder) GetString() (string, error) {
	length, err := d.GetUint16()
	if err != nil {
		return "", err
	}
	if d.pos+int(length) > len(d.buf) {
		return "", ErrMessageTooShort
	}
	s := string(d.buf[d.pos : d.pos+int(length)])
	d.pos += int(length)
	return s, nil
}

// GetBytes reads a fixed number of raw bytes.
func (d *Decoder) GetBytes(n int) ([]byte, error) {
	if d.pos+n > len(d.buf) {
		return nil, ErrMessageTooShort
	}
	b := make([]byte, n)
	copy(b, d.buf[d.pos:d.pos+n])
	d.pos += n
	return b, nil
}

// GetQID reads a QID (13 bytes).
func (d *Decoder) GetQID() (QID, error) {
	if d.pos+13 > len(d.buf) {
		return QID{}, ErrMessageTooShort
	}
	q := QID{
		Type:    d.buf[d.pos],
		Version: binary.LittleEndian.Uint32(d.buf[d.pos+1:]),
		Path:    binary.LittleEndian.Uint64(d.buf[d.pos+5:]),
	}
	d.pos += 13
	return q, nil
}

// GetStat reads a Stat structure from an Rgetattr response.
// Reference: tinyemu-2019-12-21/virtio.c:2241-2251 (getattr marshalling)
// Note: nsec fields are uint64 on the wire but stored as uint32 in the struct.
func (d *Decoder) GetStat() (Stat, error) {
	var st Stat
	var err error
	var nsec uint64

	st.QID, err = d.GetQID()
	if err != nil {
		return st, err
	}
	st.Mode, err = d.GetUint32()
	if err != nil {
		return st, err
	}
	st.UID, err = d.GetUint32()
	if err != nil {
		return st, err
	}
	st.GID, err = d.GetUint32()
	if err != nil {
		return st, err
	}
	st.NLink, err = d.GetUint64()
	if err != nil {
		return st, err
	}
	st.RDev, err = d.GetUint64()
	if err != nil {
		return st, err
	}
	st.Size, err = d.GetUint64()
	if err != nil {
		return st, err
	}
	st.BlkSize, err = d.GetUint64()
	if err != nil {
		return st, err
	}
	st.Blocks, err = d.GetUint64()
	if err != nil {
		return st, err
	}
	st.AtimeSec, err = d.GetUint64()
	if err != nil {
		return st, err
	}
	// nsec fields are uint64 on the wire, truncate to uint32 for storage
	nsec, err = d.GetUint64()
	if err != nil {
		return st, err
	}
	st.AtimeNsec = uint32(nsec)
	st.MtimeSec, err = d.GetUint64()
	if err != nil {
		return st, err
	}
	nsec, err = d.GetUint64()
	if err != nil {
		return st, err
	}
	st.MtimeNsec = uint32(nsec)
	st.CtimeSec, err = d.GetUint64()
	if err != nil {
		return st, err
	}
	nsec, err = d.GetUint64()
	if err != nil {
		return st, err
	}
	st.CtimeNsec = uint32(nsec)
	// Skip trailing zeros: btime_sec, btime_nsec, gen, data_version
	if err = d.Skip(32); err != nil {
		return st, err
	}
	return st, nil
}

// Message parsing functions for specific message types

// TversionMsg represents a Tversion request.
type TversionMsg struct {
	Msize   uint32
	Version string
}

// ParseTversion parses a Tversion message body.
func ParseTversion(d *Decoder) (TversionMsg, error) {
	var msg TversionMsg
	var err error
	msg.Msize, err = d.GetUint32()
	if err != nil {
		return msg, err
	}
	msg.Version, err = d.GetString()
	return msg, err
}

// RversionMsg represents an Rversion response.
type RversionMsg struct {
	Msize   uint32
	Version string
}

// MarshalRversion marshals an Rversion message.
func MarshalRversion(e *Encoder, msg RversionMsg) error {
	if err := e.PutUint32(msg.Msize); err != nil {
		return err
	}
	return e.PutString(msg.Version)
}

// TattachMsg represents a Tattach request.
type TattachMsg struct {
	Fid    uint32
	AFid   uint32 // Auth fid (usually NOFID)
	Uname  string
	Aname  string
	NUname uint32 // Numeric user ID (9P2000.L extension)
}

// NOFID is used when no fid is specified
const NOFID = 0xFFFFFFFF

// ParseTattach parses a Tattach message body.
func ParseTattach(d *Decoder) (TattachMsg, error) {
	var msg TattachMsg
	var err error
	msg.Fid, err = d.GetUint32()
	if err != nil {
		return msg, err
	}
	msg.AFid, err = d.GetUint32()
	if err != nil {
		return msg, err
	}
	msg.Uname, err = d.GetString()
	if err != nil {
		return msg, err
	}
	msg.Aname, err = d.GetString()
	if err != nil {
		return msg, err
	}
	// 9P2000.L extension: numeric user ID
	if d.Remaining() >= 4 {
		msg.NUname, err = d.GetUint32()
	}
	return msg, err
}

// RattachMsg represents a Rattach response.
type RattachMsg struct {
	QID QID
}

// MarshalRattach marshals a Rattach message.
func MarshalRattach(e *Encoder, msg RattachMsg) error {
	return e.PutQID(msg.QID)
}

// TwalkMsg represents a Twalk request.
type TwalkMsg struct {
	Fid    uint32
	NewFid uint32
	Names  []string
}

// ParseTwalk parses a Twalk message body.
func ParseTwalk(d *Decoder) (TwalkMsg, error) {
	var msg TwalkMsg
	var err error
	msg.Fid, err = d.GetUint32()
	if err != nil {
		return msg, err
	}
	msg.NewFid, err = d.GetUint32()
	if err != nil {
		return msg, err
	}
	nwname, err := d.GetUint16()
	if err != nil {
		return msg, err
	}
	msg.Names = make([]string, nwname)
	for i := 0; i < int(nwname); i++ {
		msg.Names[i], err = d.GetString()
		if err != nil {
			return msg, err
		}
	}
	return msg, nil
}

// RwalkMsg represents an Rwalk response.
type RwalkMsg struct {
	QIDs []QID
}

// MarshalRwalk marshals an Rwalk message.
func MarshalRwalk(e *Encoder, msg RwalkMsg) error {
	if err := e.PutUint16(uint16(len(msg.QIDs))); err != nil {
		return err
	}
	for _, qid := range msg.QIDs {
		if err := e.PutQID(qid); err != nil {
			return err
		}
	}
	return nil
}

// TlopenMsg represents a Tlopen request.
type TlopenMsg struct {
	Fid   uint32
	Flags uint32
}

// ParseTlopen parses a Tlopen message body.
func ParseTlopen(d *Decoder) (TlopenMsg, error) {
	var msg TlopenMsg
	var err error
	msg.Fid, err = d.GetUint32()
	if err != nil {
		return msg, err
	}
	msg.Flags, err = d.GetUint32()
	return msg, err
}

// RlopenMsg represents an Rlopen response.
type RlopenMsg struct {
	QID    QID
	IOUnit uint32
}

// MarshalRlopen marshals an Rlopen message.
func MarshalRlopen(e *Encoder, msg RlopenMsg) error {
	if err := e.PutQID(msg.QID); err != nil {
		return err
	}
	return e.PutUint32(msg.IOUnit)
}

// TlcreateMsg represents a Tlcreate request.
type TlcreateMsg struct {
	Fid   uint32
	Name  string
	Flags uint32
	Mode  uint32
	GID   uint32
}

// ParseTlcreate parses a Tlcreate message body.
func ParseTlcreate(d *Decoder) (TlcreateMsg, error) {
	var msg TlcreateMsg
	var err error
	msg.Fid, err = d.GetUint32()
	if err != nil {
		return msg, err
	}
	msg.Name, err = d.GetString()
	if err != nil {
		return msg, err
	}
	msg.Flags, err = d.GetUint32()
	if err != nil {
		return msg, err
	}
	msg.Mode, err = d.GetUint32()
	if err != nil {
		return msg, err
	}
	msg.GID, err = d.GetUint32()
	return msg, err
}

// TreadMsg represents a Tread request.
type TreadMsg struct {
	Fid    uint32
	Offset uint64
	Count  uint32
}

// ParseTread parses a Tread message body.
func ParseTread(d *Decoder) (TreadMsg, error) {
	var msg TreadMsg
	var err error
	msg.Fid, err = d.GetUint32()
	if err != nil {
		return msg, err
	}
	msg.Offset, err = d.GetUint64()
	if err != nil {
		return msg, err
	}
	msg.Count, err = d.GetUint32()
	return msg, err
}

// RreadMsg represents an Rread response.
type RreadMsg struct {
	Data []byte
}

// MarshalRread marshals an Rread message.
func MarshalRread(e *Encoder, data []byte) error {
	if err := e.PutUint32(uint32(len(data))); err != nil {
		return err
	}
	return e.PutBytes(data)
}

// TwriteMsg represents a Twrite request.
type TwriteMsg struct {
	Fid    uint32
	Offset uint64
	Data   []byte
}

// ParseTwrite parses a Twrite message body.
func ParseTwrite(d *Decoder) (TwriteMsg, error) {
	var msg TwriteMsg
	var err error
	msg.Fid, err = d.GetUint32()
	if err != nil {
		return msg, err
	}
	msg.Offset, err = d.GetUint64()
	if err != nil {
		return msg, err
	}
	count, err := d.GetUint32()
	if err != nil {
		return msg, err
	}
	msg.Data, err = d.GetBytes(int(count))
	return msg, err
}

// RwriteMsg represents an Rwrite response.
type RwriteMsg struct {
	Count uint32
}

// MarshalRwrite marshals an Rwrite message.
func MarshalRwrite(e *Encoder, count uint32) error {
	return e.PutUint32(count)
}

// TclunkMsg represents a Tclunk request.
type TclunkMsg struct {
	Fid uint32
}

// ParseTclunk parses a Tclunk message body.
func ParseTclunk(d *Decoder) (TclunkMsg, error) {
	fid, err := d.GetUint32()
	return TclunkMsg{Fid: fid}, err
}

// TgetattrMsg represents a Tgetattr request.
type TgetattrMsg struct {
	Fid     uint32
	ReqMask uint64
}

// ParseTgetattr parses a Tgetattr message body.
func ParseTgetattr(d *Decoder) (TgetattrMsg, error) {
	var msg TgetattrMsg
	var err error
	msg.Fid, err = d.GetUint32()
	if err != nil {
		return msg, err
	}
	msg.ReqMask, err = d.GetUint64()
	return msg, err
}

// RgetattrMsg represents an Rgetattr response.
type RgetattrMsg struct {
	Valid uint64
	Stat  Stat
}

// MarshalRgetattr marshals an Rgetattr message.
func MarshalRgetattr(e *Encoder, msg RgetattrMsg) error {
	if err := e.PutUint64(msg.Valid); err != nil {
		return err
	}
	return e.PutStat(msg.Stat)
}

// TsetattrMsg represents a Tsetattr request.
type TsetattrMsg struct {
	Fid       uint32
	Valid     uint32
	Mode      uint32
	UID       uint32
	GID       uint32
	Size      uint64
	AtimeSec  uint64
	AtimeNsec uint64
	MtimeSec  uint64
	MtimeNsec uint64
}

// ParseTsetattr parses a Tsetattr message body.
func ParseTsetattr(d *Decoder) (TsetattrMsg, error) {
	var msg TsetattrMsg
	var err error
	msg.Fid, err = d.GetUint32()
	if err != nil {
		return msg, err
	}
	msg.Valid, err = d.GetUint32()
	if err != nil {
		return msg, err
	}
	msg.Mode, err = d.GetUint32()
	if err != nil {
		return msg, err
	}
	msg.UID, err = d.GetUint32()
	if err != nil {
		return msg, err
	}
	msg.GID, err = d.GetUint32()
	if err != nil {
		return msg, err
	}
	msg.Size, err = d.GetUint64()
	if err != nil {
		return msg, err
	}
	msg.AtimeSec, err = d.GetUint64()
	if err != nil {
		return msg, err
	}
	msg.AtimeNsec, err = d.GetUint64()
	if err != nil {
		return msg, err
	}
	msg.MtimeSec, err = d.GetUint64()
	if err != nil {
		return msg, err
	}
	msg.MtimeNsec, err = d.GetUint64()
	return msg, err
}

// TstatfsMsg represents a Tstatfs request.
type TstatfsMsg struct {
	Fid uint32
}

// ParseTstatfs parses a Tstatfs message body.
func ParseTstatfs(d *Decoder) (TstatfsMsg, error) {
	fid, err := d.GetUint32()
	return TstatfsMsg{Fid: fid}, err
}

// MarshalRstatfs marshals an Rstatfs message.
func MarshalRstatfs(e *Encoder, st StatFS) error {
	if err := e.PutUint32(st.Type); err != nil {
		return err
	}
	if err := e.PutUint32(st.BSize); err != nil {
		return err
	}
	if err := e.PutUint64(st.Blocks); err != nil {
		return err
	}
	if err := e.PutUint64(st.BFree); err != nil {
		return err
	}
	if err := e.PutUint64(st.BAvail); err != nil {
		return err
	}
	if err := e.PutUint64(st.Files); err != nil {
		return err
	}
	if err := e.PutUint64(st.FFree); err != nil {
		return err
	}
	if err := e.PutUint64(st.FSid); err != nil {
		return err
	}
	return e.PutUint32(st.NameLen)
}

// TreaddirMsg represents a Treaddir request.
type TreaddirMsg struct {
	Fid    uint32
	Offset uint64
	Count  uint32
}

// ParseTreaddir parses a Treaddir message body.
func ParseTreaddir(d *Decoder) (TreaddirMsg, error) {
	var msg TreaddirMsg
	var err error
	msg.Fid, err = d.GetUint32()
	if err != nil {
		return msg, err
	}
	msg.Offset, err = d.GetUint64()
	if err != nil {
		return msg, err
	}
	msg.Count, err = d.GetUint32()
	return msg, err
}

// MarshalDirent marshals a single directory entry.
// Returns the number of bytes written.
func MarshalDirent(e *Encoder, d Dirent) error {
	if err := e.PutQID(d.QID); err != nil {
		return err
	}
	if err := e.PutUint64(d.Offset); err != nil {
		return err
	}
	if err := e.PutUint8(d.Type); err != nil {
		return err
	}
	return e.PutString(d.Name)
}

// DirentSize returns the encoded size of a directory entry.
func DirentSize(name string) int {
	return 13 + 8 + 1 + 2 + len(name) // QID + offset + type + strlen + name
}

// TmkdirMsg represents a Tmkdir request.
type TmkdirMsg struct {
	DFid uint32
	Name string
	Mode uint32
	GID  uint32
}

// ParseTmkdir parses a Tmkdir message body.
func ParseTmkdir(d *Decoder) (TmkdirMsg, error) {
	var msg TmkdirMsg
	var err error
	msg.DFid, err = d.GetUint32()
	if err != nil {
		return msg, err
	}
	msg.Name, err = d.GetString()
	if err != nil {
		return msg, err
	}
	msg.Mode, err = d.GetUint32()
	if err != nil {
		return msg, err
	}
	msg.GID, err = d.GetUint32()
	return msg, err
}

// RmkdirMsg represents an Rmkdir response.
type RmkdirMsg struct {
	QID QID
}

// MarshalRmkdir marshals an Rmkdir message.
func MarshalRmkdir(e *Encoder, qid QID) error {
	return e.PutQID(qid)
}

// TunlinkatMsg represents a Tunlinkat request.
type TunlinkatMsg struct {
	DFid  uint32
	Name  string
	Flags uint32
}

// ParseTunlinkat parses a Tunlinkat message body.
func ParseTunlinkat(d *Decoder) (TunlinkatMsg, error) {
	var msg TunlinkatMsg
	var err error
	msg.DFid, err = d.GetUint32()
	if err != nil {
		return msg, err
	}
	msg.Name, err = d.GetString()
	if err != nil {
		return msg, err
	}
	msg.Flags, err = d.GetUint32()
	return msg, err
}

// TrenameatMsg represents a Trenameat request.
type TrenameatMsg struct {
	OldDFid uint32
	OldName string
	NewDFid uint32
	NewName string
}

// ParseTrenameat parses a Trenameat message body.
func ParseTrenameat(d *Decoder) (TrenameatMsg, error) {
	var msg TrenameatMsg
	var err error
	msg.OldDFid, err = d.GetUint32()
	if err != nil {
		return msg, err
	}
	msg.OldName, err = d.GetString()
	if err != nil {
		return msg, err
	}
	msg.NewDFid, err = d.GetUint32()
	if err != nil {
		return msg, err
	}
	msg.NewName, err = d.GetString()
	return msg, err
}

// RerrorMsg represents an Rerror response.
type RerrorMsg struct {
	Ecode uint32
}

// MarshalRerror marshals an Rerror message.
func MarshalRerror(e *Encoder, ecode uint32) error {
	return e.PutUint32(ecode)
}

// TflushMsg represents a Tflush request.
type TflushMsg struct {
	OldTag uint16
}

// ParseTflush parses a Tflush message body.
func ParseTflush(d *Decoder) (TflushMsg, error) {
	tag, err := d.GetUint16()
	return TflushMsg{OldTag: tag}, err
}

// TsymlinkMsg represents a Tsymlink request.
type TsymlinkMsg struct {
	Fid    uint32
	Name   string
	Target string
	GID    uint32
}

// ParseTsymlink parses a Tsymlink message body.
func ParseTsymlink(d *Decoder) (TsymlinkMsg, error) {
	var msg TsymlinkMsg
	var err error
	msg.Fid, err = d.GetUint32()
	if err != nil {
		return msg, err
	}
	msg.Name, err = d.GetString()
	if err != nil {
		return msg, err
	}
	msg.Target, err = d.GetString()
	if err != nil {
		return msg, err
	}
	msg.GID, err = d.GetUint32()
	return msg, err
}

// TmknodMsg represents a Tmknod request.
type TmknodMsg struct {
	DFid  uint32
	Name  string
	Mode  uint32
	Major uint32
	Minor uint32
	GID   uint32
}

// ParseTmknod parses a Tmknod message body.
func ParseTmknod(d *Decoder) (TmknodMsg, error) {
	var msg TmknodMsg
	var err error
	msg.DFid, err = d.GetUint32()
	if err != nil {
		return msg, err
	}
	msg.Name, err = d.GetString()
	if err != nil {
		return msg, err
	}
	msg.Mode, err = d.GetUint32()
	if err != nil {
		return msg, err
	}
	msg.Major, err = d.GetUint32()
	if err != nil {
		return msg, err
	}
	msg.Minor, err = d.GetUint32()
	if err != nil {
		return msg, err
	}
	msg.GID, err = d.GetUint32()
	return msg, err
}

// TreadlinkMsg represents a Treadlink request.
type TreadlinkMsg struct {
	Fid uint32
}

// ParseTreadlink parses a Treadlink message body.
func ParseTreadlink(d *Decoder) (TreadlinkMsg, error) {
	fid, err := d.GetUint32()
	return TreadlinkMsg{Fid: fid}, err
}

// MarshalRreadlink marshals an Rreadlink message.
func MarshalRreadlink(e *Encoder, target string) error {
	return e.PutString(target)
}

// TlinkMsg represents a Tlink request.
type TlinkMsg struct {
	DFid uint32
	Fid  uint32
	Name string
}

// ParseTlink parses a Tlink message body.
func ParseTlink(d *Decoder) (TlinkMsg, error) {
	var msg TlinkMsg
	var err error
	msg.DFid, err = d.GetUint32()
	if err != nil {
		return msg, err
	}
	msg.Fid, err = d.GetUint32()
	if err != nil {
		return msg, err
	}
	msg.Name, err = d.GetString()
	return msg, err
}

// TfsyncMsg represents a Tfsync request.
type TfsyncMsg struct {
	Fid uint32
}

// ParseTfsync parses a Tfsync message body.
func ParseTfsync(d *Decoder) (TfsyncMsg, error) {
	fid, err := d.GetUint32()
	return TfsyncMsg{Fid: fid}, err
}

// TlockMsg represents a Tlock request.
type TlockMsg struct {
	Fid      uint32
	Type     uint8
	Flags    uint32
	Start    uint64
	Length   uint64
	ProcID   uint32
	ClientID string
}

// ParseTlock parses a Tlock message body.
func ParseTlock(d *Decoder) (TlockMsg, error) {
	var msg TlockMsg
	var err error
	msg.Fid, err = d.GetUint32()
	if err != nil {
		return msg, err
	}
	msg.Type, err = d.GetUint8()
	if err != nil {
		return msg, err
	}
	msg.Flags, err = d.GetUint32()
	if err != nil {
		return msg, err
	}
	msg.Start, err = d.GetUint64()
	if err != nil {
		return msg, err
	}
	msg.Length, err = d.GetUint64()
	if err != nil {
		return msg, err
	}
	msg.ProcID, err = d.GetUint32()
	if err != nil {
		return msg, err
	}
	msg.ClientID, err = d.GetString()
	return msg, err
}

// MarshalRlock marshals an Rlock message.
func MarshalRlock(e *Encoder, status uint8) error {
	return e.PutUint8(status)
}

// TgetlockMsg represents a Tgetlock request.
type TgetlockMsg struct {
	Fid      uint32
	Type     uint8
	Start    uint64
	Length   uint64
	ProcID   uint32
	ClientID string
}

// ParseTgetlock parses a Tgetlock message body.
func ParseTgetlock(d *Decoder) (TgetlockMsg, error) {
	var msg TgetlockMsg
	var err error
	msg.Fid, err = d.GetUint32()
	if err != nil {
		return msg, err
	}
	msg.Type, err = d.GetUint8()
	if err != nil {
		return msg, err
	}
	msg.Start, err = d.GetUint64()
	if err != nil {
		return msg, err
	}
	msg.Length, err = d.GetUint64()
	if err != nil {
		return msg, err
	}
	msg.ProcID, err = d.GetUint32()
	if err != nil {
		return msg, err
	}
	msg.ClientID, err = d.GetString()
	return msg, err
}

// RgetlockMsg represents an Rgetlock response.
type RgetlockMsg struct {
	Type     uint8
	Start    uint64
	Length   uint64
	ProcID   uint32
	ClientID string
}

// MarshalRgetlock marshals an Rgetlock message.
func MarshalRgetlock(e *Encoder, msg RgetlockMsg) error {
	if err := e.PutUint8(msg.Type); err != nil {
		return err
	}
	if err := e.PutUint64(msg.Start); err != nil {
		return err
	}
	if err := e.PutUint64(msg.Length); err != nil {
		return err
	}
	if err := e.PutUint32(msg.ProcID); err != nil {
		return err
	}
	return e.PutString(msg.ClientID)
}

// ReadMessage reads a complete 9P message from a reader.
func ReadMessage(r io.Reader, maxSize uint32) ([]byte, error) {
	// Read header
	header := make([]byte, HeaderSize)
	if _, err := io.ReadFull(r, header); err != nil {
		return nil, fmt.Errorf("reading header: %w", err)
	}

	size := binary.LittleEndian.Uint32(header[0:4])
	if size < HeaderSize {
		return nil, ErrInvalidMessage
	}
	if size > maxSize {
		return nil, ErrMessageTooLarge
	}

	// Read rest of message
	msg := make([]byte, size)
	copy(msg, header)
	if _, err := io.ReadFull(r, msg[HeaderSize:]); err != nil {
		return nil, fmt.Errorf("reading body: %w", err)
	}

	return msg, nil
}

// WriteMessage writes a complete 9P message to a writer.
func WriteMessage(w io.Writer, msgType uint8, tag uint16, body []byte) error {
	size := uint32(HeaderSize + len(body))
	header := make([]byte, HeaderSize)
	binary.LittleEndian.PutUint32(header[0:4], size)
	header[4] = msgType
	binary.LittleEndian.PutUint16(header[5:7], tag)

	if _, err := w.Write(header); err != nil {
		return err
	}
	if len(body) > 0 {
		if _, err := w.Write(body); err != nil {
			return err
		}
	}
	return nil
}

// Package p9 implements the 9P2000.L protocol used for VirtIO 9P filesystem sharing.
//
// 9P2000.L is the Linux variant of the 9P protocol, which uses file identifiers (fids)
// rather than file descriptors and supports Unix-like file operations.
package p9

// Protocol message types (T = request, R = response)
const (
	// Tversion/Rversion - protocol version negotiation
	Tversion = 100
	Rversion = 101

	// Tauth/Rauth - authentication (not commonly used)
	Tauth = 102
	Rauth = 103

	// Tattach/Rattach - attach to filesystem
	Tattach = 104
	Rattach = 105

	// Rerror - error response (9P2000 classic, uses error string)
	Rerror = 107

	// Rlerror - error response (9P2000.L, uses errno directly)
	// Reference: tinyemu-2019-12-21/virtio.c:1992-2000
	Rlerror = 7

	// Tflush/Rflush - flush pending request
	Tflush = 108
	Rflush = 109

	// Twalk/Rwalk - walk filesystem tree
	Twalk = 110
	Rwalk = 111

	// Tread/Rread - read data
	Tread = 116
	Rread = 117

	// Twrite/Rwrite - write data
	Twrite = 118
	Rwrite = 119

	// Tclunk/Rclunk - close fid
	Tclunk = 120
	Rclunk = 121

	// Tremove/Rremove - remove file (deprecated in 9P2000.L)
	Tremove = 122
	Rremove = 123

	// Tstat/Rstat - get attributes (9P2000 style)
	Tstat = 124
	Rstat = 125

	// Twstat/Rwstat - set attributes (9P2000 style)
	Twstat = 126
	Rwstat = 127

	// 9P2000.L extensions
	// Tstatfs/Rstatfs - filesystem statistics
	Tstatfs = 8
	Rstatfs = 9

	// Tlopen/Rlopen - open a file
	Tlopen = 12
	Rlopen = 13

	// Tlcreate/Rlcreate - create a file
	Tlcreate = 14
	Rlcreate = 15

	// Tsymlink/Rsymlink - create symbolic link
	Tsymlink = 16
	Rsymlink = 17

	// Tmknod/Rmknod - create device node
	Tmknod = 18
	Rmknod = 19

	// Trename/Rrename - rename file
	Trename = 20
	Rrename = 21

	// Treadlink/Rreadlink - read symbolic link
	Treadlink = 22
	Rreadlink = 23

	// Tgetattr/Rgetattr - get file attributes
	Tgetattr = 24
	Rgetattr = 25

	// Tsetattr/Rsetattr - set file attributes
	Tsetattr = 26
	Rsetattr = 27

	// Txattrwalk/Rxattrwalk - extended attributes
	Txattrwalk = 30
	Rxattrwalk = 31

	// Txattrcreate/Rxattrcreate - create extended attribute
	Txattrcreate = 32
	Rxattrcreate = 33

	// Treaddir/Rreaddir - read directory entries
	Treaddir = 40
	Rreaddir = 41

	// Tfsync/Rfsync - synchronize file
	Tfsync = 50
	Rfsync = 51

	// Tlock/Rlock - file locking
	Tlock = 52
	Rlock = 53

	// Tgetlock/Rgetlock - get lock status
	Tgetlock = 54
	Rgetlock = 55

	// Tlink/Rlink - create hard link
	Tlink = 70
	Rlink = 71

	// Tmkdir/Rmkdir - create directory
	Tmkdir = 72
	Rmkdir = 73

	// Trenameat/Rrenameat - rename file (relative)
	Trenameat = 74
	Rrenameat = 75

	// Tunlinkat/Runlinkat - remove file or directory
	Tunlinkat = 76
	Runlinkat = 77
)

// QID type bits
const (
	QtDir     = 0x80 // Directory
	QtAppend  = 0x40 // Append-only file
	QtExcl    = 0x20 // Exclusive use file
	QtMount   = 0x10 // Mounted channel
	QtAuth    = 0x08 // Authentication file
	QtTmp     = 0x04 // Temporary file
	QtSymlink = 0x02 // Symbolic link
	QtLink    = 0x01 // Hard link
	QtFile    = 0x00 // Regular file
)

// Mode bits for stat
const (
	ModeIRWXUGO = 0x01FF
	ModeISVTX   = 0x0200 // Sticky bit
	ModeISGID   = 0x0400 // Set GID
	ModeISUID   = 0x0800 // Set UID

	ModeIFMT  = 0xF000 // Type mask
	ModeIFIFO = 0x1000 // FIFO
	ModeIFCHR = 0x2000 // Character device
	ModeIFDIR = 0x4000 // Directory
	ModeIFBLK = 0x6000 // Block device
	ModeIFREG = 0x8000 // Regular file
	ModeIFLNK = 0xA000 // Symbolic link
	ModeIFSOC = 0xC000 // Socket
)

// Open flags
const (
	OpenRDONLY    = 0x00000000
	OpenWRONLY    = 0x00000001
	OpenRDWR      = 0x00000002
	OpenNOACCESS  = 0x00000003
	OpenCREAT     = 0x00000040
	OpenEXCL      = 0x00000080
	OpenNOCTTY    = 0x00000100
	OpenTRUNC     = 0x00000200
	OpenAPPEND    = 0x00000400
	OpenNONBLOCK  = 0x00000800
	OpenDSYNC     = 0x00001000
	OpenFASYNC    = 0x00002000
	OpenDIRECT    = 0x00004000
	OpenLARGEFILE = 0x00008000
	OpenDIRECTORY = 0x00010000
	OpenNOFOLLOW  = 0x00020000
	OpenNOATIME   = 0x00040000
	OpenCLOEXEC   = 0x00080000
	OpenSYNC      = 0x00100000
)

// Setattr mask bits
const (
	SetattrMode     = 0x00000001
	SetattrUID      = 0x00000002
	SetattrGID      = 0x00000004
	SetattrSize     = 0x00000008
	SetattrAtime    = 0x00000010
	SetattrMtime    = 0x00000020
	SetattrCtime    = 0x00000040
	SetattrAtimeSet = 0x00000080
	SetattrMtimeSet = 0x00000100
)

// Getattr request mask bits
const (
	GetattrMode    = 0x00000001
	GetattrNlink   = 0x00000002
	GetattrUID     = 0x00000004
	GetattrGID     = 0x00000008
	GetattrRdev    = 0x00000010
	GetattrAtime   = 0x00000020
	GetattrMtime   = 0x00000040
	GetattrCtime   = 0x00000080
	GetattrIno     = 0x00000100
	GetattrSize    = 0x00000200
	GetattrBlocks  = 0x00000400
	GetattrBtime   = 0x00000800
	GetattrGen     = 0x00001000
	GetattrDataVer = 0x00002000
	GetattrBasic   = 0x000007ff // All basic stats
	GetattrAll     = 0x00003fff // All stats
)

// Lock types
const (
	LockTypeRDLCK = 0
	LockTypeWRLCK = 1
	LockTypeUNLCK = 2
)

// Lock flags
const (
	LockFlagsBlock   = 1
	LockFlagsReclaim = 2
)

// Lock status values
const (
	LockSuccess = 0
	LockBlocked = 1
	LockError   = 2
	LockGrace   = 3
)

// Error codes (Unix errno values)
const (
	EPERM     = 1
	ENOENT    = 2
	EIO       = 5
	EEXIST    = 17
	ENOTDIR   = 20
	EINVAL    = 22
	ENOSPC    = 28
	ENOTEMPTY = 39
	EPROTO    = 71
	ENOTSUP   = 524
)

// QID represents a unique file identifier.
// The QID is comparable to an inode number in Unix.
type QID struct {
	Type    uint8  // QID type (QtDir, QtFile, etc.)
	Version uint32 // Version number for cache coherence
	Path    uint64 // Unique identifier (like inode number)
}

// Stat represents file attributes (9P2000.L style).
type Stat struct {
	QID       QID
	Mode      uint32
	UID       uint32
	GID       uint32
	NLink     uint64
	RDev      uint64
	Size      uint64
	BlkSize   uint64
	Blocks    uint64
	AtimeSec  uint64
	AtimeNsec uint32
	MtimeSec  uint64
	MtimeNsec uint32
	CtimeSec  uint64
	CtimeNsec uint32
}

// StatFS represents filesystem statistics.
type StatFS struct {
	Type    uint32 // Filesystem type
	BSize   uint32 // Block size
	Blocks  uint64 // Total blocks
	BFree   uint64 // Free blocks
	BAvail  uint64 // Available blocks (non-root)
	Files   uint64 // Total file nodes
	FFree   uint64 // Free file nodes
	FSid    uint64 // Filesystem ID
	NameLen uint32 // Max filename length
}

// Dirent represents a directory entry.
type Dirent struct {
	QID    QID
	Offset uint64 // Offset for next readdir
	Type   uint8  // Entry type
	Name   string
}

// Lock represents a file lock.
type Lock struct {
	Type     uint8
	Flags    uint32
	Start    uint64
	Length   uint64
	ProcID   uint32
	ClientID string
}

// MessageTypeName returns the name of a message type.
func MessageTypeName(typ uint8) string {
	switch typ {
	case Tversion:
		return "Tversion"
	case Rversion:
		return "Rversion"
	case Tauth:
		return "Tauth"
	case Rauth:
		return "Rauth"
	case Tattach:
		return "Tattach"
	case Rattach:
		return "Rattach"
	case Rerror:
		return "Rerror"
	case Rlerror:
		return "Rlerror"
	case Tflush:
		return "Tflush"
	case Rflush:
		return "Rflush"
	case Twalk:
		return "Twalk"
	case Rwalk:
		return "Rwalk"
	case Tread:
		return "Tread"
	case Rread:
		return "Rread"
	case Twrite:
		return "Twrite"
	case Rwrite:
		return "Rwrite"
	case Tclunk:
		return "Tclunk"
	case Rclunk:
		return "Rclunk"
	case Tstatfs:
		return "Tstatfs"
	case Rstatfs:
		return "Rstatfs"
	case Tlopen:
		return "Tlopen"
	case Rlopen:
		return "Rlopen"
	case Tlcreate:
		return "Tlcreate"
	case Rlcreate:
		return "Rlcreate"
	case Tsymlink:
		return "Tsymlink"
	case Rsymlink:
		return "Rsymlink"
	case Tmknod:
		return "Tmknod"
	case Rmknod:
		return "Rmknod"
	case Treadlink:
		return "Treadlink"
	case Rreadlink:
		return "Rreadlink"
	case Tgetattr:
		return "Tgetattr"
	case Rgetattr:
		return "Rgetattr"
	case Tsetattr:
		return "Tsetattr"
	case Rsetattr:
		return "Rsetattr"
	case Txattrwalk:
		return "Txattrwalk"
	case Rxattrwalk:
		return "Rxattrwalk"
	case Treaddir:
		return "Treaddir"
	case Rreaddir:
		return "Rreaddir"
	case Tfsync:
		return "Tfsync"
	case Rfsync:
		return "Rfsync"
	case Tlock:
		return "Tlock"
	case Rlock:
		return "Rlock"
	case Tgetlock:
		return "Tgetlock"
	case Rgetlock:
		return "Rgetlock"
	case Tlink:
		return "Tlink"
	case Rlink:
		return "Rlink"
	case Tmkdir:
		return "Tmkdir"
	case Rmkdir:
		return "Rmkdir"
	case Trenameat:
		return "Trenameat"
	case Rrenameat:
		return "Rrenameat"
	case Tunlinkat:
		return "Tunlinkat"
	case Runlinkat:
		return "Runlinkat"
	default:
		return "unknown"
	}
}

//go:build linux || darwin

package p9

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
	"syscall"

	"golang.org/x/sys/unix"
)

// HostFS implements FSDevice by exposing a host directory.
type HostFS struct {
	mu       sync.RWMutex
	rootPath string
}

// NewHostFS creates a new host filesystem rooted at the given path.
// Reference: tinyemu-2019-12-21/fs_disk.c:623-659
func NewHostFS(rootPath string) (*HostFS, error) {
	// Use Lstat (not Stat) to match C behavior - don't follow symlinks
	// This prevents creating a HostFS rooted at a symlink
	info, err := os.Lstat(rootPath)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, errors.New("root path is not a directory")
	}

	absPath, err := filepath.Abs(rootPath)
	if err != nil {
		return nil, err
	}

	return &HostFS{
		rootPath: absPath,
	}, nil
}

// hostFile implements the File interface.
type hostFile struct {
	path     string // Full path on host
	uid      uint32
	isOpened bool
	isDir    bool
	fd       *os.File
	dir      *os.File
}

func (f *hostFile) Path() string {
	return f.path
}

// Statfs returns filesystem statistics.
func (h *HostFS) Statfs() (StatFS, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(h.rootPath, &stat); err != nil {
		return StatFS{}, err
	}

	return StatFS{
		Type:    uint32(stat.Type),
		BSize:   uint32(stat.Bsize),
		Blocks:  stat.Blocks,
		BFree:   stat.Bfree,
		BAvail:  stat.Bavail,
		Files:   stat.Files,
		FFree:   stat.Ffree,
		FSid:    0, // Not easily available
		NameLen: statfsNameLen(&stat),
	}, nil
}

// Attach attaches to the filesystem.
func (h *HostFS) Attach(uid uint32, uname, aname string) (File, QID, error) {
	info, err := os.Lstat(h.rootPath)
	if err != nil {
		return nil, QID{}, errnoToP9(err)
	}

	f := &hostFile{
		path: h.rootPath,
		uid:  uid,
	}

	return f, statToQID(info), nil
}

// Walk walks the filesystem tree.
func (h *HostFS) Walk(f File, names []string) (File, []QID, error) {
	hf := f.(*hostFile)

	path := hf.path
	qids := make([]QID, 0, len(names))

	for _, name := range names {
		path = filepath.Join(path, name)

		// Security check: ensure we don't escape the root
		if !h.isPathSafe(path) {
			return nil, qids, errors.New("path escapes root")
		}

		info, err := os.Lstat(path)
		if err != nil {
			return nil, qids, errnoToP9(err)
		}

		qids = append(qids, statToQID(info))
	}

	newFile := &hostFile{
		path: path,
		uid:  hf.uid,
	}

	return newFile, qids, nil
}

// isPathSafe checks that path doesn't escape the root.
func (h *HostFS) isPathSafe(path string) bool {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return false
	}

	// Check that the path is within the root
	rel, err := filepath.Rel(h.rootPath, absPath)
	if err != nil {
		return false
	}

	// If the relative path starts with "..", it escapes the root
	if len(rel) >= 2 && rel[0] == '.' && rel[1] == '.' {
		return false
	}

	return true
}

// Open opens a file.
func (h *HostFS) Open(f File, flags uint32) (QID, uint32, error) {
	hf := f.(*hostFile)

	info, err := os.Lstat(hf.path)
	if err != nil {
		return QID{}, 0, errnoToP9(err)
	}

	if info.IsDir() {
		dir, err := os.Open(hf.path)
		if err != nil {
			return QID{}, 0, errnoToP9(err)
		}
		hf.dir = dir
		hf.isDir = true
		hf.isOpened = true
	} else {
		osFlags := p9FlagsToHost(flags)
		fd, err := os.OpenFile(hf.path, osFlags, 0)
		if err != nil {
			return QID{}, 0, errnoToP9(err)
		}
		hf.fd = fd
		hf.isDir = false
		hf.isOpened = true
	}

	return statToQID(info), 0, nil // IOUnit 0 means no limit
}

// Create creates a new file.
func (h *HostFS) Create(f File, name string, flags, mode, gid uint32) (QID, uint32, error) {
	hf := f.(*hostFile)

	path := filepath.Join(hf.path, name)
	if !h.isPathSafe(path) {
		return QID{}, 0, errors.New("path escapes root")
	}

	osFlags := p9FlagsToHost(flags) | os.O_CREATE
	fd, err := os.OpenFile(path, osFlags, os.FileMode(mode&0777))
	if err != nil {
		return QID{}, 0, errnoToP9(err)
	}

	info, err := fd.Stat()
	if err != nil {
		fd.Close()
		return QID{}, 0, errnoToP9(err)
	}

	// Update file to point to new file
	hf.path = path
	hf.fd = fd
	hf.isOpened = true
	hf.isDir = false

	return statToQID(info), 0, nil
}

// Read reads from a file.
// Reference: tinyemu-2019-12-21/fs_disk.c:349-361
func (h *HostFS) Read(f File, offset uint64, count uint32) ([]byte, error) {
	hf := f.(*hostFile)

	// Match C behavior: return -P9_EPROTO if not opened or is directory
	if !hf.isOpened || hf.isDir {
		return nil, &p9Error{EPROTO, "file not opened or is directory"}
	}

	buf := make([]byte, count)
	n, err := hf.fd.ReadAt(buf, int64(offset))
	if err != nil && err != io.EOF {
		return nil, errnoToP9(err)
	}

	return buf[:n], nil
}

// Write writes to a file.
// Reference: tinyemu-2019-12-21/fs_disk.c:363-375
func (h *HostFS) Write(f File, offset uint64, data []byte) (uint32, error) {
	hf := f.(*hostFile)

	// Match C behavior: return -P9_EPROTO if not opened or is directory
	if !hf.isOpened || hf.isDir {
		return 0, &p9Error{EPROTO, "file not opened or is directory"}
	}

	n, err := hf.fd.WriteAt(data, int64(offset))
	if err != nil {
		return uint32(n), errnoToP9(err)
	}

	return uint32(n), nil
}

// Clunk closes a file handle.
// Reference: tinyemu-2019-12-21/fs_disk.c:377-386 (fs_close)
func (h *HostFS) Clunk(f File) error {
	hf := f.(*hostFile)

	if hf.isOpened {
		if hf.isDir {
			if hf.dir != nil {
				hf.dir.Close()
				hf.dir = nil
			}
		} else {
			if hf.fd != nil {
				hf.fd.Close()
				hf.fd = nil
			}
		}
		hf.isOpened = false
	}

	return nil
}

// Remove removes a file.
func (h *HostFS) Remove(f File) error {
	hf := f.(*hostFile)

	h.Clunk(f)

	err := os.Remove(hf.path)
	if err != nil {
		return errnoToP9(err)
	}

	return nil
}

// Getattr returns file attributes.
// Reference: tinyemu-2019-12-21/fs_disk.c:388-410 (fs_stat)
func (h *HostFS) Getattr(f File, mask uint64) (Stat, uint64, error) {
	hf := f.(*hostFile)

	info, err := os.Lstat(hf.path)
	if err != nil {
		return Stat{}, 0, errnoToP9(err)
	}

	stat := infoToStat(info)
	return stat, GetattrBasic, nil
}

// Setattr sets file attributes.
// Reference: tinyemu-2019-12-21/fs_disk.c:412-471
func (h *HostFS) Setattr(f File, valid uint32, mode, uid, gid uint32, size uint64,
	atimeSec, atimeNsec, mtimeSec, mtimeNsec uint64) error {

	hf := f.(*hostFile)
	ctimeUpdated := false

	// Handle UID/GID changes first (before mode for suid)
	if valid&(SetattrUID|SetattrGID) != 0 {
		newUID := -1
		newGID := -1
		if valid&SetattrUID != 0 {
			newUID = int(uid)
		}
		if valid&SetattrGID != 0 {
			newGID = int(gid)
		}
		if err := os.Lchown(hf.path, newUID, newGID); err != nil {
			return errnoToP9(err)
		}
		ctimeUpdated = true
	}

	// Handle mode (must be done after uid change for suid)
	if valid&SetattrMode != 0 {
		if err := os.Chmod(hf.path, os.FileMode(mode)); err != nil {
			return errnoToP9(err)
		}
		ctimeUpdated = true
	}

	// Handle size
	if valid&SetattrSize != 0 {
		if err := os.Truncate(hf.path, int64(size)); err != nil {
			return errnoToP9(err)
		}
		ctimeUpdated = true
	}

	// Handle atime/mtime
	if valid&(SetattrAtime|SetattrMtime) != 0 {
		var ts [2]unix.Timespec

		if valid&SetattrAtime != 0 {
			if valid&SetattrAtimeSet != 0 {
				ts[0].Sec = int64(atimeSec)
				ts[0].Nsec = int64(atimeNsec)
			} else {
				ts[0].Sec = 0
				ts[0].Nsec = utimeNow
			}
		} else {
			ts[0].Sec = 0
			ts[0].Nsec = utimeOmit
		}

		if valid&SetattrMtime != 0 {
			if valid&SetattrMtimeSet != 0 {
				ts[1].Sec = int64(mtimeSec)
				ts[1].Nsec = int64(mtimeNsec)
			} else {
				ts[1].Sec = 0
				ts[1].Nsec = utimeNow
			}
		} else {
			ts[1].Sec = 0
			ts[1].Nsec = utimeOmit
		}

		if err := utimesNanoAt(hf.path, ts[:]); err != nil {
			return errnoToP9(err)
		}
		ctimeUpdated = true
	}

	// Handle ctime update without other changes
	if valid&SetattrCtime != 0 && !ctimeUpdated {
		// Empty lchown updates ctime
		if err := os.Lchown(hf.path, -1, -1); err != nil {
			return errnoToP9(err)
		}
	}

	return nil
}

// Readdir reads directory entries.
// Reference: tinyemu-2019-12-21/fs_disk.c:293-347
func (h *HostFS) Readdir(f File, offset uint64, count uint32) ([]byte, error) {
	hf := f.(*hostFile)

	// Match C behavior: return -P9_EPROTO if not opened or not a directory
	if !hf.isOpened || !hf.isDir {
		return nil, &p9Error{EPROTO, "not an open directory"}
	}

	// Seek to beginning and skip to offset
	if _, err := hf.dir.Seek(0, 0); err != nil {
		return nil, errnoToP9(err)
	}

	entries, err := hf.dir.ReadDir(-1)
	if err != nil {
		return nil, errnoToP9(err)
	}

	// Build response
	buf := make([]byte, count)
	enc := NewEncoder(buf)
	currentOffset := uint64(0)

	for _, entry := range entries {
		entryOffset := currentOffset + 1
		currentOffset = entryOffset

		// Skip entries before the requested offset
		if entryOffset <= offset {
			continue
		}

		info, err := entry.Info()
		if err != nil {
			continue
		}

		dirent := Dirent{
			QID:    statToQID(info),
			Offset: entryOffset,
			Type:   qidTypeFromMode(info.Mode()),
			Name:   entry.Name(),
		}

		// Check if entry fits
		size := DirentSize(entry.Name())
		if enc.Len()+size > int(count) {
			break
		}

		if err := MarshalDirent(enc, dirent); err != nil {
			break
		}
	}

	return enc.Bytes(), nil
}

// Mkdir creates a directory.
func (h *HostFS) Mkdir(f File, name string, mode, gid uint32) (QID, error) {
	hf := f.(*hostFile)

	path := filepath.Join(hf.path, name)
	if !h.isPathSafe(path) {
		return QID{}, errors.New("path escapes root")
	}

	if err := os.Mkdir(path, os.FileMode(mode&0777)); err != nil {
		return QID{}, errnoToP9(err)
	}

	info, err := os.Lstat(path)
	if err != nil {
		return QID{}, errnoToP9(err)
	}

	return statToQID(info), nil
}

// Symlink creates a symbolic link.
// Reference: tinyemu-2019-12-21/fs_disk.c:486-504
func (h *HostFS) Symlink(f File, name, target string, gid uint32) (QID, error) {
	hf := f.(*hostFile)

	path := filepath.Join(hf.path, name)
	if !h.isPathSafe(path) {
		return QID{}, errors.New("path escapes root")
	}

	if err := os.Symlink(target, path); err != nil {
		return QID{}, errnoToP9(err)
	}

	info, err := os.Lstat(path)
	if err != nil {
		return QID{}, errnoToP9(err)
	}

	return statToQID(info), nil
}

// Mknod creates a device node.
// Reference: tinyemu-2019-12-21/fs_disk.c:506-525
func (h *HostFS) Mknod(f File, name string, mode, major, minor, gid uint32) (QID, error) {
	hf := f.(*hostFile)

	path := filepath.Join(hf.path, name)
	if !h.isPathSafe(path) {
		return QID{}, errors.New("path escapes root")
	}

	// Creating device nodes requires root privileges.
	// Use unix.Mkdev to match C makedev(major, minor) behavior.
	dev := unix.Mkdev(major, minor)
	if err := unix.Mknod(path, mode, int(dev)); err != nil {
		return QID{}, errnoToP9(err)
	}

	info, err := os.Lstat(path)
	if err != nil {
		return QID{}, errnoToP9(err)
	}

	return statToQID(info), nil
}

// Readlink reads a symbolic link.
// Reference: tinyemu-2019-12-21/fs_disk.c:527-535
func (h *HostFS) Readlink(f File) (string, error) {
	hf := f.(*hostFile)

	target, err := os.Readlink(hf.path)
	if err != nil {
		return "", errnoToP9(err)
	}

	return target, nil
}

// Link creates a hard link.
// Reference: tinyemu-2019-12-21/fs_disk.c:473-484
func (h *HostFS) Link(dfid File, f File, name string) error {
	hfDir := dfid.(*hostFile)
	hfSrc := f.(*hostFile)

	path := filepath.Join(hfDir.path, name)
	if !h.isPathSafe(path) {
		return errors.New("path escapes root")
	}

	if err := os.Link(hfSrc.path, path); err != nil {
		return errnoToP9(err)
	}

	return nil
}

// Renameat renames a file.
// Reference: tinyemu-2019-12-21/fs_disk.c:537-551
func (h *HostFS) Renameat(oldDirF File, oldName string, newDirF File, newName string) error {
	hfOldDir := oldDirF.(*hostFile)
	hfNewDir := newDirF.(*hostFile)

	oldPath := filepath.Join(hfOldDir.path, oldName)
	newPath := filepath.Join(hfNewDir.path, newName)

	if !h.isPathSafe(oldPath) || !h.isPathSafe(newPath) {
		return errors.New("path escapes root")
	}

	if err := os.Rename(oldPath, newPath); err != nil {
		return errnoToP9(err)
	}

	return nil
}

// Unlinkat removes a file or directory.
// Reference: tinyemu-2019-12-21/fs_disk.c:553-565
func (h *HostFS) Unlinkat(f File, name string, flags uint32) error {
	hf := f.(*hostFile)

	path := filepath.Join(hf.path, name)
	if !h.isPathSafe(path) {
		return errors.New("path escapes root")
	}

	var err error
	if flags&0x200 != 0 { // AT_REMOVEDIR
		err = os.Remove(path)
	} else {
		err = os.Remove(path)
	}

	if err != nil {
		return errnoToP9(err)
	}

	return nil
}

// Fsync synchronizes file data.
func (h *HostFS) Fsync(f File) error {
	hf := f.(*hostFile)

	if !hf.isOpened {
		return nil
	}

	if hf.isDir {
		if hf.dir != nil {
			return hf.dir.Sync()
		}
	} else {
		if hf.fd != nil {
			return hf.fd.Sync()
		}
	}

	return nil
}

// Lock applies a file lock.
// Reference: tinyemu-2019-12-21/fs_disk.c:567-590
func (h *HostFS) Lock(f File, lock *Lock) (uint8, error) {
	hf := f.(*hostFile)

	// Match C behavior: reject if not opened or is directory
	if !hf.isOpened || hf.isDir {
		return 0, &p9Error{EPROTO, "file not opened or is directory"}
	}

	fl := syscall.Flock_t{
		Type:   int16(lock.Type),
		Whence: 0, // SEEK_SET
		Start:  int64(lock.Start),
		Len:    int64(lock.Length),
	}

	err := syscall.FcntlFlock(hf.fd.Fd(), syscall.F_SETLK, &fl)
	if err == nil {
		return LockSuccess, nil
	}

	// Check for EAGAIN or EACCES - indicates lock is blocked
	if err == syscall.EAGAIN || err == syscall.EACCES {
		return LockBlocked, nil
	}

	return LockError, errnoToP9(err)
}

// Getlock gets lock information.
// Reference: tinyemu-2019-12-21/fs_disk.c:592-615
func (h *HostFS) Getlock(f File, lock *Lock) (*Lock, error) {
	hf := f.(*hostFile)

	// Match C behavior: reject if not opened or is directory
	if !hf.isOpened || hf.isDir {
		return nil, &p9Error{EPROTO, "file not opened or is directory"}
	}

	fl := syscall.Flock_t{
		Type:   int16(lock.Type),
		Whence: 0, // SEEK_SET
		Start:  int64(lock.Start),
		Len:    int64(lock.Length),
	}

	err := syscall.FcntlFlock(hf.fd.Fd(), syscall.F_GETLK, &fl)
	if err != nil {
		return nil, errnoToP9(err)
	}

	// Return the lock info from the kernel
	return &Lock{
		Type:     uint8(fl.Type),
		Start:    uint64(fl.Start),
		Length:   uint64(fl.Len),
		ProcID:   uint32(fl.Pid),
		ClientID: lock.ClientID, // Preserve client ID from input
	}, nil
}

// Helper functions

// statToQID converts os.FileInfo to a 9P QID.
// Reference: tinyemu-2019-12-21/fs_disk.c:133-143 (stat_to_qid)
func statToQID(info os.FileInfo) QID {
	var typ uint8

	mode := info.Mode()
	if mode.IsDir() {
		typ = QtDir
	} else if mode&os.ModeSymlink != 0 {
		typ = QtSymlink
	} else {
		typ = QtFile
	}

	// Get inode number from Sys() if available
	var path uint64
	if sys, ok := info.Sys().(*syscall.Stat_t); ok {
		path = sys.Ino
	}

	return QID{
		Type:    typ,
		Version: 0, // No caching
		Path:    path,
	}
}

func infoToStat(info os.FileInfo) Stat {
	st := Stat{
		QID:     statToQID(info),
		Mode:    uint32(info.Mode()),
		Size:    uint64(info.Size()),
		BlkSize: 4096, // Default block size
	}

	// Get additional info from Sys() if available
	// Reference: tinyemu-2019-12-21/fs_disk.c:388-410
	if sys, ok := info.Sys().(*syscall.Stat_t); ok {
		// Use Unix mode from syscall.Stat_t, not os.FileMode which has different bit layout
		st.Mode = uint32(sys.Mode)
		st.UID = sys.Uid
		st.GID = sys.Gid
		st.NLink = uint64(sys.Nlink)
		st.RDev = uint64(sys.Rdev)
		st.Blocks = uint64(sys.Blocks)
		st.AtimeSec, st.AtimeNsec = statATime(sys)
		st.MtimeSec, st.MtimeNsec = statMTime(sys)
		st.CtimeSec, st.CtimeNsec = statCTime(sys)
	} else {
		// Fallback using ModTime
		mtime := info.ModTime()
		st.MtimeSec = uint64(mtime.Unix())
		st.MtimeNsec = uint32(mtime.Nanosecond())
		st.AtimeSec = st.MtimeSec
		st.AtimeNsec = st.MtimeNsec
		st.CtimeSec = st.MtimeSec
		st.CtimeNsec = st.MtimeNsec
	}

	return st
}

func qidTypeFromMode(mode os.FileMode) uint8 {
	if mode.IsDir() {
		return QtDir
	} else if mode&os.ModeSymlink != 0 {
		return QtSymlink
	}
	return QtFile
}

// p9FlagsToHost converts 9P open flags to host OS flags.
// Reference: tinyemu-2019-12-21/fs_disk.c:121-131 (p9_flags_to_host)
func p9FlagsToHost(flags uint32) int {
	osFlags := int(flags & OpenNOACCESS) // O_RDONLY, O_WRONLY, O_RDWR

	if flags&OpenCREAT != 0 {
		osFlags |= os.O_CREATE
	}
	if flags&OpenEXCL != 0 {
		osFlags |= os.O_EXCL
	}
	if flags&OpenTRUNC != 0 {
		osFlags |= os.O_TRUNC
	}
	if flags&OpenAPPEND != 0 {
		osFlags |= os.O_APPEND
	}
	if flags&OpenNONBLOCK != 0 {
		osFlags |= syscall.O_NONBLOCK
	}
	if flags&OpenDSYNC != 0 {
		osFlags |= syscall.O_DSYNC
	}
	if flags&OpenNOFOLLOW != 0 {
		osFlags |= syscall.O_NOFOLLOW
	}
	if flags&OpenSYNC != 0 {
		osFlags |= os.O_SYNC
	}

	return osFlags
}

// errnoToP9 converts an OS error to a 9P error.
// Reference: tinyemu-2019-12-21/fs_disk.c:91-101 (errno_to_p9)
func errnoToP9(err error) error {
	if err == nil {
		return nil
	}

	// Check for os.PathError or os.LinkError
	var pathErr *os.PathError
	var linkErr *os.LinkError
	var sysErr syscall.Errno

	if errors.As(err, &pathErr) {
		err = pathErr.Err
	} else if errors.As(err, &linkErr) {
		err = linkErr.Err
	}

	if errors.As(err, &sysErr) {
		switch sysErr {
		case syscall.EPERM:
			return &p9Error{EPERM, "permission denied"}
		case syscall.ENOENT:
			return &p9Error{ENOENT, "no such file or directory"}
		case syscall.EIO:
			return &p9Error{EIO, "I/O error"}
		case syscall.EEXIST:
			return &p9Error{EEXIST, "file exists"}
		case syscall.ENOTDIR:
			return &p9Error{ENOTDIR, "not a directory"}
		case syscall.EINVAL:
			return &p9Error{EINVAL, "invalid argument"}
		case syscall.ENOSPC:
			return &p9Error{ENOSPC, "no space left on device"}
		case syscall.ENOTEMPTY:
			return &p9Error{ENOTEMPTY, "directory not empty"}
		case syscall.ENOTSUP:
			// Reference: tinyemu-2019-12-21/fs_disk.c:88
			return &p9Error{ENOTSUP, "operation not supported"}
		case syscall.EPROTO:
			// Reference: tinyemu-2019-12-21/fs_disk.c:87
			return &p9Error{EPROTO, "protocol error"}
		}
	}

	return &p9Error{EIO, err.Error()}
}

// p9Error represents a 9P error code.
type p9Error struct {
	Code uint32
	Msg  string
}

func (e *p9Error) Error() string {
	return e.Msg
}

// ErrorCode returns the 9P error code from an error, or EIO if unknown.
func ErrorCode(err error) uint32 {
	if err == nil {
		return 0
	}
	if pe, ok := err.(*p9Error); ok {
		return pe.Code
	}
	return EIO
}

// Verify HostFS implements FSDevice
var _ FSDevice = (*HostFS)(nil)

// Verify hostFile implements File
var _ File = (*hostFile)(nil)

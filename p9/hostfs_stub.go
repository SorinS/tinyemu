//go:build !linux && !darwin

package p9

import "errors"

// errNotSupported is returned by all HostFS methods on non-Linux platforms.
var errNotSupported = errors.New("HostFS is not supported on this platform")

// HostFS is not supported on non-Linux platforms.
// This stub exists to allow cross-compilation.
// Reference: tinyemu-2019-12-21/fs_disk.c relies on Unix-specific syscalls
type HostFS struct{}

// hostFile stub for non-Linux platforms.
type hostFile struct{}

func (f *hostFile) Path() string { return "" }

// NewHostFS returns an error on non-Linux platforms as HostFS relies on
// Unix-specific syscalls (statfs, mknod, utimensat, etc.).
func NewHostFS(rootPath string) (*HostFS, error) {
	return nil, errNotSupported
}

// ErrorCode returns the 9P error code from an error, or EIO if unknown.
func ErrorCode(err error) uint32 {
	if err == nil {
		return 0
	}
	return EIO
}

// FSDevice interface implementation stubs.
// These methods will never be called since NewHostFS always returns an error,
// but they're required for the type to implement FSDevice.

func (h *HostFS) Statfs() (StatFS, error) {
	return StatFS{}, errNotSupported
}

func (h *HostFS) Attach(uid uint32, uname, aname string) (File, QID, error) {
	return nil, QID{}, errNotSupported
}

func (h *HostFS) Walk(f File, names []string) (File, []QID, error) {
	return nil, nil, errNotSupported
}

func (h *HostFS) Open(f File, flags uint32) (QID, uint32, error) {
	return QID{}, 0, errNotSupported
}

func (h *HostFS) Create(f File, name string, flags, mode, gid uint32) (QID, uint32, error) {
	return QID{}, 0, errNotSupported
}

func (h *HostFS) Read(f File, offset uint64, count uint32) ([]byte, error) {
	return nil, errNotSupported
}

func (h *HostFS) Write(f File, offset uint64, data []byte) (uint32, error) {
	return 0, errNotSupported
}

func (h *HostFS) Clunk(f File) error {
	return errNotSupported
}

func (h *HostFS) Remove(f File) error {
	return errNotSupported
}

func (h *HostFS) Getattr(f File, mask uint64) (Stat, uint64, error) {
	return Stat{}, 0, errNotSupported
}

func (h *HostFS) Setattr(f File, valid uint32, mode, uid, gid uint32, size uint64,
	atimeSec, atimeNsec, mtimeSec, mtimeNsec uint64) error {
	return errNotSupported
}

func (h *HostFS) Readdir(f File, offset uint64, count uint32) ([]byte, error) {
	return nil, errNotSupported
}

func (h *HostFS) Mkdir(f File, name string, mode, gid uint32) (QID, error) {
	return QID{}, errNotSupported
}

func (h *HostFS) Symlink(f File, name, target string, gid uint32) (QID, error) {
	return QID{}, errNotSupported
}

func (h *HostFS) Mknod(f File, name string, mode, major, minor, gid uint32) (QID, error) {
	return QID{}, errNotSupported
}

func (h *HostFS) Readlink(f File) (string, error) {
	return "", errNotSupported
}

func (h *HostFS) Link(dfid File, f File, name string) error {
	return errNotSupported
}

func (h *HostFS) Renameat(oldDirF File, oldName string, newDirF File, newName string) error {
	return errNotSupported
}

func (h *HostFS) Unlinkat(f File, name string, flags uint32) error {
	return errNotSupported
}

func (h *HostFS) Fsync(f File) error {
	return errNotSupported
}

func (h *HostFS) Lock(f File, lock *Lock) (uint8, error) {
	return 0, errNotSupported
}

func (h *HostFS) Getlock(f File, lock *Lock) (*Lock, error) {
	return nil, errNotSupported
}

// Verify HostFS implements FSDevice
var _ FSDevice = (*HostFS)(nil)

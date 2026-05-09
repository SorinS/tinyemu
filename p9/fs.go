package p9

// FSDevice is the interface for 9P filesystem providers.
// It abstracts filesystem operations for use with the 9P protocol.
type FSDevice interface {
	// Statfs returns filesystem statistics.
	Statfs() (StatFS, error)

	// Attach attaches to the filesystem and returns a root file handle.
	Attach(uid uint32, uname, aname string) (File, QID, error)

	// Walk walks the filesystem tree from f using the given path components.
	// Returns the new file handle and QIDs for each path component walked.
	Walk(f File, names []string) (File, []QID, error)

	// Open opens a file with the given flags.
	Open(f File, flags uint32) (QID, uint32, error) // Returns QID, IOUnit, error

	// Create creates a new file.
	Create(f File, name string, flags, mode, gid uint32) (QID, uint32, error)

	// Read reads data from a file.
	Read(f File, offset uint64, count uint32) ([]byte, error)

	// Write writes data to a file.
	Write(f File, offset uint64, data []byte) (uint32, error)

	// Clunk closes a file handle (releases fid).
	Clunk(f File) error

	// Remove removes a file (and clunks the fid).
	Remove(f File) error

	// Getattr returns file attributes.
	Getattr(f File, mask uint64) (Stat, uint64, error) // Returns stat, valid_mask, error

	// Setattr sets file attributes.
	Setattr(f File, valid uint32, mode, uid, gid uint32, size uint64,
		atimeSec, atimeNsec, mtimeSec, mtimeNsec uint64) error

	// Readdir reads directory entries.
	// Returns directory entries starting at offset, up to count bytes.
	Readdir(f File, offset uint64, count uint32) ([]byte, error)

	// Mkdir creates a directory.
	Mkdir(f File, name string, mode, gid uint32) (QID, error)

	// Symlink creates a symbolic link.
	Symlink(f File, name, target string, gid uint32) (QID, error)

	// Mknod creates a device node.
	Mknod(f File, name string, mode, major, minor, gid uint32) (QID, error)

	// Readlink reads a symbolic link.
	Readlink(f File) (string, error)

	// Link creates a hard link.
	Link(dfid File, f File, name string) error

	// Renameat renames a file.
	Renameat(oldDirF File, oldName string, newDirF File, newName string) error

	// Unlinkat removes a file or directory.
	Unlinkat(f File, name string, flags uint32) error

	// Fsync synchronizes file data.
	Fsync(f File) error

	// Lock applies a file lock.
	Lock(f File, lock *Lock) (uint8, error)

	// Getlock gets lock information.
	Getlock(f File, lock *Lock) (*Lock, error)
}

// File represents an open file handle (fid) in the 9P protocol.
// This is an opaque handle that the FSDevice implementation uses
// to track open files.
type File interface {
	// Path returns the file path (for debugging).
	Path() string
}

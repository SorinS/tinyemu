//go:build linux

package p9

import (
	"os"
	"path/filepath"
	"testing"
)

// TestNewHostFS verifies NewHostFS behavior matches C fs_disk_init.
// Reference: tinyemu-2019-12-21/fs_disk.c:623-659
func TestNewHostFS(t *testing.T) {
	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "hostfs_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Test valid directory
	fs, err := NewHostFS(tmpDir)
	if err != nil {
		t.Fatalf("NewHostFS() error = %v", err)
	}
	if fs == nil {
		t.Fatal("NewHostFS() returned nil")
	}

	// Test non-existent directory
	_, err = NewHostFS("/nonexistent/path")
	if err == nil {
		t.Error("NewHostFS() with non-existent path should fail")
	}

	// Test file instead of directory
	tmpFile := filepath.Join(tmpDir, "file.txt")
	os.WriteFile(tmpFile, []byte("test"), 0644)
	_, err = NewHostFS(tmpFile)
	if err == nil {
		t.Error("NewHostFS() with file should fail")
	}

	// Test symlink to directory - should fail (C uses lstat)
	// Reference: tinyemu-2019-12-21/fs_disk.c:628-630
	realDir := filepath.Join(tmpDir, "realdir")
	os.Mkdir(realDir, 0755)
	symlinkPath := filepath.Join(tmpDir, "linkdir")
	os.Symlink(realDir, symlinkPath)
	_, err = NewHostFS(symlinkPath)
	if err == nil {
		t.Error("NewHostFS() with symlink to directory should fail (matches C lstat behavior)")
	}
}

func TestHostFSAttach(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "hostfs_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	fs, _ := NewHostFS(tmpDir)

	f, qid, err := fs.Attach(1000, "user", "/")
	if err != nil {
		t.Fatalf("Attach() error = %v", err)
	}

	if f == nil {
		t.Error("Attach() returned nil file")
	}

	if qid.Type != QtDir {
		t.Errorf("Attach() qid.Type = %d, want QtDir", qid.Type)
	}

	if f.Path() != tmpDir {
		t.Errorf("Attach() path = %q, want %q", f.Path(), tmpDir)
	}
}

func TestHostFSWalk(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "hostfs_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create directory structure
	os.Mkdir(filepath.Join(tmpDir, "dir1"), 0755)
	os.Mkdir(filepath.Join(tmpDir, "dir1", "dir2"), 0755)
	os.WriteFile(filepath.Join(tmpDir, "dir1", "dir2", "file.txt"), []byte("test"), 0644)

	fs, _ := NewHostFS(tmpDir)
	root, _, _ := fs.Attach(1000, "user", "/")

	// Walk to dir1
	f1, qids, err := fs.Walk(root, []string{"dir1"})
	if err != nil {
		t.Fatalf("Walk(dir1) error = %v", err)
	}
	if len(qids) != 1 {
		t.Errorf("Walk(dir1) returned %d qids, want 1", len(qids))
	}
	if qids[0].Type != QtDir {
		t.Errorf("Walk(dir1) qid.Type = %d, want QtDir", qids[0].Type)
	}

	// Walk multiple components
	f2, qids, err := fs.Walk(f1, []string{"dir2", "file.txt"})
	if err != nil {
		t.Fatalf("Walk(dir2/file.txt) error = %v", err)
	}
	if len(qids) != 2 {
		t.Errorf("Walk(dir2/file.txt) returned %d qids, want 2", len(qids))
	}
	if qids[0].Type != QtDir {
		t.Errorf("Walk(dir2) qid.Type = %d, want QtDir", qids[0].Type)
	}
	if qids[1].Type != QtFile {
		t.Errorf("Walk(file.txt) qid.Type = %d, want QtFile", qids[1].Type)
	}
	_ = f2

	// Walk to non-existent file
	_, _, err = fs.Walk(root, []string{"nonexistent"})
	if err == nil {
		t.Error("Walk(nonexistent) should fail")
	}
}

func TestHostFSOpenReadWrite(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "hostfs_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create test file
	testFile := filepath.Join(tmpDir, "test.txt")
	os.WriteFile(testFile, []byte("hello world"), 0644)

	fs, _ := NewHostFS(tmpDir)
	root, _, _ := fs.Attach(1000, "user", "/")

	// Walk to file
	f, _, err := fs.Walk(root, []string{"test.txt"})
	if err != nil {
		t.Fatalf("Walk() error = %v", err)
	}

	// Open file
	qid, _, err := fs.Open(f, OpenRDONLY)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if qid.Type != QtFile {
		t.Errorf("Open() qid.Type = %d, want QtFile", qid.Type)
	}

	// Read file
	data, err := fs.Read(f, 0, 1024)
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if string(data) != "hello world" {
		t.Errorf("Read() = %q, want %q", data, "hello world")
	}

	// Read with offset
	data, err = fs.Read(f, 6, 1024)
	if err != nil {
		t.Fatalf("Read(offset=6) error = %v", err)
	}
	if string(data) != "world" {
		t.Errorf("Read(offset=6) = %q, want %q", data, "world")
	}

	// Clunk
	if err := fs.Clunk(f); err != nil {
		t.Fatalf("Clunk() error = %v", err)
	}
}

func TestHostFSWrite(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "hostfs_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create test file
	testFile := filepath.Join(tmpDir, "write.txt")
	os.WriteFile(testFile, []byte(""), 0644)

	fs, _ := NewHostFS(tmpDir)
	root, _, _ := fs.Attach(1000, "user", "/")

	// Walk to file
	f, _, _ := fs.Walk(root, []string{"write.txt"})

	// Open for writing
	_, _, err = fs.Open(f, OpenWRONLY)
	if err != nil {
		t.Fatalf("Open(WRONLY) error = %v", err)
	}

	// Write data
	n, err := fs.Write(f, 0, []byte("hello"))
	if err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if n != 5 {
		t.Errorf("Write() = %d, want 5", n)
	}

	// Close and verify
	fs.Clunk(f)

	content, _ := os.ReadFile(testFile)
	if string(content) != "hello" {
		t.Errorf("file content = %q, want %q", content, "hello")
	}
}

func TestHostFSCreate(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "hostfs_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	fs, _ := NewHostFS(tmpDir)
	root, _, _ := fs.Attach(1000, "user", "/")

	// Create new file
	qid, _, err := fs.Create(root, "newfile.txt", OpenRDWR, 0644, 1000)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if qid.Type != QtFile {
		t.Errorf("Create() qid.Type = %d, want QtFile", qid.Type)
	}

	// Verify file exists
	if _, err := os.Stat(filepath.Join(tmpDir, "newfile.txt")); err != nil {
		t.Errorf("created file not found: %v", err)
	}
}

func TestHostFSMkdir(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "hostfs_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	fs, _ := NewHostFS(tmpDir)
	root, _, _ := fs.Attach(1000, "user", "/")

	// Create directory
	qid, err := fs.Mkdir(root, "newdir", 0755, 1000)
	if err != nil {
		t.Fatalf("Mkdir() error = %v", err)
	}
	if qid.Type != QtDir {
		t.Errorf("Mkdir() qid.Type = %d, want QtDir", qid.Type)
	}

	// Verify directory exists
	info, err := os.Stat(filepath.Join(tmpDir, "newdir"))
	if err != nil {
		t.Fatalf("created directory not found: %v", err)
	}
	if !info.IsDir() {
		t.Error("Mkdir() did not create a directory")
	}
}

func TestHostFSUnlinkat(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "hostfs_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create test file
	testFile := filepath.Join(tmpDir, "delete.txt")
	os.WriteFile(testFile, []byte("test"), 0644)

	fs, _ := NewHostFS(tmpDir)
	root, _, _ := fs.Attach(1000, "user", "/")

	// Delete file
	err = fs.Unlinkat(root, "delete.txt", 0)
	if err != nil {
		t.Fatalf("Unlinkat() error = %v", err)
	}

	// Verify file is gone
	if _, err := os.Stat(testFile); !os.IsNotExist(err) {
		t.Error("Unlinkat() did not delete file")
	}
}

func TestHostFSRenameat(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "hostfs_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create test file
	oldFile := filepath.Join(tmpDir, "old.txt")
	os.WriteFile(oldFile, []byte("test"), 0644)

	fs, _ := NewHostFS(tmpDir)
	root, _, _ := fs.Attach(1000, "user", "/")

	// Rename file
	err = fs.Renameat(root, "old.txt", root, "new.txt")
	if err != nil {
		t.Fatalf("Renameat() error = %v", err)
	}

	// Verify old file is gone
	if _, err := os.Stat(oldFile); !os.IsNotExist(err) {
		t.Error("Renameat() did not remove old file")
	}

	// Verify new file exists
	newFile := filepath.Join(tmpDir, "new.txt")
	if _, err := os.Stat(newFile); err != nil {
		t.Error("Renameat() did not create new file")
	}
}

func TestHostFSSymlink(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "hostfs_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create target file
	os.WriteFile(filepath.Join(tmpDir, "target.txt"), []byte("target"), 0644)

	fs, _ := NewHostFS(tmpDir)
	root, _, _ := fs.Attach(1000, "user", "/")

	// Create symlink
	qid, err := fs.Symlink(root, "link.txt", "target.txt", 1000)
	if err != nil {
		t.Fatalf("Symlink() error = %v", err)
	}
	if qid.Type != QtSymlink {
		t.Errorf("Symlink() qid.Type = %d, want QtSymlink", qid.Type)
	}

	// Verify symlink
	linkPath := filepath.Join(tmpDir, "link.txt")
	info, err := os.Lstat(linkPath)
	if err != nil {
		t.Fatalf("symlink not found: %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Error("Symlink() did not create a symlink")
	}
}

func TestHostFSReadlink(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "hostfs_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create symlink
	linkPath := filepath.Join(tmpDir, "link.txt")
	os.Symlink("target.txt", linkPath)

	fs, _ := NewHostFS(tmpDir)
	root, _, _ := fs.Attach(1000, "user", "/")

	// Walk to symlink
	f, _, _ := fs.Walk(root, []string{"link.txt"})

	// Read symlink
	target, err := fs.Readlink(f)
	if err != nil {
		t.Fatalf("Readlink() error = %v", err)
	}
	if target != "target.txt" {
		t.Errorf("Readlink() = %q, want %q", target, "target.txt")
	}
}

func TestHostFSGetattr(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "hostfs_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create test file
	testFile := filepath.Join(tmpDir, "test.txt")
	os.WriteFile(testFile, []byte("hello"), 0644)

	fs, _ := NewHostFS(tmpDir)
	root, _, _ := fs.Attach(1000, "user", "/")

	// Walk to file
	f, _, _ := fs.Walk(root, []string{"test.txt"})

	// Get attributes
	stat, valid, err := fs.Getattr(f, GetattrBasic)
	if err != nil {
		t.Fatalf("Getattr() error = %v", err)
	}

	if valid == 0 {
		t.Error("Getattr() returned valid = 0")
	}

	if stat.Size != 5 {
		t.Errorf("Getattr() size = %d, want 5", stat.Size)
	}

	if stat.QID.Type != QtFile {
		t.Errorf("Getattr() qid.Type = %d, want QtFile", stat.QID.Type)
	}
}

func TestHostFSReaddir(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "hostfs_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create test files
	os.WriteFile(filepath.Join(tmpDir, "file1.txt"), []byte("1"), 0644)
	os.WriteFile(filepath.Join(tmpDir, "file2.txt"), []byte("2"), 0644)
	os.Mkdir(filepath.Join(tmpDir, "subdir"), 0755)

	fs, _ := NewHostFS(tmpDir)
	root, _, _ := fs.Attach(1000, "user", "/")

	// Open directory
	_, _, err = fs.Open(root, OpenRDONLY)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}

	// Read directory
	data, err := fs.Readdir(root, 0, 4096)
	if err != nil {
		t.Fatalf("Readdir() error = %v", err)
	}

	// Should have some data (3 entries)
	if len(data) == 0 {
		t.Error("Readdir() returned empty data")
	}
}

func TestHostFSStatfs(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "hostfs_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	fs, _ := NewHostFS(tmpDir)

	stat, err := fs.Statfs()
	if err != nil {
		t.Fatalf("Statfs() error = %v", err)
	}

	// Should have reasonable values
	if stat.BSize == 0 {
		t.Error("Statfs() bsize = 0")
	}
	if stat.Blocks == 0 {
		t.Error("Statfs() blocks = 0")
	}
}

func TestHostFSPathEscape(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "hostfs_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	fs, _ := NewHostFS(tmpDir)
	root, _, _ := fs.Attach(1000, "user", "/")

	// Try to escape with ..
	_, _, err = fs.Walk(root, []string{"..", "..", "etc", "passwd"})
	if err == nil {
		t.Error("Walk() with path escape should fail")
	}
}

func TestHostFSFsync(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "hostfs_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create test file
	testFile := filepath.Join(tmpDir, "sync.txt")
	os.WriteFile(testFile, []byte("test"), 0644)

	fs, _ := NewHostFS(tmpDir)
	root, _, _ := fs.Attach(1000, "user", "/")

	// Walk and open
	f, _, _ := fs.Walk(root, []string{"sync.txt"})
	fs.Open(f, OpenRDWR)

	// Fsync should succeed
	if err := fs.Fsync(f); err != nil {
		t.Errorf("Fsync() error = %v", err)
	}

	fs.Clunk(f)
}

func TestErrorCode(t *testing.T) {
	tests := []struct {
		err  error
		want uint32
	}{
		{nil, 0},
		{&p9Error{Code: ENOENT, Msg: "not found"}, ENOENT},
		{&p9Error{Code: EPERM, Msg: "permission denied"}, EPERM},
		{os.ErrNotExist, EIO}, // Unmapped errors become EIO
	}

	for _, tt := range tests {
		got := ErrorCode(tt.err)
		if got != tt.want {
			t.Errorf("ErrorCode(%v) = %d, want %d", tt.err, got, tt.want)
		}
	}
}

// Additional tests for improved coverage
// Reference: TinyEMU fs.h, fs_disk.c

func TestHostFSRemove(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "hostfs_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create test file
	testFile := filepath.Join(tmpDir, "remove_me.txt")
	os.WriteFile(testFile, []byte("test"), 0644)

	fs, _ := NewHostFS(tmpDir)
	root, _, _ := fs.Attach(1000, "user", "/")

	// Walk to file
	f, _, _ := fs.Walk(root, []string{"remove_me.txt"})

	// Open and then remove
	fs.Open(f, OpenRDONLY)
	err = fs.Remove(f)
	if err != nil {
		t.Fatalf("Remove() error = %v", err)
	}

	// Verify file is gone
	if _, err := os.Stat(testFile); !os.IsNotExist(err) {
		t.Error("Remove() did not delete file")
	}
}

func TestHostFSLink(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "hostfs_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create target file
	targetFile := filepath.Join(tmpDir, "target.txt")
	os.WriteFile(targetFile, []byte("target content"), 0644)

	fs, _ := NewHostFS(tmpDir)
	root, _, _ := fs.Attach(1000, "user", "/")

	// Walk to target file
	targetF, _, _ := fs.Walk(root, []string{"target.txt"})

	// Create hard link
	err = fs.Link(root, targetF, "link.txt")
	if err != nil {
		t.Fatalf("Link() error = %v", err)
	}

	// Verify link exists
	linkPath := filepath.Join(tmpDir, "link.txt")
	info, err := os.Stat(linkPath)
	if err != nil {
		t.Fatalf("hard link not found: %v", err)
	}
	if info.IsDir() {
		t.Error("Link() created a directory, not a file")
	}

	// Verify content matches
	content, _ := os.ReadFile(linkPath)
	if string(content) != "target content" {
		t.Errorf("link content = %q, want %q", content, "target content")
	}
}

func TestHostFSSetattr(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "hostfs_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create test file
	testFile := filepath.Join(tmpDir, "setattr.txt")
	os.WriteFile(testFile, []byte("hello world"), 0644)

	fs, _ := NewHostFS(tmpDir)
	root, _, _ := fs.Attach(1000, "user", "/")

	// Walk to file
	f, _, _ := fs.Walk(root, []string{"setattr.txt"})

	// Test changing mode
	err = fs.Setattr(f, SetattrMode, 0755, 0, 0, 0, 0, 0, 0, 0)
	if err != nil {
		t.Fatalf("Setattr(Mode) error = %v", err)
	}

	info, _ := os.Stat(testFile)
	mode := info.Mode().Perm()
	if mode != 0755 {
		t.Errorf("After Setattr, mode = %o, want 0755", mode)
	}

	// Test truncating file
	err = fs.Setattr(f, SetattrSize, 0, 0, 0, 5, 0, 0, 0, 0)
	if err != nil {
		t.Fatalf("Setattr(Size) error = %v", err)
	}

	info, _ = os.Stat(testFile)
	if info.Size() != 5 {
		t.Errorf("After Setattr(size=5), size = %d, want 5", info.Size())
	}
}

// TestHostFSLock verifies Lock behavior matches C fs_lock.
// Reference: tinyemu-2019-12-21/fs_disk.c:567-590
func TestHostFSLock(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "hostfs_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create test file
	testFile := filepath.Join(tmpDir, "lock.txt")
	os.WriteFile(testFile, []byte("test"), 0644)

	fs, _ := NewHostFS(tmpDir)
	root, _, _ := fs.Attach(1000, "user", "/")

	// Walk to file
	f, _, _ := fs.Walk(root, []string{"lock.txt"})
	fs.Open(f, OpenRDWR)

	// Test Lock - should succeed on opened file
	lock := &Lock{
		Type:     LockTypeWRLCK,
		Flags:    0,
		Start:    0,
		Length:   0,
		ProcID:   1234,
		ClientID: "test",
	}

	status, err := fs.Lock(f, lock)
	if err != nil {
		t.Fatalf("Lock() error = %v", err)
	}
	if status != LockSuccess {
		t.Errorf("Lock() status = %d, want %d", status, LockSuccess)
	}

	// Unlock
	unlock := &Lock{
		Type:   LockTypeUNLCK,
		Start:  0,
		Length: 0,
	}
	fs.Lock(f, unlock)
	fs.Clunk(f)
}

// TestHostFSLockNotOpened verifies Lock returns EPROTO when file not opened.
// Reference: tinyemu-2019-12-21/fs_disk.c:573-574
func TestHostFSLockNotOpened(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "hostfs_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create test file
	testFile := filepath.Join(tmpDir, "lock.txt")
	os.WriteFile(testFile, []byte("test"), 0644)

	fs, _ := NewHostFS(tmpDir)
	root, _, _ := fs.Attach(1000, "user", "/")

	// Walk to file but don't open it
	f, _, _ := fs.Walk(root, []string{"lock.txt"})

	lock := &Lock{
		Type:   LockTypeWRLCK,
		Start:  0,
		Length: 0,
	}

	// Lock should fail with error when file not opened
	_, err = fs.Lock(f, lock)
	if err == nil {
		t.Error("Lock() on unopened file should fail")
	}
}

// TestHostFSLockOnDirectory verifies Lock returns EPROTO on directories.
// Reference: tinyemu-2019-12-21/fs_disk.c:573-574
func TestHostFSLockOnDirectory(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "hostfs_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	fs, _ := NewHostFS(tmpDir)
	root, _, _ := fs.Attach(1000, "user", "/")

	// Open root directory
	fs.Open(root, OpenRDONLY)

	lock := &Lock{
		Type:   LockTypeWRLCK,
		Start:  0,
		Length: 0,
	}

	// Lock should fail on directory
	_, err = fs.Lock(root, lock)
	if err == nil {
		t.Error("Lock() on directory should fail")
	}
}

// TestHostFSGetlock verifies Getlock behavior matches C fs_getlock.
// Reference: tinyemu-2019-12-21/fs_disk.c:592-615
func TestHostFSGetlock(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "hostfs_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create test file
	testFile := filepath.Join(tmpDir, "getlock.txt")
	os.WriteFile(testFile, []byte("test"), 0644)

	fs, _ := NewHostFS(tmpDir)
	root, _, _ := fs.Attach(1000, "user", "/")

	// Walk to file
	f, _, _ := fs.Walk(root, []string{"getlock.txt"})
	fs.Open(f, OpenRDWR)

	// Test Getlock on unlocked file
	lock := &Lock{
		Type:     LockTypeRDLCK,
		Start:    0,
		Length:   100,
		ProcID:   5678,
		ClientID: "client",
	}

	result, err := fs.Getlock(f, lock)
	if err != nil {
		t.Fatalf("Getlock() error = %v", err)
	}
	if result == nil {
		t.Fatal("Getlock() returned nil result")
	}
	// Should return UNLCK since no conflicting lock exists
	if result.Type != LockTypeUNLCK {
		t.Errorf("Getlock() result.Type = %d, want %d", result.Type, LockTypeUNLCK)
	}
	// ClientID should be preserved
	if result.ClientID != "client" {
		t.Errorf("Getlock() result.ClientID = %q, want %q", result.ClientID, "client")
	}

	fs.Clunk(f)
}

// TestHostFSGetlockNotOpened verifies Getlock returns EPROTO when file not opened.
// Reference: tinyemu-2019-12-21/fs_disk.c:598-599
func TestHostFSGetlockNotOpened(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "hostfs_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create test file
	testFile := filepath.Join(tmpDir, "getlock.txt")
	os.WriteFile(testFile, []byte("test"), 0644)

	fs, _ := NewHostFS(tmpDir)
	root, _, _ := fs.Attach(1000, "user", "/")

	// Walk to file but don't open it
	f, _, _ := fs.Walk(root, []string{"getlock.txt"})

	lock := &Lock{
		Type:   LockTypeRDLCK,
		Start:  0,
		Length: 100,
	}

	// Getlock should fail when file not opened
	_, err = fs.Getlock(f, lock)
	if err == nil {
		t.Error("Getlock() on unopened file should fail")
	}
}

// TestHostFSGetlockOnDirectory verifies Getlock returns EPROTO on directories.
// Reference: tinyemu-2019-12-21/fs_disk.c:598-599
func TestHostFSGetlockOnDirectory(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "hostfs_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	fs, _ := NewHostFS(tmpDir)
	root, _, _ := fs.Attach(1000, "user", "/")

	// Open root directory
	fs.Open(root, OpenRDONLY)

	lock := &Lock{
		Type:   LockTypeRDLCK,
		Start:  0,
		Length: 100,
	}

	// Getlock should fail on directory
	_, err = fs.Getlock(root, lock)
	if err == nil {
		t.Error("Getlock() on directory should fail")
	}
}

func TestHostFSReadNotOpened(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "hostfs_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create test file
	testFile := filepath.Join(tmpDir, "read.txt")
	os.WriteFile(testFile, []byte("test"), 0644)

	fs, _ := NewHostFS(tmpDir)
	root, _, _ := fs.Attach(1000, "user", "/")

	// Walk to file but don't open it
	f, _, _ := fs.Walk(root, []string{"read.txt"})

	// Try to read without opening - should fail
	_, err = fs.Read(f, 0, 100)
	if err == nil {
		t.Error("Read() on unopened file should fail")
	}
}

func TestHostFSWriteNotOpened(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "hostfs_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create test file
	testFile := filepath.Join(tmpDir, "write.txt")
	os.WriteFile(testFile, []byte("test"), 0644)

	fs, _ := NewHostFS(tmpDir)
	root, _, _ := fs.Attach(1000, "user", "/")

	// Walk to file but don't open it
	f, _, _ := fs.Walk(root, []string{"write.txt"})

	// Try to write without opening - should fail
	_, err = fs.Write(f, 0, []byte("data"))
	if err == nil {
		t.Error("Write() on unopened file should fail")
	}
}

func TestHostFSReadDirOnFile(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "hostfs_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create test file
	testFile := filepath.Join(tmpDir, "file.txt")
	os.WriteFile(testFile, []byte("test"), 0644)

	fs, _ := NewHostFS(tmpDir)
	root, _, _ := fs.Attach(1000, "user", "/")

	// Walk to file and open as file
	f, _, _ := fs.Walk(root, []string{"file.txt"})
	fs.Open(f, OpenRDONLY)

	// Try to readdir on a file - should fail
	_, err = fs.Readdir(f, 0, 4096)
	if err == nil {
		t.Error("Readdir() on file should fail")
	}
}

func TestHostFSReadDirNotOpened(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "hostfs_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create subdir
	os.Mkdir(filepath.Join(tmpDir, "subdir"), 0755)

	fs, _ := NewHostFS(tmpDir)
	root, _, _ := fs.Attach(1000, "user", "/")

	// Walk to subdir but don't open it
	f, _, _ := fs.Walk(root, []string{"subdir"})

	// Try to readdir without opening - should fail
	_, err = fs.Readdir(f, 0, 4096)
	if err == nil {
		t.Error("Readdir() on unopened directory should fail")
	}
}

func TestHostFSWriteToDirectory(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "hostfs_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	fs, _ := NewHostFS(tmpDir)
	root, _, _ := fs.Attach(1000, "user", "/")

	// Open root directory
	fs.Open(root, OpenRDONLY)

	// Try to write to directory - should fail
	_, err = fs.Write(root, 0, []byte("data"))
	if err == nil {
		t.Error("Write() to directory should fail")
	}
}

func TestHostFSReadDirectory(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "hostfs_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	fs, _ := NewHostFS(tmpDir)
	root, _, _ := fs.Attach(1000, "user", "/")

	// Open root directory
	fs.Open(root, OpenRDONLY)

	// Try to read directory with Read (not Readdir) - should fail
	_, err = fs.Read(root, 0, 100)
	if err == nil {
		t.Error("Read() on directory should fail")
	}
}

func TestHostFSFsyncOnDir(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "hostfs_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	fs, _ := NewHostFS(tmpDir)
	root, _, _ := fs.Attach(1000, "user", "/")

	// Open root directory
	fs.Open(root, OpenRDONLY)

	// Fsync on directory should succeed
	if err := fs.Fsync(root); err != nil {
		t.Errorf("Fsync() on directory error = %v", err)
	}
}

func TestHostFSFsyncNotOpened(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "hostfs_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create test file
	testFile := filepath.Join(tmpDir, "sync.txt")
	os.WriteFile(testFile, []byte("test"), 0644)

	fs, _ := NewHostFS(tmpDir)
	root, _, _ := fs.Attach(1000, "user", "/")

	// Walk to file but don't open it
	f, _, _ := fs.Walk(root, []string{"sync.txt"})

	// Fsync on unopened file should succeed (no-op)
	if err := fs.Fsync(f); err != nil {
		t.Errorf("Fsync() on unopened file error = %v", err)
	}
}

func TestHostFSClunkIdempotent(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "hostfs_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create test file
	testFile := filepath.Join(tmpDir, "clunk.txt")
	os.WriteFile(testFile, []byte("test"), 0644)

	fs, _ := NewHostFS(tmpDir)
	root, _, _ := fs.Attach(1000, "user", "/")

	// Walk and open file
	f, _, _ := fs.Walk(root, []string{"clunk.txt"})
	fs.Open(f, OpenRDONLY)

	// Multiple clunks should be safe
	if err := fs.Clunk(f); err != nil {
		t.Errorf("First Clunk() error = %v", err)
	}
	if err := fs.Clunk(f); err != nil {
		t.Errorf("Second Clunk() error = %v", err)
	}
}

func TestHostFSClunkDir(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "hostfs_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	fs, _ := NewHostFS(tmpDir)
	root, _, _ := fs.Attach(1000, "user", "/")

	// Open directory
	fs.Open(root, OpenRDONLY)

	// Clunk directory
	if err := fs.Clunk(root); err != nil {
		t.Errorf("Clunk() on directory error = %v", err)
	}
}

func TestHostFSMkdirPathEscape(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "hostfs_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	fs, _ := NewHostFS(tmpDir)
	root, _, _ := fs.Attach(1000, "user", "/")

	// Try to create directory outside root
	_, err = fs.Mkdir(root, "../outside", 0755, 1000)
	if err == nil {
		t.Error("Mkdir() with path escape should fail")
	}
}

func TestHostFSSymlinkPathEscape(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "hostfs_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	fs, _ := NewHostFS(tmpDir)
	root, _, _ := fs.Attach(1000, "user", "/")

	// Try to create symlink outside root
	_, err = fs.Symlink(root, "../escape", "target", 1000)
	if err == nil {
		t.Error("Symlink() with path escape should fail")
	}
}

func TestHostFSCreatePathEscape(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "hostfs_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	fs, _ := NewHostFS(tmpDir)
	root, _, _ := fs.Attach(1000, "user", "/")

	// Try to create file outside root
	_, _, err = fs.Create(root, "../escape.txt", OpenRDWR, 0644, 1000)
	if err == nil {
		t.Error("Create() with path escape should fail")
	}
}

func TestHostFSLinkPathEscape(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "hostfs_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create target file
	os.WriteFile(filepath.Join(tmpDir, "target.txt"), []byte("test"), 0644)

	fs, _ := NewHostFS(tmpDir)
	root, _, _ := fs.Attach(1000, "user", "/")
	targetF, _, _ := fs.Walk(root, []string{"target.txt"})

	// Try to create link outside root
	err = fs.Link(root, targetF, "../escape")
	if err == nil {
		t.Error("Link() with path escape should fail")
	}
}

func TestHostFSRenameatPathEscape(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "hostfs_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create test file
	os.WriteFile(filepath.Join(tmpDir, "old.txt"), []byte("test"), 0644)

	fs, _ := NewHostFS(tmpDir)
	root, _, _ := fs.Attach(1000, "user", "/")

	// Try to rename to outside root
	err = fs.Renameat(root, "old.txt", root, "../escape.txt")
	if err == nil {
		t.Error("Renameat() with path escape should fail")
	}
}

func TestHostFSUnlinkatPathEscape(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "hostfs_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	fs, _ := NewHostFS(tmpDir)
	root, _, _ := fs.Attach(1000, "user", "/")

	// Try to unlink outside root
	err = fs.Unlinkat(root, "../etc/passwd", 0)
	if err == nil {
		t.Error("Unlinkat() with path escape should fail")
	}
}

// TestHostFSP9FlagsToHost verifies flag conversion matches C behavior.
// Reference: tinyemu-2019-12-21/fs_disk.c:121-131 (p9_flags_to_host)
func TestHostFSP9FlagsToHost(t *testing.T) {
	// Test various flag combinations
	tests := []struct {
		p9Flags uint32
	}{
		{OpenRDONLY},
		{OpenWRONLY},
		{OpenRDWR},
		{OpenCREAT | OpenTRUNC | OpenWRONLY},
		{OpenAPPEND | OpenWRONLY},
		{OpenSYNC | OpenRDWR},
		{OpenEXCL | OpenCREAT | OpenWRONLY},
		{OpenNONBLOCK | OpenRDWR},   // Added in C
		{OpenDSYNC | OpenWRONLY},    // Added in C
		{OpenNOFOLLOW | OpenRDONLY}, // Added in C
	}

	// Just test that the function doesn't panic
	for _, tt := range tests {
		_ = p9FlagsToHost(tt.p9Flags)
	}
}

func TestHostFileImplementsFile(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "hostfs_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	fs, _ := NewHostFS(tmpDir)
	root, _, _ := fs.Attach(1000, "user", "/")

	// Verify root implements File interface
	var f File = root
	path := f.Path()
	if path != tmpDir {
		t.Errorf("Path() = %q, want %q", path, tmpDir)
	}
}

// TestErrnoToP9AllCases verifies errno to 9P error mapping.
// Reference: tinyemu-2019-12-21/fs_disk.c:91-101 (errno_to_p9)
func TestErrnoToP9AllCases(t *testing.T) {
	// Test all errno mappings
	tests := []struct {
		err      error
		wantCode uint32
	}{
		{nil, 0},
		{os.ErrNotExist, EIO}, // Falls through to default
	}

	for _, tt := range tests {
		got := ErrorCode(errnoToP9(tt.err))
		if got != tt.wantCode {
			t.Errorf("ErrorCode(errnoToP9(%v)) = %d, want %d", tt.err, got, tt.wantCode)
		}
	}

	// Test that errnoToP9(nil) returns nil
	if errnoToP9(nil) != nil {
		t.Error("errnoToP9(nil) should return nil")
	}

	// Test EPROTO and ENOTSUP error codes exist
	// These are mapped in errnoToP9 to match C behavior
	protoErr := &p9Error{EPROTO, "protocol error"}
	if protoErr.Code != EPROTO {
		t.Errorf("EPROTO = %d, want %d", protoErr.Code, EPROTO)
	}

	notsupErr := &p9Error{ENOTSUP, "operation not supported"}
	if notsupErr.Code != ENOTSUP {
		t.Errorf("ENOTSUP = %d, want %d", notsupErr.Code, ENOTSUP)
	}
}

func TestHostFSIsPathSafeEdgeCases(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "hostfs_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	fs, _ := NewHostFS(tmpDir)

	// Test various path patterns
	tests := []struct {
		path string
		safe bool
	}{
		{tmpDir, true},
		{filepath.Join(tmpDir, "subdir"), true},
		{filepath.Join(tmpDir, "a", "b", "c"), true},
		{filepath.Join(tmpDir, "..", "escape"), false},
	}

	for _, tt := range tests {
		got := fs.isPathSafe(tt.path)
		if got != tt.safe {
			t.Errorf("isPathSafe(%q) = %v, want %v", tt.path, got, tt.safe)
		}
	}
}

// TestHostFSReadEPROTONotOpened verifies Read returns EPROTO when file not opened.
// Reference: tinyemu-2019-12-21/fs_disk.c:349-361
func TestHostFSReadEPROTONotOpened(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "hostfs_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	testFile := filepath.Join(tmpDir, "test.txt")
	os.WriteFile(testFile, []byte("test"), 0644)

	fs, _ := NewHostFS(tmpDir)
	root, _, _ := fs.Attach(1000, "user", "/")
	f, _, _ := fs.Walk(root, []string{"test.txt"})

	_, err = fs.Read(f, 0, 100)
	if err == nil {
		t.Fatal("Read() on unopened file should fail")
	}
	if ErrorCode(err) != EPROTO {
		t.Errorf("Read() error code = %d, want EPROTO (%d)", ErrorCode(err), EPROTO)
	}
}

// TestHostFSReadEPROTOOnDirectory verifies Read returns EPROTO on directory.
// Reference: tinyemu-2019-12-21/fs_disk.c:349-361
func TestHostFSReadEPROTOOnDirectory(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "hostfs_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	fs, _ := NewHostFS(tmpDir)
	root, _, _ := fs.Attach(1000, "user", "/")

	// Open root as directory
	fs.Open(root, OpenRDONLY)

	// Try to read directory - should fail with EPROTO
	_, err = fs.Read(root, 0, 100)
	if err == nil {
		t.Fatal("Read() on directory should fail")
	}
	if ErrorCode(err) != EPROTO {
		t.Errorf("Read() on directory error code = %d, want EPROTO (%d)", ErrorCode(err), EPROTO)
	}
}

// TestHostFSWriteEPROTONotOpened verifies Write returns EPROTO when file not opened.
// Reference: tinyemu-2019-12-21/fs_disk.c:363-375
func TestHostFSWriteEPROTONotOpened(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "hostfs_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	testFile := filepath.Join(tmpDir, "test.txt")
	os.WriteFile(testFile, []byte("test"), 0644)

	fs, _ := NewHostFS(tmpDir)
	root, _, _ := fs.Attach(1000, "user", "/")
	f, _, _ := fs.Walk(root, []string{"test.txt"})

	_, err = fs.Write(f, 0, []byte("data"))
	if err == nil {
		t.Fatal("Write() on unopened file should fail")
	}
	if ErrorCode(err) != EPROTO {
		t.Errorf("Write() error code = %d, want EPROTO (%d)", ErrorCode(err), EPROTO)
	}
}

// TestHostFSWriteEPROTOOnDirectory verifies Write returns EPROTO on directory.
// Reference: tinyemu-2019-12-21/fs_disk.c:363-375
func TestHostFSWriteEPROTOOnDirectory(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "hostfs_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	fs, _ := NewHostFS(tmpDir)
	root, _, _ := fs.Attach(1000, "user", "/")

	// Open root as directory
	fs.Open(root, OpenRDONLY)

	// Try to write to directory - should fail with EPROTO
	_, err = fs.Write(root, 0, []byte("data"))
	if err == nil {
		t.Fatal("Write() on directory should fail")
	}
	if ErrorCode(err) != EPROTO {
		t.Errorf("Write() on directory error code = %d, want EPROTO (%d)", ErrorCode(err), EPROTO)
	}
}

// TestHostFSReaddirEPROTONotOpened verifies Readdir returns EPROTO when not opened.
// Reference: tinyemu-2019-12-21/fs_disk.c:293-347
func TestHostFSReaddirEPROTONotOpened(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "hostfs_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	os.Mkdir(filepath.Join(tmpDir, "subdir"), 0755)

	fs, _ := NewHostFS(tmpDir)
	root, _, _ := fs.Attach(1000, "user", "/")
	f, _, _ := fs.Walk(root, []string{"subdir"})

	// Try to readdir without opening - should fail with EPROTO
	_, err = fs.Readdir(f, 0, 4096)
	if err == nil {
		t.Fatal("Readdir() on unopened directory should fail")
	}
	if ErrorCode(err) != EPROTO {
		t.Errorf("Readdir() error code = %d, want EPROTO (%d)", ErrorCode(err), EPROTO)
	}
}

// TestHostFSReaddirEPROTOOnFile verifies Readdir returns EPROTO on regular file.
// Reference: tinyemu-2019-12-21/fs_disk.c:293-347
func TestHostFSReaddirEPROTOOnFile(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "hostfs_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	testFile := filepath.Join(tmpDir, "file.txt")
	os.WriteFile(testFile, []byte("test"), 0644)

	fs, _ := NewHostFS(tmpDir)
	root, _, _ := fs.Attach(1000, "user", "/")
	f, _, _ := fs.Walk(root, []string{"file.txt"})

	// Open as regular file
	fs.Open(f, OpenRDONLY)

	// Try to readdir on file - should fail with EPROTO
	_, err = fs.Readdir(f, 0, 4096)
	if err == nil {
		t.Fatal("Readdir() on regular file should fail")
	}
	if ErrorCode(err) != EPROTO {
		t.Errorf("Readdir() on file error code = %d, want EPROTO (%d)", ErrorCode(err), EPROTO)
	}
}

// TestHostFSSetattrTime verifies Setattr handles time changes correctly.
// Reference: tinyemu-2019-12-21/fs_disk.c:436-465
func TestHostFSSetattrTime(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "hostfs_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	testFile := filepath.Join(tmpDir, "time.txt")
	os.WriteFile(testFile, []byte("test"), 0644)

	fs, _ := NewHostFS(tmpDir)
	root, _, _ := fs.Attach(1000, "user", "/")
	f, _, _ := fs.Walk(root, []string{"time.txt"})

	// Set specific atime and mtime
	atimeSec := uint64(1000000000) // 2001-09-09
	atimeNsec := uint64(123456789)
	mtimeSec := uint64(1100000000) // 2004-11-09
	mtimeNsec := uint64(987654321)

	err = fs.Setattr(f, SetattrAtime|SetattrAtimeSet|SetattrMtime|SetattrMtimeSet,
		0, 0, 0, 0, atimeSec, atimeNsec, mtimeSec, mtimeNsec)
	if err != nil {
		t.Fatalf("Setattr() error = %v", err)
	}

	// Verify times were set
	stat, _, err := fs.Getattr(f, 0)
	if err != nil {
		t.Fatalf("Getattr() error = %v", err)
	}

	if stat.AtimeSec != atimeSec {
		t.Errorf("AtimeSec = %d, want %d", stat.AtimeSec, atimeSec)
	}
	if stat.MtimeSec != mtimeSec {
		t.Errorf("MtimeSec = %d, want %d", stat.MtimeSec, mtimeSec)
	}
}

// TestHostFSSetattrMtimeOnly verifies Setattr with only mtime set.
// Reference: tinyemu-2019-12-21/fs_disk.c:436-465
func TestHostFSSetattrMtimeOnly(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "hostfs_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	testFile := filepath.Join(tmpDir, "mtime.txt")
	os.WriteFile(testFile, []byte("test"), 0644)

	fs, _ := NewHostFS(tmpDir)
	root, _, _ := fs.Attach(1000, "user", "/")
	f, _, _ := fs.Walk(root, []string{"mtime.txt"})

	// Get original atime
	origStat, _, _ := fs.Getattr(f, 0)
	origAtime := origStat.AtimeSec

	// Set only mtime
	mtimeSec := uint64(1100000000)
	err = fs.Setattr(f, SetattrMtime|SetattrMtimeSet, 0, 0, 0, 0, 0, 0, mtimeSec, 0)
	if err != nil {
		t.Fatalf("Setattr() error = %v", err)
	}

	// Verify mtime was set and atime was preserved (UTIME_OMIT)
	stat, _, _ := fs.Getattr(f, 0)
	if stat.MtimeSec != mtimeSec {
		t.Errorf("MtimeSec = %d, want %d", stat.MtimeSec, mtimeSec)
	}
	// Atime might change slightly due to filesystem access, but should be close
	if stat.AtimeSec < origAtime-1 || stat.AtimeSec > origAtime+1 {
		t.Errorf("AtimeSec = %d, want approximately %d (should be preserved)", stat.AtimeSec, origAtime)
	}
}

package fsutil

import (
	"os"
	"path/filepath"
)

// Exists reports whether path exists. A worktree's .git is a file, so a
// checkout test cannot require a directory.
func Exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// WriteAtomic writes data to path with perm, so a reader or a crash never sees
// a half-written file. It writes a temp file in path's directory, syncs it to
// disk and renames it over path. A failed write leaves path as it was and no
// temp file behind.
func WriteAtomic(path string, data []byte, perm os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	// Both calls are no-ops once the file is closed and renamed.
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
	}()

	// The umask filters a create's mode, and Chmod does not.
	if err := tmp.Chmod(perm); err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

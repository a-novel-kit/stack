package paths

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

// ErrLocked reports a lock another process holds.
var ErrLocked = errors.New("held by another process")

// Lock takes an exclusive flock on the file at path, waiting while another
// process holds it, and returns its release. The kernel drops the lock when the
// process ends, however it ends.
//
// The file may sit in a shared directory such as /tmp, so it opens without
// following a symlink and must belong to this user: a lock file another user
// planted could otherwise be held forever.
func Lock(path string) (func(), error) {
	return lock(path, syscall.LOCK_EX)
}

// TryLock is Lock without the wait: it fails at once with [ErrLocked] while
// another process holds the lock.
func TryLock(path string) (func(), error) {
	return lock(path, syscall.LOCK_EX|syscall.LOCK_NB)
}

// lock flocks the file at path with how.
func lock(path string, how int) (func(), error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDONLY|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open lock %s: %w", path, err)
	}

	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("stat lock %s: %w", path, err)
	}
	if !OwnedByUser(info) {
		_ = f.Close()
		return nil, fmt.Errorf("lock %s: %w", path, ErrForeignOwner)
	}

	if err := syscall.Flock(int(f.Fd()), how); err != nil {
		_ = f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			err = ErrLocked
		}
		return nil, fmt.Errorf("lock %s: %w", path, err)
	}
	return func() { _ = f.Close() }, nil
}

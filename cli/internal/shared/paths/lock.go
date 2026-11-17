package paths

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

// ErrLocked reports a lock another process holds.
var ErrLocked = errors.New("held by another process")

// Lock takes an exclusive flock on the file at path and returns its release.
// It waits while another process holds the lock or, with wait false, fails at
// once with [ErrLocked]. The kernel drops the lock when the process ends,
// however it ends.
//
// The file may sit in a shared directory such as /tmp, so it opens without
// following a symlink and must belong to this user: a lock file another user
// planted could otherwise be held forever.
func Lock(path string, wait bool) (func(), error) {
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

	how := syscall.LOCK_EX
	if !wait {
		how |= syscall.LOCK_NB
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

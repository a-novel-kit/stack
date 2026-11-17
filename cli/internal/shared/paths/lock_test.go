package paths_test

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/a-novel-kit/stack/cli/internal/shared/paths"
)

func TestLock(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "daemon.sock.lock")
	release, err := paths.Lock(path)
	require.NoError(t, err)

	_, err = paths.TryLock(path)
	require.ErrorIs(t, err, paths.ErrLocked, "Lock holds the lock until its release")

	release()
	release, err = paths.Lock(path)
	require.NoError(t, err)
	release()
}

func TestTryLock(t *testing.T) {
	t.Parallel()

	t.Run("Success/ReleaseFreesLock", func(t *testing.T) {
		t.Parallel()

		path := filepath.Join(t.TempDir(), "env.lock")
		release, err := paths.TryLock(path)
		require.NoError(t, err)
		release()

		release, err = paths.TryLock(path)
		require.NoError(t, err)
		release()
	})

	t.Run("Error/Held", func(t *testing.T) {
		t.Parallel()

		path := filepath.Join(t.TempDir(), "env.lock")
		release, err := paths.TryLock(path)
		require.NoError(t, err)
		t.Cleanup(release)

		// flock locks belong to the open file, so a second open in this
		// process contends like another process would.
		_, err = paths.TryLock(path)
		require.ErrorIs(t, err, paths.ErrLocked)
	})

	t.Run("Error/Symlink", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		path := filepath.Join(dir, "env.lock")
		require.NoError(t, os.Symlink(filepath.Join(dir, "target"), path))

		_, err := paths.TryLock(path)
		require.ErrorIs(t, err, syscall.ELOOP)
	})
}

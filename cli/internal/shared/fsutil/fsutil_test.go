package fsutil_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/a-novel-kit/stack/cli/internal/shared/fsutil"
)

func TestExists(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	require.True(t, fsutil.Exists(dir))
	require.False(t, fsutil.Exists(filepath.Join(dir, "missing")))
}

func TestWriteAtomic(t *testing.T) {
	t.Parallel()

	t.Run("Success/ReplacesWithPerm", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		path := filepath.Join(dir, "store")
		require.NoError(t, os.WriteFile(path, []byte("old"), 0o644))

		require.NoError(t, fsutil.WriteAtomic(path, []byte("new"), 0o600))

		data, err := os.ReadFile(path)
		require.NoError(t, err)
		require.Equal(t, "new", string(data))

		info, err := os.Stat(path)
		require.NoError(t, err)
		require.Equal(t, os.FileMode(0o600), info.Mode().Perm())

		entries, err := os.ReadDir(dir)
		require.NoError(t, err)
		require.Len(t, entries, 1, "no temp file stays behind")
	})

	t.Run("Error/MissingDirectory", func(t *testing.T) {
		t.Parallel()

		err := fsutil.WriteAtomic(filepath.Join(t.TempDir(), "missing", "store"), []byte("x"), 0o600)
		require.ErrorIs(t, err, os.ErrNotExist)
	})
}

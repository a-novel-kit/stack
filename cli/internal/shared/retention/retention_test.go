package retention_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/a-novel-kit/stack/cli/internal/shared/fsutil/fsutiltest"
	"github.com/a-novel-kit/stack/cli/internal/shared/retention"
)

func TestPrune(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	fsutiltest.Write(t, dir, map[string]string{"d.zst": "", "c.zst": "", "b.zst": "", "a.zst": "", "keep.txt": ""})
	// Names run against modification order, so only the mtime can rank them.
	now := time.Now()
	for i, name := range []string{"d.zst", "c.zst", "b.zst", "a.zst", "keep.txt"} {
		mod := now.Add(time.Duration(i) * time.Minute)
		require.NoError(t, os.Chtimes(filepath.Join(dir, name), mod, mod))
	}
	require.NoError(t, os.Mkdir(filepath.Join(dir, "sub.zst"), 0o700))
	isArchive := func(name string) bool { return strings.HasSuffix(name, ".zst") }

	retention.Prune(dir, isArchive, 2)

	files := retention.Files(dir, func(string) bool { return true })
	got := make([]string, len(files))
	for i, path := range files {
		got[i] = filepath.Base(path)
	}
	// The two newest archives stay, oldest first; the unmatched file and the
	// directory are never candidates.
	require.Equal(t, []string{"b.zst", "a.zst", "keep.txt"}, got)
}

// TestFiles pins that an unreadable dir yields no file.
func TestFiles(t *testing.T) {
	t.Parallel()

	require.Empty(t, retention.Files(filepath.Join(t.TempDir(), "missing"), func(string) bool { return true }))
}

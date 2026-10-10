package retention

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestPrune(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	now := time.Now()
	// Names run against modification order, so only the mtime can rank them.
	for i, name := range []string{"d.zst", "c.zst", "b.zst", "a.zst", "keep.txt"} {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		mod := now.Add(time.Duration(i) * time.Minute)
		if err := os.Chtimes(path, mod, mod); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(dir, "sub.zst"), 0o700); err != nil {
		t.Fatal(err)
	}
	isArchive := func(name string) bool { return strings.HasSuffix(name, ".zst") }

	Prune(dir, isArchive, 2)

	files := Files(dir, func(string) bool { return true })
	got := make([]string, len(files))
	for i, path := range files {
		got[i] = filepath.Base(path)
	}
	// The two newest archives stay, oldest first; the unmatched file and the
	// directory are never candidates.
	if want := []string{"b.zst", "a.zst", "keep.txt"}; !slices.Equal(got, want) {
		t.Errorf("after Prune: got %q, want %q", got, want)
	}
}

func TestFilesUnreadableDir(t *testing.T) {
	t.Parallel()

	if got := Files(filepath.Join(t.TempDir(), "missing"), func(string) bool { return true }); len(got) != 0 {
		t.Errorf("Files on a missing dir: got %q, want none", got)
	}
}

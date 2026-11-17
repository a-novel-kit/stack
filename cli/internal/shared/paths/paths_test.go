package paths_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/a-novel-kit/stack/cli/internal/shared/fsutil/fsutiltest"
	"github.com/a-novel-kit/stack/cli/internal/shared/paths"
)

// TestCheckOwner pins that a path passes only while it is missing or belongs to
// the current user, a symlink judged by the link itself.
func TestCheckOwner(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	fsutiltest.Write(t, dir, map[string]string{"own.sock": ""})
	own := filepath.Join(dir, "own.sock")
	link := filepath.Join(dir, "link.sock")
	require.NoError(t, os.Symlink(own, link))

	testCases := []struct {
		name string
		path string
	}{
		{name: "Success/Missing", path: filepath.Join(dir, "missing.sock")},
		{name: "Success/Own", path: own},
		{name: "Success/OwnSymlink", path: link},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.NoError(t, paths.CheckOwner(testCase.path))
		})
	}
}

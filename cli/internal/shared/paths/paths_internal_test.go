package paths

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/a-novel-kit/stack/cli/internal/shared/fsutil/fsutiltest"
)

// fakeInfo is a FileInfo with no unix owner behind it.
type fakeInfo struct{}

func (fakeInfo) Name() string       { return "fake" }
func (fakeInfo) Size() int64        { return 0 }
func (fakeInfo) Mode() fs.FileMode  { return 0 }
func (fakeInfo) ModTime() time.Time { return time.Time{} }
func (fakeInfo) IsDir() bool        { return false }
func (fakeInfo) Sys() any           { return nil }

// TestOwnedBy pins the owner comparison CheckOwner rests on, including the
// other user a test cannot create a file as.
func TestOwnedBy(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	fsutiltest.Write(t, dir, map[string]string{"own.sock": ""})
	info, err := os.Lstat(filepath.Join(dir, "own.sock"))
	require.NoError(t, err)

	testCases := []struct {
		name string
		info fs.FileInfo
		uid  int
		want bool
	}{
		{name: "Success/CurrentUser", info: info, uid: os.Getuid(), want: true},
		{name: "Error/OtherUser", info: info, uid: os.Getuid() + 1},
		{name: "Error/NoOwner", info: fakeInfo{}, uid: os.Getuid()},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, testCase.want, ownedBy(testCase.info, testCase.uid))
		})
	}
}

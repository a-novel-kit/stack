package paths

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestCheckOwner pins that a path passes only while it is missing or belongs to
// the current user, a symlink judged by the link itself.
func TestCheckOwner(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	own := filepath.Join(dir, "own.sock")
	if err := os.WriteFile(own, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.sock")
	if err := os.Symlink(own, link); err != nil {
		t.Fatal(err)
	}

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

			if err := CheckOwner(testCase.path); err != nil {
				t.Fatalf("CheckOwner(%s) = %v, want nil", testCase.path, err)
			}
		})
	}
}

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

	path := filepath.Join(t.TempDir(), "own.sock")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}

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

			if got := ownedBy(testCase.info, testCase.uid); got != testCase.want {
				t.Fatalf("ownedBy = %v, want %v", got, testCase.want)
			}
		})
	}
}

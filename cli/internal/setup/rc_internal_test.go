package setup

import (
	"cmp"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUpsertRCBlock(t *testing.T) {
	t.Parallel()

	const block = "a-novel core start\n"
	managed := beginMarker + "\n" + block + endMarker + "\n"

	testCases := []struct {
		name string

		existing *string     // nil leaves the rc file absent
		mode     os.FileMode // the seeded rc's mode, 0o600 when zero
		// linked makes the rc a symlink to the seeded file, as a dotfiles
		// checkout links it.
		linked bool

		want        string
		wantChanged bool
		wantErr     bool
	}{
		{name: "Success/MissingFile", want: managed, wantChanged: true},
		{name: "Success/EmptyFile", existing: new(""), want: managed, wantChanged: true},
		{
			name:     "Success/AppendAfterTrailingNewline",
			existing: new("export A=1\n"), want: "export A=1\n\n" + managed, wantChanged: true,
		},
		{
			name:     "Success/AppendWithoutTrailingNewline",
			existing: new("export A=1"), want: "export A=1\n\n" + managed, wantChanged: true,
		},
		{
			name:     "Success/ReplaceStaleBlock",
			existing: new("x\n" + beginMarker + "\nold\n" + endMarker + "\ny\n"), want: "x\n" + managed + "y\n",
			wantChanged: true,
		},
		{name: "Success/UpToDate", existing: new("x\n" + managed), want: "x\n" + managed},
		{
			name:     "Success/KeepMode",
			existing: new("export A=1\n"), mode: 0o644, want: "export A=1\n\n" + managed, wantChanged: true,
		},
		{
			name:     "Success/ReplaceThroughSymlink",
			existing: new("x\n" + beginMarker + "\nold\n" + endMarker + "\n"), mode: 0o644, linked: true,
			want: "x\n" + managed, wantChanged: true,
		},
		{
			name:     "Error/Malformed",
			existing: new("x\n" + beginMarker + "\n"), want: "x\n" + beginMarker + "\n", wantErr: true,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			rcPath := filepath.Join(t.TempDir(), ".zshrc")
			mode := cmp.Or(testCase.mode, 0o600)
			if testCase.existing != nil {
				seedRC(t, rcPath, *testCase.existing, mode, testCase.linked)
			}

			_, changed, err := upsertRCBlock(rcPath, block)
			if testCase.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, testCase.wantChanged, changed)

			got, err := os.ReadFile(rcPath)
			require.NoError(t, err)
			require.Equal(t, testCase.want, string(got))

			link, err := os.Lstat(rcPath)
			require.NoError(t, err)
			require.Equal(t, testCase.linked, link.Mode()&os.ModeSymlink != 0, "rc is a symlink")

			info, err := os.Stat(rcPath)
			require.NoError(t, err)
			require.Equal(t, mode, info.Mode().Perm())
		})
	}
}

// seedRC writes content at mode to rcPath, or, when linked, to a file elsewhere
// that rcPath links to.
func seedRC(t *testing.T, rcPath, content string, mode os.FileMode, linked bool) {
	t.Helper()

	file := rcPath
	if linked {
		file = filepath.Join(filepath.Dir(rcPath), "dotfiles", "zshrc")
		if err := os.Mkdir(filepath.Dir(file), 0o700); err != nil {
			panic(err)
		}
		if err := os.Symlink(file, rcPath); err != nil {
			panic(err)
		}
	}
	// The umask filters WriteFile's mode, so Chmod sets it exactly.
	if err := os.WriteFile(file, []byte(content), mode); err != nil {
		panic(err)
	}
	if err := os.Chmod(file, mode); err != nil {
		panic(err)
	}
}

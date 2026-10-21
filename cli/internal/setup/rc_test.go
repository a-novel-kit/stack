package setup

import (
	"cmp"
	"os"
	"path/filepath"
	"testing"
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

			dir := t.TempDir()
			rcPath := filepath.Join(dir, ".zshrc")
			mode := cmp.Or(testCase.mode, 0o600)
			if testCase.existing != nil {
				seedRC(t, rcPath, *testCase.existing, mode, testCase.linked)
			}

			_, changed, err := upsertRCBlock(rcPath, block)
			if (err != nil) != testCase.wantErr {
				t.Fatalf("upsertRCBlock error = %v, wantErr %v", err, testCase.wantErr)
			}
			if changed != testCase.wantChanged {
				t.Errorf("upsertRCBlock changed = %v, want %v", changed, testCase.wantChanged)
			}
			got, err := os.ReadFile(rcPath)
			if err != nil {
				t.Fatalf("read rc: %v", err)
			}
			if string(got) != testCase.want {
				t.Errorf("rc content = %q, want %q", got, testCase.want)
			}
			link, err := os.Lstat(rcPath)
			if err != nil {
				t.Fatalf("lstat rc: %v", err)
			}
			if isLink := link.Mode()&os.ModeSymlink != 0; isLink != testCase.linked {
				t.Errorf("rc is a symlink = %v, want %v", isLink, testCase.linked)
			}
			info, err := os.Stat(rcPath)
			if err != nil {
				t.Fatalf("stat rc: %v", err)
			}
			if perm := info.Mode().Perm(); perm != mode {
				t.Errorf("rc mode = %o, want %o", perm, mode)
			}
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

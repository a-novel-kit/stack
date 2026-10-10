package volumes

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/klauspost/compress/zstd"
)

// Tests for the backup write path. The archive these produce is what `clear`
// destroys a volume against, so a write that half-succeeded must not be
// reported as a backup.

var errDiskFull = errors.New("no space left on device")

// errWriter fails every write. zstd buffers, so for a small payload nothing
// reaches the destination until Close flushes the final block and the frame
// checksum — which makes this a faithful stand-in for a disk that fills at
// exactly the moment everything else has already looked fine.
type errWriter struct{}

func (errWriter) Write(p []byte) (int, error) { return 0, errDiskFull }

func TestCompressToReportsFlushFailure(t *testing.T) {
	if err := compressTo(errWriter{}, bytes.NewReader([]byte("volume contents"))); !errors.Is(err, errDiskFull) {
		t.Errorf("compressTo: got %v, want it to wrap the destination's %v", err, errDiskFull)
	}
}

func TestCompressToRoundTrips(t *testing.T) {
	payload := bytes.Repeat([]byte("volume contents "), 1024)

	var buf bytes.Buffer
	if err := compressTo(&buf, bytes.NewReader(payload)); err != nil {
		t.Fatalf("compressTo: %v", err)
	}

	dec, err := zstd.NewReader(&buf)
	if err != nil {
		t.Fatalf("zstd.NewReader: %v", err)
	}
	defer dec.Close()

	got, err := io.ReadAll(dec)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}

	if !bytes.Equal(got, payload) {
		t.Errorf("round trip: got %d bytes, want %d", len(got), len(payload))
	}
}

func TestValidateArchiveRejectsTruncated(t *testing.T) {
	dir := t.TempDir()

	var buf bytes.Buffer
	if err := compressTo(&buf, bytes.NewReader(bytes.Repeat([]byte("x"), 64*1024))); err != nil {
		t.Fatalf("compressTo: %v", err)
	}

	complete := filepath.Join(dir, "complete.tar.zst")
	if err := os.WriteFile(complete, buf.Bytes(), 0o600); err != nil {
		t.Fatalf("write complete: %v", err)
	}

	// Dropping the tail is what a backup killed mid-flush leaves behind.
	truncated := filepath.Join(dir, "truncated.tar.zst")
	if err := os.WriteFile(truncated, buf.Bytes()[:buf.Len()/2], 0o600); err != nil {
		t.Fatalf("write truncated: %v", err)
	}

	if err := validateArchive(complete); err != nil {
		t.Errorf("validateArchive(complete): got %v, want nil", err)
	}

	if err := validateArchive(truncated); err == nil {
		t.Error("validateArchive(truncated): got nil, want an error")
	}
}

func TestBackupOneLeavesNoPartialFile(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "backup.tar.zst")

	// Fails whether or not podman is installed: the export either cannot start or
	// rejects the unknown volume. Either way backupOne leaves no file behind, so
	// `restore` only ever offers a complete archive.
	if err := backupOne("a-novel-test-volume-that-does-not-exist", dest); err == nil {
		t.Fatal("backupOne: got nil, want an error for a missing volume")
	}

	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Errorf("backupOne left %s behind: stat err = %v", dest, err)
	}
}

// TestResolveBackup: resolveBackup used to return the first prefix match in name
// order, so a bare timestamp restored .auto-pre-clear ahead of the manual archive
// at the same timestamp — something other than what the operator named.
func TestResolveBackup(t *testing.T) {
	const tagged, plain = "20260722-1200.auto-pre-clear.tar.zst", "20260722-1200.tar.zst"
	cases := []struct {
		name    string
		files   []string // oldest first
		from    string
		want    string
		wantErr bool
		errHas  []string
	}{
		// Same timestamp, two variants — the collision the fix exists for. The
		// message names both candidates so the operator can disambiguate.
		{name: "Error/AmbiguousPrefix", files: []string{tagged, plain}, from: "20260722-1200", wantErr: true, errHas: []string{"auto-pre-clear", "matches 2"}},
		// The plain archive's full name is not a prefix of the tagged one, so it
		// resolves uniquely — the operator's escape hatch from the ambiguity above.
		{name: "Success/FullNameDisambiguates", files: []string{tagged, plain}, from: plain, want: plain},
		{name: "Success/UniquePrefix", files: []string{plain, "20260723-0900.tar.zst"}, from: "20260722", want: plain},
		{name: "Error/NoMatch", files: []string{plain}, from: "19990101", wantErr: true},
		// An empty from picks the newest by mtime, regardless of name order.
		{name: "Success/EmptyFromPicksNewest", files: []string{"old.tar.zst", "new.tar.zst"}, want: "new.tar.zst"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			for i, name := range c.files {
				path := filepath.Join(dir, name)
				mtime := time.Now().Add(time.Duration(i-len(c.files)) * time.Hour)
				if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.Chtimes(path, mtime, mtime); err != nil {
					t.Fatal(err)
				}
			}
			got, err := resolveBackup(dir, c.from)
			if (err != nil) != c.wantErr {
				t.Fatalf("resolveBackup(%q): got (%q, %v), want error %v", c.from, got, err, c.wantErr)
			}
			for _, want := range c.errHas {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error = %q, want it to name %q", err, want)
				}
			}
			if !c.wantErr && filepath.Base(got) != c.want {
				t.Errorf("got %s, want %s", filepath.Base(got), c.want)
			}
		})
	}
}

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
	err := compressTo(errWriter{}, bytes.NewReader([]byte("volume contents")))
	if err == nil {
		t.Fatal("compressTo: got nil, want the destination's write error")
	}

	if !errors.Is(err, errDiskFull) {
		t.Errorf("compressTo: got %v, want it to wrap %v", err, errDiskFull)
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
	// `restore --previous` only ever offers a complete archive.
	if err := backupOne("a-novel-test-volume-that-does-not-exist", dest); err == nil {
		t.Fatal("backupOne: got nil, want an error for a missing volume")
	}

	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Errorf("backupOne left %s behind: stat err = %v", dest, err)
	}
}

// resolveBackup used to return the first prefix match in name order, so a bare timestamp
// restored .auto-pre-clear ahead of the manual archive at the same timestamp — something
// other than what the operator named.

func writeArchive(t *testing.T, dir, name string) {
	t.Helper()

	if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestResolveBackupAmbiguousPrefixIsAnError(t *testing.T) {
	dir := t.TempDir()
	// Same timestamp, two variants — the collision the fix exists for.
	writeArchive(t, dir, "20260722-1200.auto-pre-clear.tar.zst")
	writeArchive(t, dir, "20260722-1200.tar.zst")

	_, err := resolveBackup(dir, "20260722-1200")
	if err == nil {
		t.Fatal("resolveBackup: got nil, want an ambiguity error for a prefix matching two archives")
	}

	// The message names both candidates so the operator can disambiguate.
	if !strings.Contains(err.Error(), "auto-pre-clear") || !strings.Contains(err.Error(), "matches 2") {
		t.Errorf("error = %q, want both candidates and the count named", err)
	}
}

func TestResolveBackupFullNameDisambiguates(t *testing.T) {
	dir := t.TempDir()
	writeArchive(t, dir, "20260722-1200.auto-pre-clear.tar.zst")
	writeArchive(t, dir, "20260722-1200.tar.zst")

	// The plain archive's full name is not a prefix of the tagged one, so it resolves
	// uniquely — the operator's escape hatch from the ambiguity above.
	got, err := resolveBackup(dir, "20260722-1200.tar.zst")
	if err != nil {
		t.Fatalf("resolveBackup: %v", err)
	}

	if filepath.Base(got) != "20260722-1200.tar.zst" {
		t.Errorf("got %s, want the plain archive", filepath.Base(got))
	}
}

func TestResolveBackupUniquePrefix(t *testing.T) {
	dir := t.TempDir()
	writeArchive(t, dir, "20260722-1200.tar.zst")
	writeArchive(t, dir, "20260723-0900.tar.zst")

	got, err := resolveBackup(dir, "20260722")
	if err != nil {
		t.Fatalf("resolveBackup: %v", err)
	}

	if filepath.Base(got) != "20260722-1200.tar.zst" {
		t.Errorf("got %s, want the single match", filepath.Base(got))
	}
}

func TestResolveBackupNoMatch(t *testing.T) {
	dir := t.TempDir()
	writeArchive(t, dir, "20260722-1200.tar.zst")

	if _, err := resolveBackup(dir, "19990101"); err == nil {
		t.Fatal("resolveBackup: got nil, want a no-match error")
	}
}

func TestResolveBackupEmptyFromPicksNewest(t *testing.T) {
	dir := t.TempDir()
	writeArchive(t, dir, "old.tar.zst")
	writeArchive(t, dir, "new.tar.zst")

	// Make "new" the more recently modified regardless of name order.
	newer := time.Now()
	if err := os.Chtimes(filepath.Join(dir, "new.tar.zst"), newer, newer); err != nil {
		t.Fatal(err)
	}

	older := newer.Add(-time.Hour)
	if err := os.Chtimes(filepath.Join(dir, "old.tar.zst"), older, older); err != nil {
		t.Fatal(err)
	}

	got, err := resolveBackup(dir, "")
	if err != nil {
		t.Fatalf("resolveBackup: %v", err)
	}

	if filepath.Base(got) != "new.tar.zst" {
		t.Errorf("got %s, want the newest by mtime", filepath.Base(got))
	}
}

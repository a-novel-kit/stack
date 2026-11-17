package volumes

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/klauspost/compress/zstd"
	"github.com/stretchr/testify/require"

	"github.com/a-novel-kit/stack/cli/internal/shared/fsutil/fsutiltest"
)

// Tests for the backup write path. The archive these produce is what `clear`
// destroys a volume against, so a write that half-succeeded must not be
// reported as a backup.

var errDiskFull = errors.New("no space left on device")

// errWriter fails every write. zstd buffers, so for a small payload nothing
// reaches the destination until Close flushes the final block and the frame
// checksum. That makes this a faithful stand-in for a disk that fills at
// exactly the moment everything else has already looked fine.
type errWriter struct{}

func (errWriter) Write([]byte) (int, error) { return 0, errDiskFull }

func TestCompressTo(t *testing.T) {
	t.Parallel()

	t.Run("Success/RoundTrip", func(t *testing.T) {
		t.Parallel()

		payload := bytes.Repeat([]byte("volume contents "), 1024)
		var buf bytes.Buffer
		require.NoError(t, compressTo(&buf, bytes.NewReader(payload)))

		dec, err := zstd.NewReader(&buf)
		require.NoError(t, err)
		defer dec.Close()

		got, err := io.ReadAll(dec)
		require.NoError(t, err)
		require.Equal(t, payload, got)
	})

	t.Run("Error/FlushFailure", func(t *testing.T) {
		t.Parallel()
		require.ErrorIs(t, compressTo(errWriter{}, bytes.NewReader([]byte("volume contents"))), errDiskFull)
	})
}

func TestValidateArchive(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	require.NoError(t, compressTo(&buf, bytes.NewReader(bytes.Repeat([]byte("x"), 64*1024))))
	dir := t.TempDir()
	fsutiltest.Write(t, dir, map[string]string{
		"complete.tar.zst": buf.String(),
		// Dropping the tail is what a backup killed mid-flush leaves behind.
		"truncated.tar.zst": buf.String()[:buf.Len()/2],
	})

	t.Run("Success", func(t *testing.T) {
		t.Parallel()
		require.NoError(t, validateArchive(filepath.Join(dir, "complete.tar.zst")))
	})

	t.Run("Error/Truncated", func(t *testing.T) {
		t.Parallel()
		require.Error(t, validateArchive(filepath.Join(dir, "truncated.tar.zst")))
	})
}

func TestBackupOne(t *testing.T) {
	t.Parallel()

	dest := filepath.Join(t.TempDir(), "backup.tar.zst")

	// Fails whether or not podman is installed: the export either cannot start or
	// rejects the unknown volume. Either way backupOne leaves no file behind, so
	// `restore` only ever offers a complete archive.
	require.Error(t, backupOne("a-novel-test-volume-that-does-not-exist", dest))
	_, err := os.Stat(dest)
	require.ErrorIs(t, err, fs.ErrNotExist)
}

// TestResolveBackup pins that a timestamp matching several archives, such as a
// manual backup and its .auto-pre-clear sibling, is refused rather than resolved
// to the first by name, which would restore something the operator did not name.
func TestResolveBackup(t *testing.T) {
	t.Parallel()

	const tagged, plain = "20260722-1200.auto-pre-clear.tar.zst", "20260722-1200.tar.zst"

	testCases := []struct {
		name    string
		files   []string // oldest first
		from    string
		want    string
		wantErr bool
		errHas  []string
	}{
		// Two variants share one timestamp. The message names both candidates
		// so the operator can tell them apart.
		{name: "Error/AmbiguousPrefix", files: []string{tagged, plain}, from: "20260722-1200", wantErr: true, errHas: []string{"auto-pre-clear", "matches 2"}},
		// The plain archive's full name is not a prefix of the tagged one, so it
		// resolves uniquely: the operator's escape hatch from the ambiguity above.
		{name: "Success/FullNameDisambiguates", files: []string{tagged, plain}, from: plain, want: plain},
		{name: "Success/UniquePrefix", files: []string{plain, "20260723-0900.tar.zst"}, from: "20260722", want: plain},
		{name: "Error/NoMatch", files: []string{plain}, from: "19990101", wantErr: true},
		// An empty from picks the newest by mtime, regardless of name order.
		{name: "Success/EmptyFromPicksNewest", files: []string{"old.tar.zst", "new.tar.zst"}, want: "new.tar.zst"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			for i, name := range testCase.files {
				fsutiltest.Write(t, dir, map[string]string{name: "x"})
				mtime := time.Now().Add(time.Duration(i-len(testCase.files)) * time.Hour)
				require.NoError(t, os.Chtimes(filepath.Join(dir, name), mtime, mtime))
			}

			got, err := resolveBackup(dir, testCase.from)
			if testCase.wantErr {
				require.Error(t, err)
				for _, want := range testCase.errHas {
					require.ErrorContains(t, err, want)
				}
				return
			}
			require.NoError(t, err)
			require.Equal(t, testCase.want, filepath.Base(got))
		})
	}
}

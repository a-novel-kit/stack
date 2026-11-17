package cli

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/a-novel-kit/stack/cli/internal/shared/fsutil/fsutiltest"
	"github.com/a-novel-kit/stack/cli/internal/shared/paths"
)

// TestReadDaemonLogFrom pins that a failed start quotes only its own output: a
// daemon that fails, is fixed, and fails again is diagnosed from the new
// message, and a crash-looping one cannot bury its error under its retries.
func TestReadDaemonLogFrom(t *testing.T) {
	t.Parallel()

	const first = "Error: previous failure\n"
	testCases := []struct {
		name    string
		content string // "" leaves the log absent
		offset  int64
		want    string
	}{
		{name: "Success/FromOffset", content: first + "Error: this attempt\n", offset: int64(len(first)), want: "Error: this attempt"},
		{name: "Success/AtEnd", content: first, offset: int64(len(first))},
		// A log that was never created: the readiness error is still reportable.
		{name: "Success/Missing"},
		{name: "Success/Capped", content: strings.Repeat("x", daemonLogQuoteLimit*3), want: strings.Repeat("x", daemonLogQuoteLimit)},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			if testCase.content != "" {
				fsutiltest.Write(t, dir, map[string]string{"daemon.log": testCase.content})
			}
			require.Equal(t, testCase.want, readDaemonLogFrom(filepath.Join(dir, "daemon.log"), testCase.offset))
		})
	}
}

// TestDaemonFailureDetail pins the three suffixes a failed start can quote: the
// --foreground hint, the empty-log notice and the daemon's own words.
//
//nolint:paralleltest // t.Setenv redirects XDG_STATE_HOME, and the sub-tests share its daemon log in order.
func TestDaemonFailureDetail(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	t.Run("Success/UnopenableLogFallsBackToHint", func(t *testing.T) {
		require.Contains(t, daemonFailureDetail(errors.New("permission denied"), 0), "--foreground")
	})

	t.Run("Success/SilentDaemon", func(t *testing.T) {
		got := daemonFailureDetail(nil, 0)
		require.Contains(t, got, "wrote nothing", "want it to report an empty log")
		require.Contains(t, got, paths.DaemonLog(), "want the log path so it can be inspected")
	})

	t.Run("Success/QuotesTheDaemon", func(t *testing.T) {
		msg := "Error: discover stacks: stack swept at /tmp/gone: no such file or directory"
		fsutiltest.Write(t, filepath.Dir(paths.DaemonLog()), map[string]string{filepath.Base(paths.DaemonLog()): msg + "\n"})

		require.Contains(t, daemonFailureDetail(nil, 0), msg)
	})
}

// TestOpenDaemonLog pins the append: a start that fails is often one of
// several, and every attempt stays in the log to explain the last one. Not
// parallel: t.Setenv points XDG_STATE_HOME, where the log lives, at a temp dir.
func TestOpenDaemonLog(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	for _, attempt := range []string{"one\n", "two\n"} {
		f, err := openDaemonLog()
		require.NoError(t, err)
		_, err = f.WriteString(attempt)
		require.NoError(t, err)
		require.NoError(t, f.Close())
	}

	body, err := os.ReadFile(paths.DaemonLog())
	require.NoError(t, err)
	require.Equal(t, "one\ntwo\n", string(body), "want both attempts")
}

package cli

import (
	"errors"
	"fmt"
	"io"
	"iter"
	"time"

	"github.com/spf13/cobra"

	"github.com/a-novel-kit/stack/cli/internal/client/rpc"
	anovelv1 "github.com/a-novel-kit/stack/cli/proto/gen/anovel/v1"
)

// logsOpts holds the flags of `run logs`.
type logsOpts struct {
	scope    stackScope
	follow   bool
	tail     int32
	since    time.Duration
	stream   string
	previous bool
	runID    string
}

func newLogsCmd() *cobra.Command {
	var o logsOpts
	cmd := &cobra.Command{
		Use:   "logs <target-or-infra>",
		Short: "Print or stream logs for a target or infra container",
		Long: `Print the log of a target's current run, or of an archived run with
--previous or --run-id. --tail starts from the last N lines, and --follow keeps
printing new lines as they arrive.

An infra container's log, named '<service>/infra/<name>', comes from podman,
with stdout and stderr merged. It keeps no archived runs, so --previous and
--run-id refuse it, and --stream does not filter it.`,
		Example: `  a-novel run logs service-json-keys/rest
  a-novel run logs service-json-keys/rest --follow
  a-novel run logs service-json-keys/infra/postgres-json-keys --follow
  a-novel run logs service-json-keys/rest --tail=100 --follow
  a-novel run logs service-json-keys/rest --tail=100 --stream=stderr
  a-novel run logs service-json-keys/rest --previous`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return streamLogs(cmd, parseEntityID(args[0], o.scope.stack), o)
		},
	}
	o.scope.bind(cmd)
	cmd.Flags().BoolVarP(&o.follow, "follow", "f", false, "stream new lines as they arrive")
	cmd.Flags().Int32Var(&o.tail, "tail", 0, "start from the last N lines (0 = all)")
	cmd.Flags().DurationVar(&o.since, "since", 0, "only lines newer than this duration")
	cmd.Flags().StringVar(&o.stream, "stream", "", "filter to stdout or stderr only")
	cmd.Flags().BoolVar(&o.previous, "previous", false, "read the most recent archived run instead of the current")
	cmd.Flags().StringVar(&o.runID, "run-id", "", "read a specific archived run by timestamp")
	return cmd
}

// streamLogs prints the log of the run o selects, of the entity ref names.
func streamLogs(cmd *cobra.Command, ref entityRef, o logsOpts) error {
	// Podman owns infra log files, so infra entries have no run archives.
	if ref.IsInfra && (o.previous || o.runID != "") {
		return errors.New("--previous / --run-id are not supported for infra (podman owns its log file)")
	}
	ctx, c := cmd.Context(), rpc.New("")
	runID := o.runID
	if o.previous && runID == "" {
		runs, err := c.ListRuns(ctx, ref.ID)
		if err != nil {
			return err
		}
		if len(runs.GetRunIds()) == 0 {
			return fmt.Errorf("%s: no archived runs yet", ref.ID)
		}
		runID = runs.GetRunIds()[0]
	}
	stream, err := c.StreamLogs(ctx, ref.ID, runID, o.follow, o.tail, logStreams[o.stream])
	if err != nil {
		return err
	}
	return printLogs(cmd.OutOrStdout(), stream, o.since)
}

// logStreams maps a --stream value to the stream it keeps; any other value
// keeps both.
var logStreams = map[string]anovelv1.LogStream{
	"stdout": anovelv1.LogStream_LOG_STREAM_STDOUT,
	"stderr": anovelv1.LogStream_LOG_STREAM_STDERR,
}

// printLogs prints each line of stream as it arrives, skipping those older
// than since when it is set. The daemon applies --tail, so a followed stream
// prints without waiting for its end.
func printLogs(w io.Writer, stream iter.Seq2[*anovelv1.StreamLogsResponse, error], since time.Duration) error {
	var cutoff time.Time
	if since > 0 {
		cutoff = time.Now().Add(-since)
	}
	for ln, err := range stream {
		if err != nil {
			return err
		}
		if !cutoff.IsZero() && ln.GetTs().AsTime().Before(cutoff) {
			continue
		}
		printLogLine(w, ln)
	}
	return nil
}

// printLogLine renders one log entry as a short timestamp, a stream tag and
// the raw line, whose ANSI escapes survive so the target's own colors show.
func printLogLine(w io.Writer, ln *anovelv1.StreamLogsResponse) {
	tag := "out"
	if ln.GetStream() == anovelv1.LogStream_LOG_STREAM_STDERR {
		tag = "err"
	}
	_, _ = fmt.Fprintf(w, "%s %s %s\n",
		ln.GetTs().AsTime().Format("15:04:05.000"),
		tag,
		ln.GetLine())
}

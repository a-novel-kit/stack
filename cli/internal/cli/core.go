package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/a-novel-kit/stack/cli/internal/daemon"
	"github.com/a-novel-kit/stack/cli/internal/rpc"
	"github.com/a-novel-kit/stack/cli/internal/shared/poll"
	anovelv1 "github.com/a-novel-kit/stack/cli/proto/gen/anovel/v1"
	"github.com/a-novel-kit/stack/cli/proto/gen/anovel/v1/anovelv1connect"
)

// daemonInternalCmd is the hidden subcommand name `a-novel core start` re-execs
// itself with to switch into daemon mode. Cobra reads a leading "--" as a flag,
// so the double-underscore prefix keeps the name parseable while making an
// accidental human invocation implausible.
const daemonInternalCmd = "__daemon"

// IsDaemonReexec reports whether args (typically os.Args) invoke the hidden
// daemon re-exec, `a-novel core __daemon`. main() uses it to skip the
// version-update check in the detached daemon process, which has no user
// watching and must not phone home on shutdown.
func IsDaemonReexec(args []string) bool {
	return len(args) >= 3 && args[1] == commandCore && args[2] == daemonInternalCmd
}

// newCoreCmd groups the verbs that manage the daemon and the workspace it
// serves.
func newCoreCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   commandCore,
		Short: "Manage the daemon and the workspace",
		Long: `The daemon is the background process that runs your services: one per user,
for every registered stack. The CLI and the UI reach it over a unix socket.
'core setup' makes each new shell run 'core start', which does nothing while
the daemon runs.`,
	}
	cmd.AddCommand(newCoreStartCmd())
	cmd.AddCommand(newCoreSetupCmd())
	cmd.AddCommand(newCoreSyncCmd())
	cmd.AddCommand(newCoreStacksCmd())
	cmd.AddCommand(newCoreBotCommentCmd())
	cmd.AddCommand(newCoreKillCmd())
	cmd.AddCommand(newCoreRestartCmd())
	cmd.AddCommand(newCoreStatusCmd())
	cmd.AddCommand(newCorePrepareReinstallCmd())
	cmd.AddCommand(newCoreDaemonInternalCmd())
	return cmd
}

// daemonUp pings the daemon briefly. A daemon that is not listening is down,
// not an error; any other failure is returned.
func daemonUp(ctx context.Context, c anovelv1connect.CoreServiceClient) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()
	_, err := rpc.Call(ctx, c.Ping, &anovelv1.PingRequest{})
	if rpc.IsNotRunning(err) {
		return false, nil
	}
	return err == nil, err
}

// stopDaemon stops a running daemon and waits until it is gone, so a script can
// run `core start` on the next line. checkpoint has the daemon checkpoint its
// go-exec targets first, for the next daemon to relaunch. Otherwise the daemon
// shuts down, and force also tears down every service's infra. It reports
// false when no daemon answered.
//
// A ping failure, a daemon slow to exit and a target the shutdown could not
// stop all fail it, so a caller never builds on a daemon that may still run.
func stopDaemon(ctx context.Context, c anovelv1connect.CoreServiceClient, out io.Writer, checkpoint, force bool) (bool, error) {
	if up, err := daemonUp(ctx, c); !up {
		return false, err
	}

	var failures []string
	if checkpoint {
		if err := prepareReinstall(ctx, c, out); err != nil {
			return true, err
		}
	} else {
		resp, err := shutdown(ctx, c, force, out)
		if err != nil {
			return true, err
		}
		failures = resp.GetFailures()
	}

	// The daemon exits even after a failure, so it is waited on either way.
	waitErr := waitDaemonGone(ctx, c, out)
	if len(failures) > 0 {
		return true, fmt.Errorf("shutdown could not stop %d target(s): %s", len(failures), strings.Join(failures, ", "))
	}
	return true, waitErr
}

// daemonExitTimeout bounds how long a stopped daemon may take to release its
// socket.
const daemonExitTimeout = 10 * time.Second

// waitDaemonGone polls until the daemon stops answering, for up to
// daemonExitTimeout.
func waitDaemonGone(ctx context.Context, c anovelv1connect.CoreServiceClient, out io.Writer) error {
	err := poll.Until(ctx, 100*time.Millisecond, daemonExitTimeout, func(ctx context.Context) (bool, error) {
		up, err := daemonUp(ctx, c)
		return !up && err == nil, nil
	})
	if errors.Is(err, poll.ErrTimeout) {
		return fmt.Errorf("daemon didn't shut down within %s", daemonExitTimeout)
	}
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintln(out, "daemon stopped.")
	return nil
}

// shutdown asks the daemon to exit and reports what it stopped. force also
// tears down every service's infra.
func shutdown(ctx context.Context, c anovelv1connect.CoreServiceClient, force bool, out io.Writer) (*anovelv1.ShutdownResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	resp, err := rpc.Call(ctx, c.Shutdown, &anovelv1.ShutdownRequest{Force: force})
	if err != nil {
		return nil, err
	}
	if force {
		_, _ = fmt.Fprintf(out, "shutdown: %d go-exec target(s) killed, %d service infra torn down\n",
			resp.GetGoExecKilled(), resp.GetInfraServicesTornDown())
	} else {
		_, _ = fmt.Fprintf(out, "shutdown: %d go-exec target(s) killed (containers left running)\n",
			resp.GetGoExecKilled())
	}
	return resp, nil
}

// prepareReinstall asks the daemon to checkpoint its go-exec targets and exit.
func prepareReinstall(ctx context.Context, c anovelv1connect.CoreServiceClient, out io.Writer) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	resp, err := rpc.Call(ctx, c.PrepareReinstall, &anovelv1.PrepareReinstallRequest{})
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(out, "checkpoint written: %s (%d go-exec target(s))\n",
		resp.GetCheckpointPath(), resp.GetGoExecTargetCount())
	return nil
}

// newCoreDaemonInternalCmd is the hidden subcommand 'core start' re-execs the
// binary with, to run the daemon in a detached child.
func newCoreDaemonInternalCmd() *cobra.Command {
	return &cobra.Command{
		Use:    daemonInternalCmd,
		Hidden: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return daemon.Run(cmd.Context())
		},
	}
}

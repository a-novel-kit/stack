package cli

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/spf13/cobra"

	"github.com/a-novel-kit/stack/cli/internal/daemon"
	"github.com/a-novel-kit/stack/cli/internal/rpc"
	"github.com/a-novel-kit/stack/cli/internal/version"
	anovelv1 "github.com/a-novel-kit/stack/cli/proto/gen/anovel/v1"
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
func daemonUp(ctx context.Context, c *rpc.Client) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()
	_, err := c.Ping(ctx)
	if rpc.IsNotRunning(err) {
		return false, nil
	}
	return err == nil, err
}

// daemonExitTimeout bounds how long a stopped daemon may take to release its
// socket.
const daemonExitTimeout = 10 * time.Second

// waitDaemonGone polls until the daemon stops answering, for up to
// daemonExitTimeout. A script can then run `core start` on the next line.
func waitDaemonGone(ctx context.Context, c *rpc.Client, out io.Writer) error {
	deadline := time.Now().Add(daemonExitTimeout)
	for time.Now().Before(deadline) {
		if up, err := daemonUp(ctx, c); !up && err == nil {
			_, _ = fmt.Fprintln(out, "daemon stopped.")
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("daemon didn't shut down within %s", daemonExitTimeout)
}

// shutdown asks the daemon to exit and reports what it stopped. force also
// tears down every service's infra.
func shutdown(ctx context.Context, c *rpc.Client, force bool, out io.Writer) (*anovelv1.ShutdownResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	resp, err := c.Shutdown(ctx, force)
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

// newCoreDaemonInternalCmd is the hidden subcommand 'core start' re-execs the
// binary with, to run the daemon in a detached child.
func newCoreDaemonInternalCmd() *cobra.Command {
	return &cobra.Command{
		Use:    daemonInternalCmd,
		Hidden: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return daemon.Run(cmd.Context(), daemon.Options{Version: version.String()})
		},
	}
}

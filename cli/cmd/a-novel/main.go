// Command a-novel is the A-Novel storyverse build tool: a single CLI for local
// development. It runs standalone capabilities such as test and build, and
// fronts a long-lived background daemon that supervises run targets.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/a-novel-kit/stack/cli/internal/cli"
	"github.com/a-novel-kit/stack/cli/internal/update"
	"github.com/a-novel-kit/stack/cli/internal/version"
)

func main() {
	// Pin the compose provider for every `podman compose` this process tree
	// runs: `podman compose` prefers docker-compose when installed, which drags
	// in the Docker-compatible socket. The daemon re-exec runs main() too, and
	// every compose call builds its env from os.Environ. The second variable
	// keeps the provider banner out of captured compose output.
	_ = os.Setenv("PODMAN_COMPOSE_PROVIDER", "podman-compose")
	_ = os.Setenv("PODMAN_COMPOSE_WARNING_LOGS", "false")

	args, sandbox, err := cli.SandboxArgs(os.Args[1:])
	switch {
	case err != nil:
		err = &cli.ExitError{Code: 2, Err: err}
	case sandbox:
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		err = cli.RunSandbox(ctx, args, os.Stdin, os.Stdout, os.Stderr)
		stop()
	default:
		err = cli.NewRoot().ExecuteContext(context.Background())
		// Best-effort "newer release available" notice, on stderr so it never
		// corrupts parsed or eval'd stdout. The detached daemon has no user to
		// notify.
		if !cli.IsDaemonReexec(os.Args) {
			update.Notify(os.Stderr, version.String())
		}
	}
	os.Exit(exitCode(err))
}

// exitCode prints err and maps it to the process exit status.
func exitCode(err error) int {
	if err == nil {
		return 0
	}
	code := 1
	var exitErr *cli.ExitError
	if errors.As(err, &exitErr) {
		code = exitErr.Code
		if exitErr.Err == nil {
			return code
		}
	}
	fmt.Fprintln(os.Stderr, "Error:", err)
	return code
}

package cli

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/a-novel-kit/stack/cli/internal/client/rpc"
	anovelv1 "github.com/a-novel-kit/stack/cli/proto/gen/anovel/v1"
)

func newExecCmd() *cobra.Command {
	var ss stackScope
	cmd := &cobra.Command{
		Use:   "exec <target> -- <cmd> [args...]",
		Short: "Run a command alongside a target (container exec, or sibling process)",
		Long: `Run a command beside a target. In a running container target, the command
runs inside the container through 'podman exec'. Otherwise it runs on the host,
in the service's directory with the target's environment, so
'run exec <service>/migrations -- psql' opens psql on the service's database.
The command's exit status becomes the CLI's.`,
		Example: `  a-novel run exec service-json-keys/rest -- sh
  a-novel run exec service-json-keys/migrations -- psql
  a-novel run exec default/service-json-keys/rest -- curl -s localhost:8080`,
		Args: cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			// Cobra strips "--", so args[0] is the target and args[1:] the command.
			return execBeside(cmd, parseEntityID(args[0], ss.stack), args[1:])
		},
	}
	ss.bind(cmd)
	return cmd
}

// execBeside runs argv beside the target ref names and prints its output,
// stderr lines on stderr. An infra container is refused: podman reaches it
// directly.
func execBeside(cmd *cobra.Command, ref entityRef, argv []string) error {
	if ref.IsInfra {
		return fmt.Errorf("exec: %s is an infra container; use 'podman exec %s_%s_%s_1 …' directly",
			ref.ID, ref.Stack, ref.Service, ref.Infra)
	}
	ctx := cmd.Context()
	stream, err := rpc.New("").Exec(ctx, ref.ID, argv)
	if err != nil {
		return err
	}
	// A pointer, so an unreported exit status stays distinct from a reported 0.
	var exitCode *int32
	for ev, err := range stream {
		if err != nil {
			if ctx.Err() == nil {
				return err
			}
			break
		}
		if ev.ExitCode != nil {
			exitCode = ev.ExitCode
			continue
		}
		w := cmd.OutOrStdout()
		if ev.GetStream() == anovelv1.LogStream_LOG_STREAM_STDERR {
			w = cmd.ErrOrStderr()
		}
		_, _ = fmt.Fprintln(w, ev.GetLine())
	}
	return execResult(exitCode)
}

// execResult turns the exec stream's terminal exit code into the CLI's own
// exit status. A nil exitCode means the daemon ended the stream without
// reporting one. That says nothing about the child, so it yields an error.
func execResult(exitCode *int32) error {
	if exitCode == nil {
		return errors.New("exec: daemon closed the stream without reporting an exit status " +
			"(a daemon older than this CLI does not send one — run 'a-novel core restart')")
	}

	if *exitCode != 0 {
		return &ExitError{Code: int(*exitCode)}
	}

	return nil
}

func newDebugCmd() *cobra.Command {
	var ss stackScope
	cmd := &cobra.Command{
		Use:   "debug <target>",
		Short: "Print the dlv attach command for a running go-exec target",
		Long: `Print the 'dlv attach' command for a running go-exec target, with its PID and a
headless port. Run it in your own terminal, then connect your IDE to that
port. Container targets are refused.`,
		Example: `  a-novel run debug service-json-keys/grpc
  a-novel run debug default/service-json-keys/grpc`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return printDebugHint(cmd, parseEntityID(args[0], ss.stack))
		},
	}
	ss.bind(cmd)
	return cmd
}

// printDebugHint prints the daemon's dlv attach command for the target ref
// names.
func printDebugHint(cmd *cobra.Command, ref entityRef) error {
	if ref.IsInfra {
		return fmt.Errorf("debug: %s is an infra container; dlv attach is only meaningful on go-exec targets", ref.ID)
	}
	resp, err := rpc.New("").Debug(cmd.Context(), ref.ID)
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintln(cmd.OutOrStdout(), resp.GetHint())
	return nil
}

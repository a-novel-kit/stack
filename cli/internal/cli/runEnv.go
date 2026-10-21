package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/a-novel-kit/stack/cli/internal/client/rpc"
	anovelv1 "github.com/a-novel-kit/stack/cli/proto/gen/anovel/v1"
)

// envOpts holds the flags of `run env`.
type envOpts struct {
	scope  stackScope
	only   string
	format string
}

func newEnvCmd() *cobra.Command {
	var o envOpts
	cmd := &cobra.Command{
		Use:   "env [<service>]",
		Short: "Print a service's env vars in shell / JSON / dotenv form",
		Long: `Print the environment the daemon gives a service's targets: the compose
constants, the ports it allocates, and the URLs and DSNs it derives from them.
A port not allocated yet prints empty. Without <service>, print every service
of the stack, or of every stack with --all-stacks.

The default shell format is ready for eval; --format=json and --format=dotenv
suit scripts and .env files.`,
		Example: `  a-novel run env service-json-keys
  eval "$(a-novel run env service-json-keys)"
  curl "$REST_URL/ping"

  a-novel run env service-json-keys --only='REST*'
  a-novel run env --format=json
  a-novel run env --all-stacks`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return printEnv(cmd, strings.Join(args, ""), o)
		},
	}
	o.scope.bindAll(cmd)
	cmd.Flags().StringVar(&o.only, "only", "", "glob filter on key names (e.g. 'REST*')")
	cmd.Flags().StringVar(&o.format, "format", "shell", "output format (shell | json | dotenv)")
	return cmd
}

// printEnv prints the env of service, or of every service in scope when it is
// empty, keeping the keys --only matches.
func printEnv(cmd *cobra.Command, service string, o envOpts) error {
	resp, err := rpc.New("").GetEnv(cmd.Context(), o.scope.stack, service, o.scope.allStacks)
	if err != nil {
		return err
	}
	// --only filters client-side, so it applies to every format.
	unmatched := func(e *anovelv1.EnvEntry) bool { return !matchGlob(o.only, e.GetKey()) }
	renderEnv(cmd.OutOrStdout(), slices.DeleteFunc(resp.GetEntries(), unmatched), o.format)
	return nil
}

// renderEnv writes the env entries in the requested format. shell is
// eval-able; json is one-record-per-entry; dotenv is plain KEY=VALUE.
func renderEnv(w io.Writer, entries []*anovelv1.EnvEntry, format string) {
	switch format {
	case "json":
		enc := json.NewEncoder(w)
		for _, e := range entries {
			_ = enc.Encode(e)
		}
	case "dotenv":
		for _, e := range entries {
			_, _ = fmt.Fprintf(w, "%s=%s\n", e.GetKey(), e.GetValue())
		}
	default: // "shell"
		var lastSvc string
		for _, e := range entries {
			svc := e.GetService()
			if e.GetStack() != "" {
				svc = e.GetStack() + "/" + svc
			}
			if svc != lastSvc {
				if lastSvc != "" {
					_, _ = fmt.Fprintln(w)
				}
				_, _ = fmt.Fprintf(w, "# %s\n", svc)
				lastSvc = svc
			}
			// Single-quote the value, escaping embedded single quotes with
			// the standard '"'"' trick, so the line is safe to eval.
			esc := strings.ReplaceAll(e.GetValue(), "'", `'"'"'`)
			_, _ = fmt.Fprintf(w, "export %s='%s'\n", e.GetKey(), esc)
		}
	}
}

// matchGlob reports whether an env key matches the --only pattern, a
// filepath.Match glob such as "REST*" or "*_PORT"; an empty pattern matches
// every key. A malformed pattern falls back to substring matching, so a typo
// still does something reasonable.
func matchGlob(pattern, name string) bool {
	if pattern == "" {
		return true
	}
	if ok, err := filepath.Match(pattern, name); err == nil {
		return ok
	}
	return strings.Contains(name, pattern)
}

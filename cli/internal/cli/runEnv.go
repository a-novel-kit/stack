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
			var service string
			if len(args) == 1 {
				service = args[0]
			}
			return printEnv(cmd, service, o)
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
// eval-able, json is one record per entry, and dotenv is plain KEY=VALUE.
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
		renderShellEnv(w, entries)
	}
}

// renderShellEnv prints entries as eval-able export lines, under a comment
// header per service.
func renderShellEnv(w io.Writer, entries []*anovelv1.EnvEntry) {
	var lastGroup string
	for _, e := range entries {
		group := e.GetService()
		if e.GetStack() != "" {
			group = e.GetStack() + "/" + group
		}

		if group != lastGroup {
			if lastGroup != "" {
				_, _ = fmt.Fprintln(w)
			}
			_, _ = fmt.Fprintf(w, "# %s\n", group)
			lastGroup = group
		}

		_, _ = fmt.Fprintf(w, "export %s=%s\n", e.GetKey(), shellQuote(e.GetValue()))
	}
}

// shellQuote single-quotes v so the line is safe to eval. Each embedded single
// quote becomes the standard '"'"' sequence.
func shellQuote(v string) string {
	return "'" + strings.ReplaceAll(v, "'", `'"'"'`) + "'"
}

// matchGlob reports whether an env key matches the --only pattern, a
// filepath.Match glob such as "REST*" or "*_PORT". An empty pattern matches
// every key. A malformed pattern falls back to a substring match, so a typo
// still filters.
func matchGlob(pattern, name string) bool {
	if pattern == "" {
		return true
	}
	if ok, err := filepath.Match(pattern, name); err == nil {
		return ok
	}
	return strings.Contains(name, pattern)
}

package cli

import (
	"strings"

	"github.com/spf13/cobra"

	"github.com/a-novel-kit/stack/cli/internal/shared/stacks"
	"github.com/a-novel-kit/stack/cli/internal/tui"
	anovelv1 "github.com/a-novel-kit/stack/cli/proto/gen/anovel/v1"
)

const (
	// modeGoExec is the --mode token and the label of go-exec mode.
	modeGoExec = "go-exec"
	// modeContainer is the --mode token and the label of container mode.
	modeContainer = "container"
	// kindTarget selects targets in the --kind filter.
	kindTarget = "target"
	// kindInfra is the word for infra containers, in flags, output and entity
	// IDs.
	kindInfra = "infra"
	// cmdNameService is the name of the `run service` command.
	cmdNameService = "service"
)

// stackScope holds the flag values shared across the daemon-backed commands.
type stackScope struct {
	stack     string
	allStacks bool
}

// bind adds the --stack flag to cmd.
func (s *stackScope) bind(cmd *cobra.Command) {
	cmd.Flags().StringVar(&s.stack, stackLabel, "", "stack name (default: first in $A_NOVEL_STACKS)")
}

// bindAll adds --stack and --all-stacks to cmd.
func (s *stackScope) bindAll(cmd *cobra.Command) {
	s.bind(cmd)
	cmd.Flags().BoolVar(&s.allStacks, "all-stacks", false, "operate across every registered stack")
}

// selector is the stack argument of a listing call: "*" with --all-stacks.
func (s *stackScope) selector() string {
	if s.allStacks {
		return "*"
	}
	return s.stack
}

// newRunCmd is the parent of every daemon-backed verb. Each verb is a thin
// client of the daemon's RPC holding no supervision state: the daemon owns the
// running targets, their phases and their logs.
func newRunCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   commandRun,
		Short: "Operate on services and targets via the a-novel daemon",
		Long: `Every verb under 'run' talks to the long-lived a-novel daemon over its unix
socket, and refuses with a clear error when it is not running (see 'a-novel
core start'). Daemon control itself lives under 'a-novel core'.`,
	}
	cmd.AddCommand(
		newUICmd(),
		newPsCmd(),
		newTopologyCmd(),
		newServiceCmd(),
		newStartCmd(),
		newKillCmd(),
		newRestartCmd(),
		newLogsCmd(),
		newEnvCmd(),
		newWatchCmd(),
		newVolumeCmd(),
		newExecCmd(),
		newDebugCmd(),
	)
	return cmd
}

func newUICmd() *cobra.Command {
	return &cobra.Command{
		Use:   "ui",
		Short: "Open the terminal UI",
		Long: `Open the full-screen view of every service: the service list, each service's
targets and infra as tabs, and the selected tab's live log. Esc opens the
command palette, ? lists every key, and q quits without stopping anything.
The UI talks to the same daemon as the CLI, so each sees what the other
starts.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return tui.Run()
		},
	}
}

// entityRef is a parsed CLI argument naming a target or an infra container.
// A literal "infra" segment tells the two apart, because a 3-segment target ID
// and a 3-segment infra shorthand have the same slash count.
type entityRef struct {
	IsInfra bool
	Stack   string
	Service string
	Target  string // set when IsInfra is false
	Infra   string // set when IsInfra is true
	// ID is the canonical full ID: "<stack>/<service>/<target>" for a
	// target, "<stack>/<service>/infra/<name>" for an infra container.
	ID string
}

// parseEntityID classifies a CLI argument as a target or infra
// reference and returns its canonical form. Recognized shapes:
//
//	<service>/<target>                      → target shorthand
//	<stack>/<service>/<target>              → target canonical
//	<service>/infra/<name>                  → infra shorthand
//	<stack>/<service>/infra/<name>          → infra canonical
//
// Only "infra" in the second-to-last segment marks an infra reference.
// Anywhere else the word is an ordinary target name. A shorthand resolves
// against defaultStack. An empty defaultStack means the first $A_NOVEL_STACKS
// entry.
func parseEntityID(arg, defaultStack string) entityRef {
	if defaultStack == "" {
		defaultStack = stacks.DefaultName
		if stack, err := stacks.Default(); err == nil {
			defaultStack = stack.Name
		}
	}
	parts := strings.Split(arg, "/")
	// Infra: 3 segments (svc/infra/name) or 4 (stack/svc/infra/name).
	if len(parts) >= 3 && parts[len(parts)-2] == kindInfra {
		ref := entityRef{IsInfra: true, Infra: parts[len(parts)-1]}
		if len(parts) == 4 {
			ref.Stack = parts[0]
			ref.Service = parts[1]
		} else {
			ref.Stack = defaultStack
			ref.Service = parts[0]
		}
		ref.ID = ref.Stack + "/" + ref.Service + "/infra/" + ref.Infra
		return ref
	}
	// Target.
	ref := entityRef{}
	switch len(parts) {
	case 3:
		ref.Stack = parts[0]
		ref.Service = parts[1]
		ref.Target = parts[2]
	case 2:
		ref.Stack = defaultStack
		ref.Service = parts[0]
		ref.Target = parts[1]
	default:
		// Any other shape becomes "<stack>//<arg>", which the daemon refuses
		// as an unknown target.
		ref.Stack = defaultStack
		ref.Target = arg
	}
	ref.ID = ref.Stack + "/" + ref.Service + "/" + ref.Target
	return ref
}

// parseMode reads a --mode token. An empty or unknown token yields fallback.
func parseMode(token string, fallback anovelv1.Mode) anovelv1.Mode {
	switch token {
	case modeGoExec:
		return anovelv1.Mode_MODE_GO_EXEC
	case modeContainer:
		return anovelv1.Mode_MODE_CONTAINER
	default:
		return fallback
	}
}

// modeLabel renders a Mode enum as its canonical CLI token.
func modeLabel(m anovelv1.Mode) string {
	switch m {
	case anovelv1.Mode_MODE_GO_EXEC:
		return modeGoExec
	case anovelv1.Mode_MODE_CONTAINER:
		return modeContainer
	default:
		return "?"
	}
}

// exitLabel renders an ExitReason enum as a short human-readable token.
func exitLabel(r anovelv1.ExitReason) string {
	switch r {
	case anovelv1.ExitReason_EXIT_REASON_SUCCESS:
		return "success"
	case anovelv1.ExitReason_EXIT_REASON_ERROR:
		return "error"
	case anovelv1.ExitReason_EXIT_REASON_KILLED:
		return "killed"
	case anovelv1.ExitReason_EXIT_REASON_CRASHED:
		return "crashed"
	default:
		return "-"
	}
}

// phaseLabel renders a Phase enum as a short human-readable token; an
// unspecified phase reads as "idle".
func phaseLabel(p anovelv1.Phase) string {
	switch p {
	case anovelv1.Phase_PHASE_PENDING:
		return "pending"
	case anovelv1.Phase_PHASE_STARTING:
		return "starting"
	case anovelv1.Phase_PHASE_RUNNING:
		return "running"
	case anovelv1.Phase_PHASE_STOPPING:
		return "stopping"
	case anovelv1.Phase_PHASE_TERMINATED:
		return "terminated"
	default:
		return "idle"
	}
}

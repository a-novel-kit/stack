package cli

import (
	"strings"

	"github.com/spf13/cobra"

	"github.com/a-novel-kit/stack/cli/internal/shared/enumword"
	anovelv1 "github.com/a-novel-kit/stack/cli/proto/gen/anovel/v1"
)

const (
	// kindTarget selects targets in the --kind filter.
	kindTarget = "target"
	// kindInfra is the word for infra containers, in flags, output and entity
	// IDs.
	kindInfra = "infra"
	// cmdNameService is the name of the `run service` command.
	cmdNameService = "service"
)

// modeWords are the words --mode and --one-shots take.
var modeWords = []string{enumword.Word(anovelv1.Mode_MODE_GO_EXEC), enumword.Word(anovelv1.Mode_MODE_CONTAINER)}

// addStackFlag defines --stack on cmd, into p. An empty stack is the daemon's
// default one.
func addStackFlag(cmd *cobra.Command, p *string) {
	cmd.Flags().StringVar(p, stackLabel, "", "stack name (default: the daemon's default stack)")
}

// stackScope holds the --stack and --all-stacks flags of a listing command.
type stackScope struct {
	stack     string
	allStacks bool
}

// bind adds --stack and --all-stacks to cmd.
func (s *stackScope) bind(cmd *cobra.Command) {
	addStackFlag(cmd, &s.stack)
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

// entityRef is a parsed CLI argument naming a target or an infra container.
// A literal "infra" segment tells the two apart, because a 3-segment target ID
// and a 3-segment infra shorthand have the same slash count.
type entityRef struct {
	// ID is the argument, with the --stack value in front of a shorthand. The
	// daemon resolves a shorthand left as it is against its default stack.
	ID      string
	IsInfra bool
	// Stack, Service and Infra are the parts of an infra ID. Stack is empty
	// for a shorthand the daemon resolves.
	Stack, Service, Infra string
}

// parseEntityID classifies a CLI argument as a target or infra reference.
// Recognized shapes:
//
//	<service>/<target>                      → target shorthand
//	<stack>/<service>/<target>              → target canonical
//	<service>/infra/<name>                  → infra shorthand
//	<stack>/<service>/infra/<name>          → infra canonical
//
// Only "infra" in the second-to-last segment marks an infra reference.
// Anywhere else the word is an ordinary target name. A non-empty stack goes in
// front of a shorthand.
func parseEntityID(arg, stack string) entityRef {
	parts := strings.Split(arg, "/")
	isInfra := (len(parts) == 3 || len(parts) == 4) && parts[len(parts)-2] == kindInfra
	isShorthand := len(parts) == 2 || isInfra && len(parts) == 3
	if isShorthand && stack != "" {
		parts = append([]string{stack}, parts...)
	}
	ref := entityRef{ID: strings.Join(parts, "/"), IsInfra: isInfra}
	if isInfra {
		ref.Service, ref.Infra = parts[len(parts)-3], parts[len(parts)-1]
		if len(parts) == 4 {
			ref.Stack = parts[0]
		}
	}
	return ref
}

// infraID is the ID the CLI prints for an infra container the daemon
// answered with.
func infraID(in *anovelv1.Infra) string {
	return in.GetStack() + "/" + in.GetService() + "/" + kindInfra + "/" + in.GetName()
}

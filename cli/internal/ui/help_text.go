package ui

// All user-facing help, usage, and command-description text lives here so it
// can be edited without touching the rendering logic in help.go.

// flagDoc is one flag's name and description, as shown in per-command help.
type flagDoc struct{ name, desc string }

// commandDoc describes a subcommand: a one-line summary for the generic
// command list, plus the usage / long description / flags / examples shown by
// per-command help (`a-novel <command> --help`).
type commandDoc struct {
	name     string
	summary  string
	usage    string
	long     string
	flags    []flagDoc
	examples []string
}

// rootUsage is the synopsis on the generic help screen.
const rootUsage = "a-novel <command> [flags]"

// helpHint tells the user how to drill into a single command.
const helpHint = "Run `a-novel <command> --help` (or `a-novel help <command>`) " +
	"for a command's flags and examples."

// Flag docs are composed per command from these fragments — each command
// advertises only the flags it actually honors, so `run --help` never lists
// the test-only --no-cover and `build`/`test` never list the run-only
// --recreate.
var (
	flagDir      = flagDoc{"-C, --dir <path>", "Directory to scan (default: current directory)"}
	flagType     = flagDoc{"-t, --type <kinds>", "Comma-separated kind filter: go,pnpm,podman (default: all)"}
	flagTypeTest = flagDoc{"-t, --type <kinds>", "Comma-separated kind filter: go,pnpm (default: all)"}
	flagJobs     = flagDoc{"-j, --jobs <n>", "Max targets run in parallel, interactive only (default: NumCPU/4, min 2)"}
	flagTimeout  = flagDoc{"-T, --timeout <dur>", "Per-target deadline, e.g. 10m / 30s / 0 to disable (default: 10m)"}
	flagYes      = flagDoc{"-y, --yes", "Skip the menu; run everything non-interactively & sequentially (CI-safe)"}
	flagNoCover  = flagDoc{"--no-cover", "Skip coverage (it is collected & reported by default)"}
	flagKeep     = flagDoc{"--keep", "Leave the test env up afterwards; the next run reuses it (skips postgres init)"}
	flagDryRun   = flagDoc{"--dry-run", "List detected targets (and their envs) and exit without running"}
	flagHelp     = flagDoc{"-h, --help", "Show this command's help"}
)

// buildFlags / testFlags are the exact, honored flag sets. `test` omits the
// podman kind (there are no podman test targets) and adds --no-cover.
var (
	buildFlags = []flagDoc{flagDir, flagType, flagJobs, flagTimeout, flagYes, flagDryRun, flagHelp}
	testFlags  = []flagDoc{flagDir, flagTypeTest, flagJobs, flagTimeout, flagYes, flagNoCover, flagKeep, flagDryRun, flagHelp}
)

// commandDocs holds the banner-style help for the two standalone capability
// commands (`build`, `test`) — the only commands that route through
// CommandHelpView. Every other command (core, run, publish, install, version)
// is a Cobra command and renders Cobra's own help; do not add them here.
var commandDocs = []commandDoc{
	{
		name:    "build",
		summary: "Build Go modules, pnpm scripts and Podman images",
		usage:   "a-novel build [flags]",
		long: "Recursively discovers build targets under the working directory — a " +
			"Go module per go.mod, every pnpm \"build\"/\"build:*\" script, a root " +
			"Dockerfile, and each builds/*.Dockerfile — then lets you pick what to build " +
			"menu (everything selected by default). Selected targets build in a bounded " +
			"parallel pool and a pass/fail report is printed. With -y or no TTY it runs " +
			"sequentially and non-interactively.",
		flags: buildFlags,
		examples: []string{
			"a-novel build                 # interactive build menu",
			"a-novel build -t go,podman    # only Go + Podman targets",
			"a-novel build -j 4            # cap parallelism at 4",
			"a-novel build -y              # build everything, no prompt (CI-safe)",
			"a-novel build --dry-run       # show what would build, run nothing",
		},
	},
	{
		name:    "test",
		summary: "Run Go / pnpm tests, spinning up podman-compose envs",
		usage:   "a-novel test [flags]",
		long: "Like build, for tests: `go test` per module (scoped per " +
			"builds/podman-compose.go[.<path>].test.yaml when present, else ./...), and " +
			"every pnpm \"test\"/\"test:*\" script. A target with a matching " +
			"podman-compose.<id>.test.yaml env has it brought up (host ports allocated " +
			"in Go, healthy-waited) around the run and torn down after — parallel-safe, " +
			"so independent envs run concurrently.",
		flags: testFlags,
		examples: []string{
			"a-novel test                  # interactive test menu",
			"a-novel test -t go            # only Go test targets",
			"a-novel test -y               # run all tests, no prompt (CI-safe)",
			"a-novel test --dry-run        # show targets + their envs, run nothing",
		},
	},
}

// lookupCommand returns the doc for name, or (zero,false) if unknown.
func lookupCommand(name string) (commandDoc, bool) {
	for _, c := range commandDocs {
		if c.name == name {
			return c, true
		}
	}
	return commandDoc{}, false
}

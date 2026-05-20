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
// advertises only the flags it actually honours, so `run --help` never lists
// the test-only --no-cover and `build`/`test` never list the run-only
// --recreate (the "wrong flag in help" bug).
var (
	flagDir      = flagDoc{"-C, --dir <path>", "Directory to scan (default: current directory)"}
	flagType     = flagDoc{"-t, --type <kinds>", "Comma-separated kind filter: go,pnpm,podman (default: all)"}
	flagJobs     = flagDoc{"-j, --jobs <n>", "Max targets run in parallel, interactive only (default: CPU count)"}
	flagTimeout  = flagDoc{"-T, --timeout <dur>", "Per-target deadline, e.g. 10m / 30s / 0 to disable (default: 10m)"}
	flagYes      = flagDoc{"-y, --yes", "Skip the menu; run everything non-interactively & sequentially (CI-safe)"}
	flagNoCover  = flagDoc{"--no-cover", "Skip coverage (it is collected & reported by default)"}
	flagRecreate = flagDoc{"--recreate", "Rebuild the compose env instead of reusing an existing one"}
	flagDocker   = flagDoc{"--docker", "Run each target as its compose service (default per-repo; targets without a compose service stay local)"}
	flagLocal    = flagDoc{"--local", "Run each target as a local exec (go run/pnpm run) — default in global mode"}
	flagDryRun   = flagDoc{"--dry-run", "List detected targets (and their envs) and exit without running"}
	flagHelp     = flagDoc{"-h, --help", "Show this command's help"}
)

// buildFlags / testFlags / runCmdFlags are the exact, honoured flag sets.
// `run` deliberately omits -j/-T/-y/--no-cover: run targets are long-lived
// (no per-target timeout or parallel pool) and selection is its own step.
var (
	buildFlags  = []flagDoc{flagDir, flagType, flagJobs, flagTimeout, flagYes, flagDryRun, flagHelp}
	testFlags   = []flagDoc{flagDir, flagType, flagJobs, flagTimeout, flagYes, flagNoCover, flagDryRun, flagHelp}
	runCmdFlags = []flagDoc{flagDir, flagType, flagDocker, flagLocal, flagRecreate, flagDryRun, flagHelp}
)

// commandDocs is the single source of truth for the command set; its order is
// the order shown on the generic help screen.
var commandDocs = []commandDoc{
	{
		name:    "build",
		summary: "Build Go modules, pnpm scripts and Podman images",
		usage:   "a-novel build [flags]",
		long: "Recursively discovers build targets under the working directory — a " +
			"Go module per go.mod, every pnpm \"build\"/\"build:*\" script, and each " +
			"builds/*.Dockerfile — then lets you pick what to build in an interactive " +
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
	{
		name:    "run",
		summary: "Run Go entrypoints / pnpm run* scripts under a live dashboard",
		usage:   "a-novel run [flags]",
		long: "Discovers Go `package main` entrypoints (cmd/…) and pnpm " +
			"\"run\"/\"run:*\" scripts, brings each one's compose env up once " +
			"(builds/podman-compose.<id>.yaml, else the plain builds/" +
			"podman-compose.yaml), then launches the selected targets as " +
			"long-lived processes behind a live status dashboard (↑/↓ to select, " +
			"tab to scroll a process's log). Quitting, or any process failing, " +
			"tears the whole environment down — no phantom runners. An already-up " +
			"env is reused by default; --recreate (or the prompt) rebuilds it.",
		flags: runCmdFlags,
		examples: []string{
			"a-novel run                   # interactive run menu + dashboard",
			"a-novel run -t go             # only Go entrypoints",
			"a-novel run --recreate        # rebuild the env instead of reusing it",
			"a-novel run --dry-run         # show entrypoints + their envs",
		},
	},
	{
		name:    "version",
		summary: "Print the resolved CLI version and exit",
		usage:   "a-novel version",
		long:    "Prints the resolved CLI version (ldflags → go install tag → VCS revision → \"dev\") and exits.",
	},
	{
		name:    "help",
		summary: "Show help; `a-novel help <command>` for one command",
		usage:   "a-novel help [command]",
		long: "With no argument, lists the available commands. With a command name, " +
			"shows that command's usage, flags and examples — equivalently to " +
			"`a-novel <command> --help`.",
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

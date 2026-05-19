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

// runFlags is the flag set shared verbatim by `build` and `test`.
var runFlags = []flagDoc{
	{"-C, --dir <path>", "Directory to scan (default: current directory)"},
	{"-t, --type <kinds>", "Comma-separated kind filter: go,pnpm,podman (default: all)"},
	{"-j, --jobs <n>", "Max targets run in parallel, interactive only (default: CPU count)"},
	{"-T, --timeout <dur>", "Per-target deadline, e.g. 10m / 30s / 0 to disable (default: 10m)"},
	{"-y, --yes", "Skip the menu; run everything non-interactively & sequentially (CI-safe)"},
	{"--no-cover", "test only: skip coverage (it is collected & reported by default)"},
	{"--dry-run", "List detected targets (and their envs) and exit without running"},
	{"-h, --help", "Show this command's help"},
}

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
		flags: runFlags,
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
		flags: runFlags,
		examples: []string{
			"a-novel test                  # interactive test menu",
			"a-novel test -t go            # only Go test targets",
			"a-novel test -y               # run all tests, no prompt (CI-safe)",
			"a-novel test --dry-run        # show targets + their envs, run nothing",
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

# Contributing to stack

This document is about working on the stack: the `a-novel` CLI, its daemon, and the developer guide. Read the [README](./README.md) first for what the CLI does, and set up your machine with the [developer guide](https://a-novel-kit.github.io/stack/). The [platform taxonomy](https://github.com/a-novel-kit/.github/blob/master/docs/taxonomy.md) places the CLI among the repository kinds, and the [service taxonomy](https://github.com/a-novel/.github/blob/master/docs/taxonomy.md) defines the service anatomy it operates on.

## Glossary

The CLI's own vocabulary. **Target**, **server**, **job** and **infra** come from the [service taxonomy](https://github.com/a-novel/.github/blob/master/docs/taxonomy.md#runnable-units) and are only summarized here.

| Term          | Meaning                                                                                                                                                      |
| ------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| **Stack**     | A workspace checkout the daemon manages: this repository, with its own `app/` and `kit/`. Stacks coexist, registered in `A_NOVEL_STACKS=name:path,…`.        |
| **Workspace** | The `app/` (services) and `kit/` (libraries) checkouts under a stack, cloned and fast-forwarded by `a-novel core sync`.                                      |
| **Daemon**    | The per-user background supervisor, reached over a unix socket. It starts and supervises targets, allocates ports, streams logs, and owns volume operations. |
| **Target**    | A `cmd/<name>` binary in a service, the runnable unit the daemon starts. A **server** stays up and exposes an API; a **job** runs once and exits.            |
| **Infra**     | A backing compose service with no `cmd/`, such as Postgres. It is a target's dependency, and the daemon brings it up before the targets that need it.        |
| **Mode**      | How a target runs: **go-exec** (`go run ./cmd/<target>`, the default) or **container** (`podman compose --profile <target>`).                                |
| **Phase**     | A target instance's lifecycle position, as the daemon tracks it: `pending → starting → running → stopping → terminated`.                                     |

## Layout

- `cli/`: the CLI's Go module, `github.com/a-novel-kit/stack/cli`. Commands live under `cli/internal/cli/` as Cobra subcommands.
- `docs/`: the developer guide, a [VitePress](https://vitepress.dev) site that `main.yaml` publishes to GitHub Pages from `master`.
- `app/`, `kit/`: checkouts of the repositories the CLI operates on, populated by `a-novel core sync`. Git ignores them; never commit inside them.

## Agent skills

The skill documents live in `.agents/skills/`, the shared path the Agent Skills standard defines. Codex and Copilot scan it directly. Claude Code scans only `.claude/skills`, so that path is a symlink into the same directory. Add a skill at `.agents/skills/<name>/SKILL.md` and every agent picks it up.

Git records that link as a symlink, so a Windows checkout needs symlink support to reproduce it: enable Developer Mode, or install Git with its "Enable symbolic links" option. Without it, the path arrives as a plain file holding the target, and Claude Code finds no skills. Staging that file would turn the link into a regular blob for everyone, so `pnpm lint:symlinks` checks the recorded mode and fails on it.

## How the CLI works

### Architecture

`a-novel` is a single binary that fronts a long-lived background daemon. Its clients, the CLI and the TUI, talk to the daemon over a unix socket through [connect-rpc](https://connectrpc.com/), with the contract in `cli/proto/anovel/v1/core.proto`. The daemon supervises every running target, owns the port allocator, streams logs, and manages volumes, so every client sees the same state.

```
cli/
├── cmd/a-novel/main.go            single binary: sandbox dispatch, Cobra root, exit codes
├── proto/anovel/v1/core.proto     connect-rpc contract
└── internal/
    ├── daemon/                    daemon-side (server, runner, env, logs, volumes, ...)
    ├── client/rpc/                Unix-socket connect-rpc client
    ├── cli/                       Cobra command tree, workspace model, gh/git helpers
    ├── detect/                    working-tree discovery of test and build targets
    ├── build/                     a detected target as a job: env, compose up/down, deadline
    ├── jobs/                      bounded parallel job runner with live progress
    ├── repocfg/                   GitHub repo config templates and the plan they build
    ├── tui/                       Bubble Tea TUI
    ├── ui/                        picker, live job view and report for every batch command
    ├── setup/                     `core setup` bootstrap
    ├── secrets/                   local AES-256-GCM secrets store + env injection
    ├── update/                    best-effort "newer version available" notice
    ├── version/                   build-version resolution (ldflags / buildinfo)
    └── shared/                    XDG paths, stacks parser, compose files, archive retention
```

### Parallel jobs

`test`, `build`, `core sync` and `repo update --all` hand their work to one runner, `internal/jobs`. At most `--jobs` units run at once, each with its output captured. On a terminal, each finished job prints one status line above a live view of the running jobs. Without a terminal, each job prints a line as it starts and as it finishes, so output never interleaves.

A report closes the run. It shows the full output of every failure, or of every repository for `repo update --all`, as the audit trail of its writes, then coverage for tests. The summary prints last, so it is on screen when the command returns.

The default limit follows what bounds the work:

| Command             | Jobs at once                                                                                            |
| ------------------- | ------------------------------------------------------------------------------------------------------- |
| `test`, `build`     | A quarter of the CPUs, at least two, behind the picker; one at a time with `-y` or without a terminal.  |
| `core sync`         | One repository per CPU.                                                                                 |
| `repo update --all` | Six repositories, under GitHub's secondary rate limit. A rate-limited `gh` call retries after a minute. |

### Key invariants

- **One daemon per user**, listening on `$XDG_RUNTIME_DIR/a-novel.sock`.
- **Stateless recovery.** The daemon checkpoints no state of its own: every restart rebuilds it from podman labels, the filesystem and `A_NOVEL_STACKS`. The reinstall checkpoint is the one exception, scoped to one handoff.
- **Strict refusal of incoherent operations**, always with a remediation hint, such as "kill the target first".
- **Labeled containers.** The `anovel.stack`, `anovel.service` and `anovel.target` labels let the daemon adopt and clean up containers across restarts.
- **The daemon owns dependency ordering and health-gating.** Before starting a target, it brings infra up, waits for its health, and runs the one-shots to success, then passes `--no-deps` on every container `up`. Compose cannot do this: a go-exec host process can depend on a container's health, and podman-compose does not reliably enforce `depends_on` conditions, notably the one-shot completion that holds a server back until migrations finish.

### Secrets

The secrets store keeps a value out of every place it could leak from. A change to the CLI keeps these rules:

- **Never printed.** No command prints a secret value, and an error names a secret by its ID.
- **Child environment only.** A secret reaches a process through its environment, never its command line.
- **Set from a terminal.** `secrets set` reads the value with echo disabled and refuses to run without a terminal, so a value never goes through a pipe, a log or a shell history.
- **Value-free manifests.** Git holds only the variable-to-ID declarations; values stay encrypted on the developer's machine.

The [`secrets` package](cli/internal/secrets/doc.go) describes how the store encrypts them.

### State directories

```
$XDG_STATE_HOME/a-novel/           default ~/.local/state/a-novel/
├── daemon.log                     a detached daemon's output
├── logs/<stack>/<service>/<target>/{current.log, run-*.log}
└── reinstall.json                 the reinstall handoff, deleted after replay
$XDG_DATA_HOME/a-novel/            default ~/.local/share/a-novel/
├── backups/<stack>/<service>/<volume>/<timestamp>.tar.zst   the latest five per volume
└── secrets/{key, store.enc}
$XDG_RUNTIME_DIR/a-novel.sock      the daemon's socket
```

### The compose contract

In container mode the daemon runs a target through the compose file its service ships at `builds/podman-compose.yaml`, and discovery reads the service's targets, infra and volumes from the same file. That file follows a contract every service repository upholds, which the [`discovery` package](cli/internal/daemon/discovery/doc.go) documents.

## Adding a command

A new command is a Cobra subcommand under `cli/internal/cli/`, built by a `new<Name>Cmd` function and wired into its parent: `core` for the daemon's lifecycle and the workspace, `run` for daemon-backed verbs, or the root for standalone ones such as `test` and `build`. Lint, format and generate stay `pnpm` scripts, not CLI verbs.

## Building and testing

After a change under `cli/`, rebuild and reinstall the CLI. The new binary takes over from the running daemon in place, so running services stay up:

```bash
a-novel install
```

Run the checks CI runs, from the stack root:

```bash
cd cli
go generate ./...   # proto bindings; CI fails on a diff
go tool -modfile=golangci-lint.mod golangci-lint run ./...
go test ./...
cd ..
pnpm lint
pnpm docs:build     # fails on a dead internal link
```

`go generate ./...` regenerates the daemon contract's Go bindings after an edit to `cli/proto/anovel/v1/core.proto`. `pnpm docs:dev` serves the guide with live reload.

## Questions?

[Open an issue](https://github.com/a-novel-kit/stack/issues), with logs and environment details.

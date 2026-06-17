# a-novel

The A-Novel storyverse build tool. Single CLI replacing the per-repo bash
scripts.

## Install

```bash
go install github.com/a-novel-kit/stack/cli/cmd/a-novel@latest
a-novel core setup    # one-time interactive bootstrap (checks, dirs, .zshrc)
```

No upfront clone: `go install` builds the binary straight from the module path,
and `core setup` clones the stack repo into `~/git-projects/a-novel` (the default
stack) for you. After setup, every new shell auto-starts the daemon
(`a-novel core start` silent if already running). Then use the verbs.

## Command groups

```
a-novel
├── test          standalone — runs Go + pnpm tests in the working tree
├── build         standalone — builds Go binaries, pnpm bundles, Podman images
├── publish       standalone — cut a release (bump, commit, tag vX.Y.Z, push)
│   ├── version <new-version>           preflight + bump + commit + tag + push
│   └── stamp <prefix> <file>           refresh vX.Y.Z references in doc files
├── install       graceful binary upgrade (daemon handoff via checkpoint)
├── core          daemon control + workspace plumbing
│   ├── start / setup / kill / status / prepare-reinstall
│   ├── sync                            clone/ff-pull the curated workspace repos
│   └── bot-token / bot-gh <org>        GitHub App token mint (comments-only gh)
└── run           daemon-backed surface for operating on services/targets:
    ├── ui                              full-screen TUI (Bubble Tea)
    ├── ps / stacks / topology          discovery & state
    ├── service (status / infra)         service-level operations
    ├── start / kill / restart           target lifecycle (go-exec | container)
    ├── logs / env / watch               observability
    ├── volume (list / backup / restore / clear)   service-scoped volumes
    └── exec / debug                    inside-container shells
```

`test`, `build` and `publish` don't need the daemon. Everything under `run`
does.

## Quick reference

```bash
# Daemon
a-novel core start            # silent if already running (lives in .zshrc)
a-novel core status           # is it running? what stacks? checkpoint?
a-novel core kill             # graceful shutdown

# Discovery
a-novel run ps                                # list services + target states
a-novel run topology --service=service-X      # ASCII dependency tree
a-novel run service status <service>          # one service in detail

# Lifecycle (auto-cascades deps via compose `depends_on`)
a-novel run start <service>/<target>          # go-exec by default
a-novel run start <service>/<target> --mode=container
a-novel run kill <service>/<target>
a-novel run restart <service>/<target>
a-novel run service infra start <service>     # bring up infra + one-shots
a-novel run service infra kill <service>      # refuses if targets running

# Observability
a-novel run logs <service>/<target> --follow
a-novel run env <service>                     # shell-evalable env block
eval "$(a-novel run env <service>)"

# Volumes (service-scoped; refuses while service is up unless --force)
a-novel run volume list <service>
a-novel run volume backup <service> --tag=<label>
a-novel run volume restore <service> [--from=<ts>]
a-novel run volume clear <service> [--no-backup]

# TUI
a-novel run ui                                # ? for help, Esc for commands

# Releases (local-only; CI release workflow fires on the pushed tag)
a-novel publish version 0.21.0                # or: patch / minor / major
```

## Architecture

`a-novel` is a single binary that fronts a long-lived background daemon.
Clients (CLI, TUI, future web UI) communicate with the daemon over a
unix-domain socket using [connect-rpc](https://connectrpc.com/). The same
daemon supervises every running target, owns the env/port allocator,
streams logs, and manages volumes. Multiple clients see consistent state.

### Package layout

```
cli/
├── cmd/a-novel/main.go            single binary; Cobra dispatch + legacy test/build
├── proto/anovel/v1/core.proto     connect-rpc contract
└── internal/
    ├── daemon/                    daemon-side (server, runner, env, logs, volumes, ...)
    ├── client/rpc/                unix-socket connect-rpc client
    ├── cli/                       Cobra command tree (test/build are wrapped legacy)
    ├── detect/                    working-tree discovery (test/build/run targets)
    ├── build/                     standalone test/build execution engine
    ├── tui/                       Bubble Tea TUI
    ├── ui/                        interactive pickers + reports for test/build
    ├── setup/                     `core setup` bootstrap
    ├── version/                   build-version resolution (ldflags / buildinfo)
    └── shared/                    XDG paths, stacks parser
```

### Key invariants

- **One daemon per user**, unix socket at `$XDG_RUNTIME_DIR/a-novel.sock`.
- **Stateless recovery**: daemon doesn't checkpoint its own state —
  every restart rebuilds from podman labels + filesystem + env var. The
  reinstall checkpoint is the named exception, scoped to one handoff
  cycle.
- **Strict refusal of incoherent ops** with always-included remediation
  hints (e.g., "kill the target first" rather than silently failing).
- **Multi-stack** by default: configure via `A_NOVEL_STACKS=name1:path1,name2:path2`.
- **Containers are labeled** (`anovel.stack`, `anovel.service`,
  `anovel.target`) so adoption + cleanup work across daemon restarts.

## State directories

- `$XDG_STATE_HOME/a-novel/` (default `~/.local/state/a-novel/`)
  - `logs/<stack>/<service>/<target>/{current.log, run-*.log}`
  - `reinstall.json` (single-purpose handoff; deleted after replay)
- `$XDG_DATA_HOME/a-novel/` (default `~/.local/share/a-novel/`)
  - `backups/<stack>/<service>/<volume>/<timestamp>.tar.zst` (max 5/volume)
- `$XDG_RUNTIME_DIR/a-novel.sock` (daemon's unix socket)

## Development

```bash
# Edit the proto (cli/proto/anovel/v1/core.proto), then:
go generate ./...      # regenerates proto Go bindings via buf
go test ./...          # or, dogfooding: a-novel test --type=go -y

# Graceful binary upgrade (preserves running go-exec targets via the
# reinstall checkpoint):
a-novel install
```

CI's `generated-go` job runs `go generate ./...` and fails on diff —
keeps proto bindings in sync.

## See also

- Per-service `app/service-*/builds/podman-compose.yaml` — the compose
  contract the daemon's discovery enforces (every `cmd/<target>/` has a
  profile-matched compose mirror; long-runners declare healthchecks;
  `depends_on` is complete; ports are `${VAR}` references the daemon
  allocates).
- `.claude/skills/use-a-novel-cli/` — the full raw-command → CLI mapping.

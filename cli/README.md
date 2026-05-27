# a-novel

The A-Novel storyverse build tool. Single CLI replacing the per-repo bash
scripts.

## Install

```bash
git clone git@github.com:a-novel-kit/stack.git ~/git-projects/a-novel
cd ~/git-projects/a-novel/cli
go install ./cmd/a-novel
a-novel core setup    # one-time interactive bootstrap (checks, dirs, .zshrc)
```

After setup, every new shell auto-starts the daemon (`a-novel core start`
silent if already running). Then use the verbs.

## Three command groups

```
a-novel
├── test          standalone — runs Go + pnpm tests in the working tree
├── build         standalone — builds Go binaries, pnpm bundles, Podman images
├── core          daemon control (start, setup, kill, status, prepare-reinstall)
└── run           daemon-backed surface for operating on services/targets:
    ├── ui                              full-screen TUI (Bubble Tea)
    ├── ps / stacks / topology          discovery & state
    ├── service (status / infra)         service-level operations
    ├── start / kill / restart           target lifecycle (go-exec | container)
    ├── logs / env / watch               observability
    ├── volume (list / backup / restore / clear)   service-scoped volumes
    └── exec / debug                    inside-container shells
```

`test` and `build` don't need the daemon. Everything under `run` does.

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
```

## Architecture

`a-novel` is a single binary that fronts a long-lived background daemon.
Clients (CLI, TUI, future web UI) communicate with the daemon over a
unix-domain socket using [connect-rpc](https://connectrpc.com/). The same
daemon supervises every running target, owns the env/port allocator,
streams logs, and manages volumes. Multiple clients see consistent state.

Spec: `spec.md` at the repo root (working document; deleted post-MVP).
Implementation plan + phased rollout: `PLAN.md`.

### Package layout

```
cli/
├── cmd/a-novel/main.go            single binary; Cobra dispatch
├── proto/anovel/v1/core.proto     connect-rpc contract
├── internal/
│   ├── daemon/                    daemon-side (server, runner, env, logs, volumes, ...)
│   ├── client/rpc/                unix-socket connect-rpc client
│   ├── cli/                       Cobra command tree (test/build are wrapped legacy)
│   ├── tui/                       Bubble Tea TUI
│   ├── setup/                     `core setup` bootstrap
│   ├── help/                      shared help registry (TODO: phase 13 polish)
│   └── shared/                    XDG paths, stacks parser
└── scripts/install.sh             graceful daemon-handoff install wrapper
```

### Key invariants

- **One daemon per user**, unix socket at `$XDG_RUNTIME_DIR/a-novel.sock`.
- **Stateless recovery** (spec §3.3): daemon doesn't checkpoint its own
  state — every restart rebuilds from podman labels + filesystem + env
  var. The reinstall checkpoint (§3.6) is the named exception, scoped
  to one handoff cycle.
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
make test-unit         # not yet wired; for now: go test ./...

# Graceful binary upgrade (preserves running go-exec targets via reinstall
# checkpoint — see spec §3.6):
./scripts/install.sh
```

CI's `generated-go` job runs `go generate ./...` and fails on diff —
keeps proto bindings in sync.

## See also

- `spec.md` — full design specification (working doc)
- `PLAN.md` — implementation plan + phased rollout
- Per-service `app/service-*/builds/podman-compose.yaml` — compose
  contract per spec §11 (every service-* repo's PR in the
  `chore/builds/migrate-compose-for-daemon` branch)

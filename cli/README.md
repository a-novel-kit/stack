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
├── secrets       standalone — local, encrypted secrets manager (child-env only)
│   ├── init                            create the local key + store dir
│   ├── set <id>                        store a value (no echo); never printed
│   ├── ls / rm <id>                    list ids / delete a secret
│   └── exec --env NAME=<id> -- <cmd>   run a command with secrets in its env
├── install       graceful binary upgrade (daemon handoff via checkpoint)
├── core          daemon control + workspace plumbing
│   ├── start / setup / kill / status / prepare-reinstall
│   ├── sync                            clone/ff-pull the curated workspace repos
│   └── bot-comment <org> <repo> <n>    comment as the org bot (via dispatcher workflow)
└── run           daemon-backed surface for operating on services/targets:
    ├── ui                              full-screen TUI (Bubble Tea)
    ├── ps / stacks / topology          discovery & state
    ├── service (status / infra)         service-level operations
    ├── start / kill / restart           target lifecycle (go-exec | container)
    ├── logs / env / watch               observability
    ├── volume (list / backup / restore / clear)   service-scoped volumes
    └── exec / debug                    inside-container shells
```

`test`, `build`, `publish` and `secrets` don't need the daemon. Everything
under `run` does.

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

# Secrets (local, encrypted; injected into the child env only)
a-novel secrets init                          # one-time: create key + store
a-novel secrets set OPENAI_KEY                # reads value with no echo
a-novel secrets ls                            # ids only, never values
a-novel secrets exec --env OPENAI_API_KEY=OPENAI_KEY -- python main.py
```

## Secrets

`a-novel secrets` is a local, encrypted secrets manager. It lets you (and an
AI agent) run the toolchain with API secrets — e.g. `OPENAI_API_KEY` — injected
into a **child process's environment only**, so a secret is never seen on a
terminal, printed, logged, committed, or passed as a CLI argument.

```bash
a-novel secrets init                 # create the local key + store dir (idempotent)
a-novel secrets set <id>             # read a value with NO echo, store it encrypted
a-novel secrets ls                   # list secret ids (never values)
a-novel secrets rm <id>              # delete a secret
a-novel secrets exec --env NAME=<id> [--env ...] -- <cmd> [args...]
```

### Per-repo manifest + auto-injection

A service repo can commit a **value-free** manifest at `.a-novel/secrets.yaml`
in its root, declaring the secrets it needs. Each entry pairs a target
environment-variable name with a secret `id` in the local store (never a value)
and an optional description:

```yaml
# .a-novel/secrets.yaml — safe to commit; contains no secrets
secrets:
  - env: OPENAI_API_KEY
    id: openai-key
    description: OpenAI API key used by narrative generation.
  - env: ANTHROPIC_API_KEY
    id: anthropic-key
```

The declared secrets are decrypted from the local store and injected
automatically into the child env of `a-novel test`, `a-novel run` and
`a-novel run ui`. If the local key or the store is absent — or a declared secret
isn't set yet — that secret is **skipped, never injected, and never an error**:
the repo can still run the tests that don't need it. A skipped secret surfaces a
value-free warning (its env var, id, and description, plus `a-novel secrets set
<id>`) in the `test` output and in the service log shown by `run logs` and the
UI, so you know exactly what to provision.

### Security properties

- **Encrypted at rest.** The store (`store.enc`) is the whole secrets map
  encrypted with **AES-256-GCM**; a fresh random 12-byte nonce per write, so two
  writes of the same value yield different ciphertext, and a tampered store fails
  to decrypt. Crypto is Go standard library only.
- **0600 local key.** A 32-byte key (`key`, mode `0600`) inside a `0700`
  directory under `$XDG_DATA_HOME/a-novel/secrets/`. Generated once and never
  overwritten. Writes are atomic (temp file + rename).
- **TTY-set, never piped.** `secrets set` reads the value with echo disabled and
  refuses to run without a terminal, so a value can't be piped through a log or
  shell history.
- **Never printed.** No command prints a secret value — not `ls`, not `env`, not
  the test/run env progress block. Errors carry the secret **id**, never the
  value.
- **Value-free git manifests.** Only the env-var → id (+ description)
  declarations live in git; values stay encrypted on the developer's machine.
- **Child-env only.** Secrets reach a process exclusively through its
  environment — never a command line.

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
    ├── secrets/                   local AES-256-GCM secrets store + env injection
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
  - `secrets/{key, store.enc}` (0600 local key + AES-256-GCM encrypted store)
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

## The compose contract

In **container** mode the daemon doesn't invent how to run a target — it defers
to the compose file a service repo ships at `builds/podman-compose.yaml`.
Discovery only works when that file mirrors the service's targets exactly, so the
mirror is a contract every service repo upholds:

- **One profile per target.** Each `cmd/<target>/` has a matching compose service
  gated behind `--profile <target>`, so `a-novel run start <service>/<target>`
  becomes `podman compose --profile <target> up`. A `cmd/` with no compose mirror
  is invisible to container mode.
- **Healthcheck ⇒ target kind.** A server (long-running, exposes an API) declares
  a `HEALTHCHECK`; a job (one-shot) does not. The daemon reads that presence to
  tell the two apart — a server is "ready" when its healthcheck passes, a job when
  it exits 0.
- **Complete `depends_on`.** Every dependency a target needs — infra such as
  Postgres, and one-shot jobs such as migrations — is declared, with
  `condition: service_healthy` for servers and `service_completed_successfully`
  for jobs, so the daemon cascades start-up in the right order.
- **`${VAR}` ports.** Host ports are `${VAR}` references, never hard-coded: the
  daemon owns the port allocator and injects a free port per run, so two stacks —
  or two targets — never collide.

What a target, server, job, or infra _is_ belongs to
[service anatomy](https://github.com/a-novel/.github/blob/master/CONTRIBUTING.md);
this section is only how the daemon discovers it through compose.

## See also

- Per-service `app/service-*/builds/podman-compose.yaml` — the compose mirror
  each service ships; see [The compose contract](#the-compose-contract) above.
- `.claude/skills/use-a-novel-cli/` — the full raw-command → CLI mapping.

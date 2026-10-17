# Contributing to stack

The platform taxonomy — repository kinds, libraries vs tooling, and where the `a-novel` CLI sits — lives in the [libraries, tooling & platform concepts](https://github.com/a-novel-kit/.github/blob/master/CONTRIBUTING.md). The service anatomy the CLI operates on (target, server / job, infra, the clean-arch layers) is defined in the [service & architecture concepts](https://github.com/a-novel/.github/blob/master/CONTRIBUTING.md) and only referenced here. Platform setup and day-to-day commands are in the [developer guide](https://a-novel-kit.github.io/stack/); read the [README](./README.md) first for what the CLI does.

This repo is the source of the `a-novel` CLI; [`cli/README.md`](./cli/README.md) is its usage reference. This file covers how to work on the repo and [how the CLI works](#how-the-cli-works).

## Glossary

The CLI's own vocabulary. Terms borrowed from service anatomy — **target**, **server**, **job**, **infra** — are defined once in the [service & architecture concepts](https://github.com/a-novel/.github/blob/master/CONTRIBUTING.md) and only summarized here.

| Term          | Meaning                                                                                                                                                                                                                                                    |
| ------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **Stack**     | This repo _and_ a registered workspace checkout the daemon manages. Stacks coexist (`A_NOVEL_STACKS=name:path,…`); each carries its own `app/` + `kit/`.                                                                                                   |
| **Workspace** | The `app/` (services) + `kit/` (libraries) checkouts under a stack, cloned and fast-forwarded by `a-novel core sync`. Distinct from the stack that hosts them.                                                                                             |
| **Daemon**    | The per-user background supervisor: one per user, reached over a unix socket. It starts and supervises targets, allocates ports, streams logs, and owns volume operations.                                                                                 |
| **Target**    | A `cmd/<name>` binary in a service — the runnable unit the daemon starts. A **server** stays up and exposes an API; a **job** runs once and exits. (See [service anatomy](https://github.com/a-novel/.github/blob/master/CONTRIBUTING.md#runnable-units).) |
| **Infra**     | A backing compose service with no `cmd/` (e.g. Postgres) — a target's dependency, not a target itself. The daemon brings it up before the targets that need it.                                                                                            |
| **Mode**      | How a target runs: **go-exec** (`go run ./cmd/<target>`, the default) or **container** (`podman compose --profile <target>`).                                                                                                                              |
| **Phase**     | A target instance's lifecycle position, as the daemon tracks it: `pending → starting → running → stopping → terminated`.                                                                                                                                   |

## Layout

- `cli/` — the CLI's Go module (`github.com/a-novel-kit/stack/cli`). Commands live under `cli/internal/cli/` as Cobra subcommands.
- `docs/` — the developer guide, a [VitePress](https://vitepress.dev) site that `main.yaml` publishes to GitHub Pages from `master`. `pnpm docs:dev` serves it with live reload; `pnpm docs:build` fails on a dead internal link, as CI does.
- `app/`, `kit/` — checkouts of the repos the CLI operates on. Gitignored and populated by `a-novel core sync`; never commit inside them.

## Agent skills

The skill documents live in `.agents/skills/`, the shared path the Agent Skills standard defines. Codex and Copilot scan it directly. Claude Code scans only `.claude/skills`, so that path is a symlink into the same directory — one source, no copies to keep in sync. Add a skill at `.agents/skills/<name>/SKILL.md` and every agent picks it up.

Git records that link as a symlink, so a Windows checkout needs symlink support to reproduce it: enable Developer Mode, or install Git with its "Enable symbolic links" option. Without it the path arrives as a plain file holding the target and Claude Code finds no skills, while Codex and Copilot keep reading `.agents/skills`. Staging that file would turn the link into a regular blob for everyone, so `pnpm lint:symlinks` checks the recorded mode and fails on it.

## How the CLI works

The [CLI reference](./cli/README.md) covers how to use the CLI. This section covers how it works, for anyone changing it or a repository it operates on.

### Architecture

`a-novel` is a single binary that fronts a long-lived background daemon.
Clients (CLI, TUI, future web UI) communicate with the daemon over a
Unix-domain socket using [connect-rpc](https://connectrpc.com/). The same
daemon supervises every running target, owns the env/port allocator,
streams logs, and manages volumes. Multiple clients see consistent state.

#### Package layout

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
    ├── repocfg/                   GitHub repo config templates + apply engine
    ├── tui/                       Bubble Tea TUI
    ├── ui/                        picker, live job view and report for every batch command
    ├── setup/                     `core setup` bootstrap
    ├── secrets/                   local AES-256-GCM secrets store + env injection
    ├── update/                    best-effort "newer version available" notice
    ├── version/                   build-version resolution (ldflags / buildinfo)
    └── shared/                    XDG paths, stacks parser, compose files, archive retention
```

#### Parallel jobs

`test`, `build`, `core sync` and `repo update --all` hand their work to one
runner (`internal/jobs`): at most `--jobs` units run at once, each with its
output captured. On a terminal, each finished job prints one status line that
stays in scrollback, above a live view of the running jobs (spinner, latest
output line, timer). Without a terminal, each job prints a line as it starts and
as it finishes, so output never interleaves. A report closes the run: the full
output of every failure (of every repo, for `repo update --all`, as the audit
trail of its writes), then coverage for tests. The summary prints last, so it is
on screen when the command returns: the status lines again when the output above
pushed them out of view, then the outcome counts.

The default limit follows what bounds the work. `test` and `build` split the CPUs
(NumCPU/4 behind the picker, one at a time with `-y`); `core sync` runs one repo
per CPU; `repo update --all` applies six repos at a time, under GitHub's
secondary rate limit, and a rate-limited `gh` call is retried after a minute.

#### Key invariants

- **One daemon per user**, Unix socket at `$XDG_RUNTIME_DIR/a-novel.sock`.
- **Stateless recovery**: daemon doesn't checkpoint its own state —
  every restart rebuilds from podman labels + filesystem + env var. The
  reinstall checkpoint is the named exception, scoped to one handoff
  cycle.
- **Strict refusal of incoherent ops** with always-included remediation
  hints (e.g., "kill the target first" rather than silently failing).
- **Multi-stack** by default: configure via `A_NOVEL_STACKS=name1:path1,name2:path2`.
- **Containers are labeled** (`anovel.stack`, `anovel.service`,
  `anovel.target`) so adoption + cleanup work across daemon restarts.
- **The daemon owns dependency ordering and health-gating**, not compose. It
  walks a target's deps itself — bringing infra up and waiting for health, then
  running one-shots to success — before starting the target, and passes
  `--no-deps` on every container `up`. Two reasons it can't delegate to compose:
  the dependency graph crosses the container boundary (a go-exec host process
  depending on a container's health), which compose has no way to express; and
  podman-compose does not reliably enforce `depends_on` conditions — notably
  one-shot completion, the gate that holds a server back until migrations
  finish. The compose files still declare `depends_on` for readers and for a
  direct `podman compose up`, but the daemon does not rely on it.

### Secrets

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

### State directories

- `$XDG_STATE_HOME/a-novel/` (default `~/.local/state/a-novel/`)
  - `logs/<stack>/<service>/<target>/{current.log, run-*.log}`
  - `reinstall.json` (single-purpose handoff; deleted after replay)
- `$XDG_DATA_HOME/a-novel/` (default `~/.local/share/a-novel/`)
  - `backups/<stack>/<service>/<volume>/<timestamp>.tar.zst` (max 5/volume)
  - `secrets/{key, store.enc}` (0600 local key + AES-256-GCM encrypted store)
- `$XDG_RUNTIME_DIR/a-novel.sock` (daemon's unix socket)

### The compose contract

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

## Adding a command

A new command is a Cobra subcommand under `cli/internal/cli/` (a `*_cmd.go` file), wired into the right parent — `core` (lifecycle), `run` (daemon-backed verbs), or top-level (`test` / `build` / `publish`). Lint, format, and generate stay as `pnpm` scripts, not CLI verbs.

## Building and testing

After a change under `cli/`, run `a-novel install` to rebuild and reinstall — the new binary takes over from the running daemon in place, so running services stay up. Test with `a-novel test`, or `go test ./...` from `cli/`.

After editing the daemon's contract in `cli/proto/anovel/v1/core.proto`, regenerate its Go bindings:

```bash
go generate ./...
```

CI's `generated-go-cli` job runs `go generate ./...` and fails on a diff, which keeps the bindings in sync.

## Questions?

[Open an issue](https://github.com/a-novel-kit/stack/issues) — include logs and environment details.

---
name: use-a-novel-cli
description: >
  Canonical reference for the `a-novel` CLI. ALWAYS load alongside any skill that
  involves running tests, building artifacts, or starting/stopping local services. The
  CLI replaces raw invocations (`make test-unit`, `podman build`, `podman compose up`)
  with three command groups: `test` (standalone), `build` (standalone), and `run`
  (daemon-backed verbs for `start`/`kill`/`logs`/`env`/`volume`/`ui` etc.) plus `core`
  for daemon lifecycle. Prefer `a-novel <verb>` over equivalent raw commands wherever
  a 1:1 mapping exists; raw `make` targets stay valid (CI uses them) but local-dev
  workflows should route through `a-novel`. Loaded automatically by `implement-feature`,
  `open-pull-request`, `resolve-pr-feedback`, `write-go`, `write-go-tests`,
  `write-go-service`, `write-go-kit`, `write-js-package`, `write-dockerfiles`,
  `write-bash-scripts`, and `write-proto`.
---

# Use the `a-novel` CLI

The `a-novel` CLI is the single user-facing entrypoint for local-dev workflows in the
a-novel ecosystem. It replaces ad-hoc invocations of `make`, `go test`, `podman build`,
and `podman compose` with three coherent command groups:

```
a-novel
├── test          standalone — runs Go + pnpm tests in the working tree
├── build         standalone — builds Go binaries, pnpm bundles, Podman images
├── core          daemon control (start, setup, kill, status, prepare-reinstall)
└── run           daemon-backed verbs (services + targets)
```

**Always prefer `a-novel <verb>` over the equivalent raw command** when one exists.
Raw `make` targets still work and CI uses them, but local-dev workflows should route
through the CLI for consistency.

---

## Quick mapping: raw → `a-novel`

| Raw / make | `a-novel` equivalent |
|---|---|
| `make test-unit` | `a-novel test --type=go -y` |
| `make test-pkg` | `a-novel test --type=go -y` (CLI auto-discovers `pkg/go` targets) |
| `make test-pkg-js` | `a-novel test --type=pnpm -y` |
| `make test` (everything) | `a-novel test -y` |
| `go test ./...` | `a-novel test --type=go --dir=.` |
| `make build` | `a-novel build -y` |
| `podman build -f Dockerfile -t name:local .` | `a-novel build --type=podman` |
| `pnpm build` | `a-novel build --type=pnpm` |
| `go run ./cmd/<target>` (service local-dev) | `a-novel run start <service>/<target>` |
| `podman compose --profile X up -d` | `a-novel run start <service>/<target> --mode=container` |
| `podman compose up <infra>` | `a-novel run service infra start <service>` |
| `podman compose down` | `a-novel run service infra kill <service>` |
| `podman logs -f <container>` | `a-novel run logs <service>/<target> --follow` |
| `podman volume export` + manual tar | `a-novel run volume backup <service>` |

Use the bare `a-novel <verb> --help` (or `a-novel help <verb>`) for the full flag list
of any subcommand. Every subcommand carries exhaustive Short/Long/Example help text.

---

## `a-novel test` — running tests

Discovers every Go test target (`go test ./...` per module, scoped by
`builds/podman-compose.go[.<path>].test.yaml` when present) and every pnpm
`test`/`test:*` script in the working tree, lets you pick which to run via a TUI
picker, runs the selection, and prints a pass/fail report. Test envs are brought up +
torn down per-target so independent envs run in parallel safely.

Common patterns:

```bash
a-novel test                  # interactive picker (everything selected by default)
a-novel test -y               # run everything non-interactively (CI-safe)
a-novel test --type=go        # only Go tests
a-novel test --type=pnpm      # only pnpm tests
a-novel test --type=go -y     # all Go tests, no prompt
a-novel test --dry-run        # show what would run; exit without running
a-novel test --no-cover       # skip coverage (on by default)
a-novel test -j 4             # cap parallelism at 4 (interactive only)
```

**When to use:** ALWAYS for local-dev test runs. Use `make test-unit` /
`make test-pkg` ONLY when:
- Documenting CI behavior (CI workflows invoke `make`)
- Scripted contexts inside the service repo that need the raw make target
- Debugging a specific make-rule interaction

**Substitutions in skill text:**
- Instead of "run `make test-unit`" → "run `a-novel test --type=go -y` (or
  `make test-unit` inside the service repo)"
- Test plan checkboxes in PR bodies:
  ```
  - [ ] `a-novel test --type=go -y` passes
  - [ ] `a-novel test --type=pnpm -y` passes (if JS changed)
  ```

---

## `a-novel build` — building artifacts

Discovers Go modules, pnpm build scripts, and `builds/*.Dockerfile` targets under
the working directory. Same interactive-picker / `-y`-non-interactive shape as
`a-novel test`.

```bash
a-novel build                 # interactive picker
a-novel build -y              # build everything non-interactively
a-novel build --type=go       # only Go binaries
a-novel build --type=podman   # only Podman images
a-novel build --type=go,pnpm  # union filter
a-novel build --dry-run       # list targets without building
```

**When to use:** ALWAYS for local-dev builds, especially when validating a
Dockerfile change. Avoid `podman build -f ...` directly; `a-novel build --type=podman`
discovers all Dockerfiles, builds them with the same convention CI uses, and prints
a pass/fail report.

---

## `a-novel run` — daemon-backed service operations

This is the entire surface for starting, stopping, observing, and inspecting
locally-running services. Requires the a-novel daemon to be running
(`a-novel core start`; lives in `~/.zshrc` after `a-novel core setup`).

### Lifecycle

```bash
a-novel run start <service>/<target>          # go-exec mode (default)
a-novel run start <service>/<target> --mode=container
a-novel run kill <service>/<target>
a-novel run restart <service>/<target>
a-novel run service infra start <service>     # bring up infra + auto-run one-shots
a-novel run service infra kill <service>      # refuses if any target running
a-novel run service infra kill <service> --force  # cascade-kill
```

The supervisor **auto-walks dependencies**: `a-novel run start service-X/rest`
brings up postgres, runs migrations + rotate-keys (one-shots), then starts rest.
Mutual exclusion is enforced (refuses with hint if the target is already running
in the other mode). One-shots are tracked per infra-up session — they re-run on
every `infra start` per spec §5.5.

### Observability

```bash
a-novel run ps                                # list services + target states
a-novel run topology --service=<svc>          # ASCII dep tree
a-novel run logs <service>/<target>           # snapshot
a-novel run logs <service>/<target> --follow  # stream live
a-novel run logs <service>/<target> --previous  # most recent archived run
a-novel run env <service>                     # shell-evalable env block
eval "$(a-novel run env <service>)"           # inject env into your shell
```

The daemon writes JSON-line logs to `~/.local/state/a-novel/logs/...` (current +
5 archived runs per target). `run logs` reads from there; `--follow` subscribes
through the daemon so multiple followers see the same stream.

### Volumes (service-scoped)

```bash
a-novel run volume list <service>
a-novel run volume backup <service> --tag=<label>
a-novel run volume restore <service> [--from=<timestamp>]
a-novel run volume clear <service> [--no-backup]
```

All destructive ops (backup/restore/clear) refuse while the service is up. Pass
`--force` to cascade-stop first. Backups land in `~/.local/share/a-novel/backups/`
as `tar.zst` archives (max 5 per volume, oldest pruned).

### TUI

```bash
a-novel run ui                                # full-screen TUI
# Inside: ? for help, Esc for command palette, q to quit
```

The TUI is a thin client over the same RPCs as the CLI — actions taken in the UI
are observable from `a-novel run watch` (when implemented) and vice-versa.

---

## `a-novel core` — daemon lifecycle

```bash
a-novel core setup            # one-time interactive bootstrap (run once after install)
a-novel core start            # idempotent + silent if already running (lives in .zshrc)
a-novel core status           # is it running? what stacks? checkpoint pending?
a-novel core kill             # graceful shutdown
a-novel core prepare-reinstall  # for scripts/install.sh — checkpoints + exits
```

`core setup` is interactive; everything else is non-interactive and `.zshrc`-safe.

---

## When NOT to use the CLI

Some tasks fall outside the CLI's scope and still use raw commands:

- **Lint / format**: `go tool -modfile=golangci-lint.mod golangci-lint run ./...`
  (per [[feedback-go-tools-policy]]); `go tool buf format -w` for protos. The
  CLI doesn't wrap these.
- **Direct database access**: use `a-novel run exec <service>/<infra-target> --
  psql -U postgres` (when implemented) OR `podman exec <container-name> psql ...`
  if exec isn't wired yet.
- **CI workflows**: CI invokes `make test-unit` / `make build` / etc. directly.
  Skills documenting CI behavior should reference the `make` targets.
- **Git operations**: standard `git` / `gh` (per [[feedback-bot-attribution]] —
  user token for PR ops, `bot_gh` for comments).

---

## Failure mode: daemon down

If `a-novel run <verb>` reports "daemon not reachable", run `a-novel core status`
to confirm. If down, `a-novel core start` brings it up (silent if already running).
First-time setup: `a-novel core setup`.

The daemon refuses to start if the default stack isn't set up — surface the error
verbatim to the user.

---

## Related memories

- [[feedback-go-tools-policy]] — `go tool -modfile=<x>.mod` for golangci-lint /
  gotestsum (the CLI doesn't wrap these; raw invocations stay).
- [[feedback-bot-attribution]] — `bot_gh` for PR/issue comments only; PR
  creation uses the operator's user token.
- [[project-workspace-layout]] — `app/` (gitignored services) + `kit/` checkouts.

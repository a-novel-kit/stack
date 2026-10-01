---
name: use-a-novel-cli
description: >
  Operate a-novel for tests, builds, releases, services, workspace/repository management, and
  secrets. Always load alongside skills that test, build, release, or start/stop services.
---

# Use the `a-novel` CLI

The `a-novel` CLI is the single user-facing entrypoint for local-dev workflows. It replaces
the deleted Makefiles and per-repo bash scripts (`go test` wrappers, `podman build`, `podman
compose`, `publish.sh`) with one coherent command surface:

```
a-novel
├── test          standalone — runs Go + pnpm tests in the working tree
├── build         standalone — builds Go binaries, pnpm bundles, Podman images
├── publish       standalone — release doc helpers (releases themselves run in CI)
├── repo          standalone — GitHub repo config, rulesets, required checks
├── core          daemon control (start, setup, kill, status, prepare-reinstall)
├── run           daemon-backed verbs (services + targets)
├── secrets       standalone — local encrypted secrets, injected into child envs
├── install       standalone — rebuild + reinstall the CLI, state-preserving
├── claude        standalone — launch Claude Code rooted at the stack
└── version       standalone — print the CLI version
```

`secrets`, `install`, `claude` and `version` complete the surface and have their own sections
below; `cli/README.md` in the stack repo remains the exhaustive reference.

**Always prefer `a-novel <verb>` over the equivalent raw command** when one exists.
Makefiles are gone from every repo — `make` is never the answer. What the CLI doesn't
cover lives in pnpm scripts (lint/format/generate, see "When NOT to use the CLI") so
each repo's surface is exactly: `a-novel <verb>` + `pnpm <script>` + raw `go`/`git`.

Load this skill alongside any skill that runs tests, builds artifacts, releases, or starts local
services.

---

## Choose the command reference

Read this entry point once, then load only the references for the operations you will perform.
The selected reference is required before executing its commands.

- Tests, builds, or release doc stamping: [testing and building](references/testing-building.md).
- Repository templates, rulesets, or required checks: [repository configuration](references/repositories.md).
  Agents use the dry-run path; repository writes remain human-only.
- Starting, stopping, inspecting, or clearing a service: [service operations](references/services.md).
- Daemon, stacks, workspace sync, or bot comments: [workspace operations](references/workspace.md).
- Installation, launch, version checks, or secrets: [installation and secrets](references/installation-secrets.md).
- Adding or reviewing package scripts: [script ownership and naming](references/package-scripts.md).

Use the narrowest target that covers the changed behavior. Keep lint/format/generate in the
repository's pnpm scripts; CI uses its own actions. None of these commands changes authorization
for releases, destructive cleanup, comments, or governance writes.

## Quick mapping: raw / legacy → `a-novel`

| Raw / legacy (deleted)                          | `a-novel` equivalent                                              |
| ----------------------------------------------- | ----------------------------------------------------------------- |
| `make test-unit` (gone)                         | `a-novel test --type=go -y`                                       |
| `make test-pkg` (gone)                          | `a-novel test --type=go -y` (CLI auto-discovers `pkg/go` targets) |
| `make test-pkg-js` (gone)                       | `a-novel test --type=pnpm -y`                                     |
| `make test` (gone)                              | `a-novel test -y`                                                 |
| `go test ./...`                                 | `a-novel test --type=go --dir=.`                                  |
| `make build` (gone)                             | `a-novel build -y`                                                |
| `podman build -f Dockerfile -t name:local .`    | `a-novel build --type=podman`                                     |
| `pnpm build`                                    | `a-novel build --type=pnpm`                                       |
| `go run ./cmd/<target>` (service local-dev)     | `a-novel run start <service>/<target>`                            |
| `podman compose --profile X up -d`              | `a-novel run start <service>/<target> --mode=container`           |
| `podman compose up <infra>`                     | `a-novel run service infra start <service>`                       |
| `podman compose down`                           | `a-novel run service infra kill <service>`                        |
| `podman logs -f <container>`                    | `a-novel run logs <service>/<target> --follow`                    |
| `podman volume export` + manual tar             | `a-novel run volume backup <service>`                             |
| `scripts/publish.sh patch` / `pnpm publish:*`   | release workflow in CI (release-core action) — no local verb      |
| `scripts/prepublish-version.sh <prefix> <file>` | `a-novel publish stamp <prefix> <file>`                           |
| `make lint-go` (gone)                           | `pnpm lint:go` (not a CLI verb — see "When NOT to use")           |
| `make format` (gone)                            | `pnpm format:go` / `pnpm format` / `pnpm format:proto`            |
| `make generate` (gone)                          | `pnpm generate:go` (plus `pnpm generate:mjml` where present)      |

`a-novel <verb> --help` (or `a-novel help <verb>`) prints the full flag list of any
subcommand. Every subcommand carries exhaustive Short/Long/Example help text.

---

## Driving the CLI non-interactively (agents, CI, scripts)

The CLI is interactive only where a human benefits — the `test` / `build` pickers
and the `run ui` TUI. Everything else runs to completion and returns. Agents, CI
jobs and scripts drive it like this:

- **`test` / `build`: always pass `-y`.** It skips the picker and runs every
  discovered target sequentially (CI-safe). Both fall back to that path with no
  TTY, but pass `-y` explicitly — it states intent and survives a stray PTY.
  Pair with `--dry-run` to inspect the target list first.

  ```bash
  a-novel test -y --type=go        # all Go tests, no prompt
  a-novel build -y --type=podman   # all Podman images, no prompt
  ```

- **`run` verbs are already non-interactive** — `start`, `kill`, `restart`,
  `logs`, `ps`, `service`, `volume`, `topology`, `env`, `watch`, `exec` and
  `debug` all complete and return an exit code. Only `run ui` (the TUI) is
  interactive: **never launch it from an agent or CI** — use the discrete verbs.

- **Observe state with `run watch`, do not poll `ps`.** It subscribes to the
  daemon's event stream and emits one newline-delimited JSON object per state
  change (phase transition, exit, health flip), so you react the moment a target
  turns healthy. Narrow it with `--service` / `--target`.

  ```bash
  a-novel run watch --service=service-json-keys   # NDJSON, one event per line
  ```

- **Ask for machine-readable output where it exists.** `run ps --json` emits one
  JSON object per service (with canonical fully-qualified target IDs).
  `run env --format=json` (or `dotenv`) replaces the default eval-able `shell` form.

  ```bash
  a-novel run ps --json
  a-novel run env <service> --format=json
  ```

---

## When NOT to use the CLI

Some tasks fall outside the CLI and use pnpm scripts or raw commands:

- **Lint / format / generate**: pnpm scripts, uniform across repos —
  `pnpm lint:go` / `pnpm lint:proto` / `pnpm lint` (node) and
  `pnpm format:go` / `pnpm format:proto` / `pnpm format` (prettier), plus
  `pnpm generate:go` (mocks/proto stubs). Each is a one-line wrapper over the
  raw form (`go tool -modfile=golangci-lint.mod golangci-lint run ./...`,
  `go tool -modfile=buf.mod buf format -w`, `go generate ./...`), so the raw forms
  stay valid too. Every Go tool is pinned in its own `<tool>.mod`, so each raw
  invocation names the modfile it comes from.
- **Direct database access**: `a-novel run exec <service>/<target> -- psql ...`.
  For a running container-mode target, the command runs inside its container
  (`podman exec`); for a go-exec or stopped target it runs on the host with the
  target's resolved env (`POSTGRES_DSN`, `*_PORT`, …) — e.g.
  `a-novel run exec <service>/migrations -- psql` to get `psql` with the right
  DSN. Raw `podman exec <container-name> psql ...` still works against a running
  container.
- **CI workflows**: CI never shells into the CLI — the `kit/workflows`
  composite actions invoke `gotestsum` / `golangci-lint` / `pnpm run <script>`
  directly. Skills documenting CI behavior reference those actions, not local
  commands.
- **Git operations**: standard `git` / `gh` — the operator's user token for PR ops,
  `a-novel core bot-comment` for comments.

---

## Failure mode: daemon down

If `a-novel run <verb>` reports "daemon not reachable", run `a-novel core status`
to confirm. If down, `a-novel core start` brings it up (silent if already running).
First-time setup: `a-novel core setup`.

The daemon refuses to start if the default stack isn't set up — surface the error
verbatim to the user.

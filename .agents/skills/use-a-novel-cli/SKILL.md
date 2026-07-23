---
name: use-a-novel-cli
description: >
  Canonical reference for the `a-novel` CLI. ALWAYS load alongside any skill that runs tests,
  builds artifacts, releases, or starts/stops local services. Covers the groups `test`, `build`,
  `publish`, `repo` (repository config, rulesets, required checks), `run` (daemon-backed
  `start`/`kill`/`logs`/`env`/`volume`/`ui`) and `core` (daemon lifecycle). Prefer
  `a-novel <verb>` over raw commands; lint/format/generate live in pnpm scripts
  (`pnpm lint:go`, `pnpm format:go`), never in Makefiles — deleted from every repo.
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
└── run           daemon-backed verbs (services + targets)
```

`secrets` (a local encrypted store injected into a child env), `claude` and `install` complete the
surface; `cli/README.md` in the stack repo is the exhaustive reference.

**Always prefer `a-novel <verb>` over the equivalent raw command** when one exists.
Makefiles are gone from every repo — `make` is never the answer. What the CLI doesn't
cover lives in pnpm scripts (lint/format/generate, see "When NOT to use the CLI") so
each repo's surface is exactly: `a-novel <verb>` + `pnpm <script>` + raw `go`/`git`.

Load this skill alongside any skill that runs tests, builds artifacts, releases, or starts local
services.

---

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

## `a-novel test` — running tests

Discovers every Go test target (`go test ./...` per module, scoped by
`builds/podman-compose.go[.<path>].test.yaml` when present) and every pnpm
`test`/`test:*` script in the working tree, lets you pick which to run via a TUI
picker, runs the selection, and prints a pass/fail report. Test envs come up and down
per-target, so independent envs run in parallel safely.

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

**When to use:** ALWAYS for local-dev test runs — there is no `make` fallback
(Makefiles and the `scripts/test*.sh` family are deleted). Raw `go test ./<path>/...`
remains for a single package/test while iterating. CI runs `gotestsum` directly
through the `kit/workflows` composite actions, not through the CLI.

**Test plan checkboxes in PR bodies:**

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

**When to use:** ALWAYS for local-dev builds, especially to validate a Dockerfile
change. Avoid raw `podman build -f ...`: `a-novel build --type=podman` discovers all
Dockerfiles, builds them with the same convention CI uses, and prints a pass/fail report.

---

## `a-novel publish` — release doc helpers

Releases are cut **in CI**: trigger the repo's release workflow and pick a release
type (patch / minor / major), and the `release-core` action (in `a-novel-kit/workflows`)
bumps the version, refreshes doc refs, commits, tags `vX.Y.Z`, pushes, and creates the
GitHub Release. The [Agent] bot performs the push. `manage-versions` covers it in depth.

There is **no local release command** — `stamp` is the only verb under `a-novel publish`.
`a-novel publish stamp <prefix> <file>` is the doc-stamping helper the `prepublish:doc`
pnpm scripts call: it rewrites `<prefix>vX.Y.Z` references (prefix is a regex) to the
current package.json version.

---

## `a-novel repo` — repository config and governance

`create` scaffolds a repository from its class template; `update` reconciles an existing one. This is
how the governance workflows, the branch rulesets, and the required-check list reach every repo — so
after adding or renaming a job in a repo's `.github/workflows/main.yaml`, its ruleset stays stale
until `update` runs.

The **class** is inferred from the repo name: `service-*` → a Go backend service, `platform-*` → a
SvelteKit frontend platform (a _terminal_ app — it ships a container image and a healthcheck route but
exports no package), `workflows` / `.github` → the shared-CI and meta repos, everything else → a shared
library (`golib`, `nodelib`, `jwt`, `stack`). A repo needing a different class carries a
`repos/<org>_<repo>.yaml` override, which wins over the name-based guess.

```bash
a-novel repo update --dry-run    # print the API operations, no writes — the agent-safe form
a-novel repo update              # interactive, human-only: a human must run this
a-novel repo update --all        # every whitelisted checkout present under app/ or kit/
```

Four behaviours to know before running it:

- **Required checks are derived, not configured.** They are the jobs in the repo's `main.yaml` (minus
  `report-*` and master-only jobs) plus the always-required set. A new job becomes a required check on
  the next `update`, and not before.
- **Config comes from the working tree**, not from GitHub. Run it on an up-to-date default branch, or
  it deploys whatever your checkout holds.
- **A checkout off its default branch is skipped**, silently and by design — reconciling from an
  in-progress branch would push half-finished template edits fleet-wide. Check branches before `--all`,
  or the repos you most care about are the ones quietly missed.
- **A newer deployed pin survives.** For files pinning `a-novel-kit/workflows` actions, a version
  already ahead of the template's is kept, so `update` never rolls back a bump Renovate landed.

Agents stop at `--dry-run`: the write path refuses a non-TTY.

---

## `a-novel run` — daemon-backed service operations

The entire surface for starting, stopping, observing, and inspecting locally-running
services. Requires the a-novel daemon (`a-novel core start`; lives in `~/.zshrc`
after `a-novel core setup`).

Run it from a single repo, or from the stack root — where it fans out across every `app/service-*`
**and** `app/platform-*` checkout, so a platform's dev-server `run`/`run:*` script shows up in the
picker beside the services' targets.

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
in the other mode). One-shots are tracked per infra-up session and re-run on every
`infra start`; they are idempotent by contract, so re-applying migrations locally
is by design.

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
are observable from `a-novel run watch` and vice-versa. Agents and CI use the discrete
verbs instead, see [Driving the CLI non-interactively](#driving-the-cli-non-interactively-agents-ci-scripts).

---

## `a-novel core` — daemon lifecycle + workspace tooling

```bash
a-novel core setup            # one-time interactive bootstrap (run once after install)
a-novel core start            # idempotent + silent if already running (lives in .zshrc)
a-novel core restart          # stop then start (use --preserve-targets for checkpoint replay)
a-novel core status           # is it running? what stacks? checkpoint pending?
a-novel core kill [--force]   # graceful shutdown (--force also tears down infra)
a-novel core prepare-reinstall  # used by `a-novel install` — checkpoints + exits

# Workspace tooling (ported from the old sync / bot-token bash scripts, now deleted).
a-novel core sync                          # clone/ff-pull the curated workspace whitelist
a-novel core sync --allow=a-novel-kit/golib  # subset to specific repos
a-novel core sync --ignore=<org>/<repo>      # skip specific repos
a-novel core bot-comment <org> <repo> <number> --body <text> [--reply-to <id>]
                                           # comment as the org App bot (see below)

# Stack lifecycle — allocate, audit, give back.
a-novel core stacks new <name>        # clone a fresh stack under the OS temp dir
a-novel core stacks new <name> --root=<path>  # ...or somewhere durable
a-novel core stacks list              # every stack: path, targets up, infra up, volumes
a-novel core stacks prune <name>      # kill its targets + infra, clear its volumes, remove its files
a-novel core stacks prune <name> --dry-run    # report what would be reclaimed
a-novel core stacks prune <name> --purge-backups  # also delete its volume backups
a-novel core stacks prune --all -y    # sweep every stack but the default
```

**Pruning a scratch stack.** A stack allocates three things and only one is a
file, so deleting the root reclaims the checkout but leaves containers holding
host ports and volumes in the container store. `stacks prune` releases all three,
in that order.

It refuses the default stack — that is the workspace, not scratch space — and
`--all` sweeps every _other_ registered stack, the pass to run after a batch of
agent sessions. It also refuses a stack whose checkouts hold work that exists
nowhere else (dirty tree, a non-default branch, unpushed commits) unless `--force`.
`$A_NOVEL_STACKS` lives in your shell config, so prune prints the entry to drop
instead of editing the file under you.

Volume backups survive: `ClearVolume` takes one on the way past, so the artefact
that undoes a prune outlives it. `--purge-backups` deletes them too.

**Where a new stack lives.** `stacks new` defaults to `<os temp dir>/a-novel-stacks/<name>`
via Go's `os.TempDir()`, which honours `$TMPDIR` — a per-user `/var/folders/…/T`
on macOS, `/tmp` on Linux. The OS reclaims both, so a stack nobody prunes expires
instead of accumulating. Pass `--root` for somewhere durable.

Because that home is swept, a registration can outlive its files. The daemon
skips such a stack rather than refusing to start over it, and `stacks list`
flags it (`files are gone — drop it from A_NOVEL_STACKS`) so the stale entry
stays visible.

`bot-comment` is the **only** way to post a PR/issue/review comment as
`<app-slug>[bot]`. It mints no local token: it triggers the centralized
`bot-comment` workflow in `a-novel-kit/stack` with your own `gh` token, and
that workflow (which alone holds the App keys) posts the comment and is watched
to completion. No `.pem` ever lives on a dev machine; you need only `gh` +
`actions:write` on the dispatcher repo. The bot can only comment — PR
authoring/merge/close are impossible through it.

`core setup` is interactive; everything else is non-interactive and `.zshrc`-safe.

**Sub-agents spawning fresh stacks**: run `a-novel core sync --root=<new-stack-root>`
as the first action in the new workspace. It pulls the six whitelisted repos into
`kit/` and `app/` so later test/build/run commands have something to operate on.
The whitelist is deliberately narrow (workflows, golib, nodelib, service-template,
service-json-keys, service-authentication) until the broader workspace stabilises;
expanding it is a one-line PR.

---

## pnpm scripts vs. the CLI — the boundary

When you touch a repo's `package.json` scripts (or review a PR that does), apply one rule:

> A pnpm script earns its place only when it carries something **specific to
> the repo** — a local package, a config file, a fixed argument set, or a hook
> the CLI itself invokes. A script that merely **mirrors a CLI capability** is
> indirection and must be deleted; run the CLI directly instead.

- **Delete** (pure mirrors): `publish:major|minor|patch` — releases are cut in
  CI by the release workflow (the `release-core` action), never a pnpm script or
  a local command. These wrappers added nothing and drifted; delete them.
- **Keep** (repo-specific constructs the CLI discovers or invokes):
  - `test` (`vitest run …`), `build:rest` (`vite build …`) — the concrete
    invocations `a-novel test` / `a-novel build` discover and run.
  - `lint:go` / `lint:proto` / `format:go` / `format:proto` / `generate:go` —
    lint/format/generate have no CLI verb by design (see below); these are
    their canonical home.
  - `prepublish:doc` and its `prepublish:doc:readme` / `:openapi` children —
    the release flow (`release-core`) runs `prepublish:doc` as a hook, and the
    children carry this repo's stamp prefix + file (`a-novel publish stamp
'<prefix>' <file>`). Those repo-specific args justify the script.

The smell test for a new/edited script: _strip the repo-specific part — if
what's left is just an `a-novel <verb>` call, the script shouldn't exist._

### Naming: generic does everything, language lanes are suffixed

A second rule governs how the surviving scripts are **named**:

> A **generic** verb (`format`, `lint`, `build`, `generate`, `test`) must do
> **everything** that verb covers in the repo. A script scoped to one
> language/lane is **suffixed** (`format:go`, `lint:proto`, `format:js`). A
> bare verb that silently runs only one lane is the bug this rule forbids.

- **Multi-lane verb → umbrella + suffixes.** A service has Go, Protobuf and a
  JS package, so `format` = `pnpm format:go && pnpm format:proto && pnpm
format:js`, and `lint` likewise. Each lane is a `:`-suffixed script; the bare
  verb chains them. The classic violation: `format` aliased to Prettier only,
  so `pnpm format` leaves Go unformatted and the contributor trips `lint-go` in
  CI.
- **Single-lane verb → stay generic, do NOT suffix.** A pure-JS repo
  (`nodelib`), a Prettier-only repo (`workflows`), or `build`/`test` in a
  service (only a JS pnpm lane — Go is built/tested via `a-novel`) already do
  everything under the bare verb. A redundant `:js`/`:go` alias there is
  overdoing it: the suffix disambiguates **multiple** lanes.
- **Name the lane by what it actually contains.** The Node/Prettier lane is
  `:js` when the repo ships a real JS/TS package (the lane runs eslint + tsc +
  prettier on actual JS). When the lane only runs Prettier over docs/config and
  there is **no JS** (`golib`), name it `:prettier` — `format:js` in a Go-only
  repo is the confusion this rule exists to prevent.
- **CI calls the lane, not the umbrella.** The `lint-node` composite action
  runs on a node-only runner with no Go/buf toolchain, so it must target the
  node lane (`lint:ci` → `lint:js`, or `lint_action: "lint:prettier"`), never
  the bare `lint` umbrella. The per-language CI jobs (`lint-go`, `lint-proto`)
  invoke their tools directly, not through pnpm. When you turn a bare verb into
  a Go-inclusive umbrella, re-point that repo's `lint-node` at the node lane in
  the same change or you red-build CI.

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

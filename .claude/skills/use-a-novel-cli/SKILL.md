---
name: use-a-novel-cli
description: >
  Canonical reference for the `a-novel` CLI. ALWAYS load alongside any skill that
  involves running tests, building artifacts, releasing, or starting/stopping local
  services. The CLI replaces the deleted Makefiles and per-repo scripts with four
  command groups: `test`, `build`, `publish` (standalone), and `run` (daemon-backed
  verbs for `start`/`kill`/`logs`/`env`/`volume`/`ui` etc.) plus `core` for daemon
  lifecycle. Prefer `a-novel <verb>` over equivalent raw commands wherever a 1:1
  mapping exists; lint/format/generate live in pnpm scripts (`pnpm lint:go`,
  `pnpm format:go`, ...), never in Makefiles (which no longer exist in any repo).
  Loaded automatically by `implement-feature`,
  `open-pull-request`, `resolve-pr-feedback`, `write-go`, `write-go-tests`,
  `write-go-service`, `write-go-kit`, `write-js-package`, `write-dockerfiles`,
  `write-bash-scripts`, and `write-proto`.
---

# Use the `a-novel` CLI

The `a-novel` CLI is the single user-facing entrypoint for local-dev workflows in the
a-novel ecosystem. It replaces the deleted Makefiles and per-repo bash scripts (`go
test` wrappers, `podman build`, `podman compose`, `publish.sh`) with four coherent
command groups:

```
a-novel
├── test          standalone — runs Go + pnpm tests in the working tree
├── build         standalone — builds Go binaries, pnpm bundles, Podman images
├── publish       standalone — cut a release (bump, commit, tag vX.Y.Z, push)
├── core          daemon control (start, setup, kill, status, prepare-reinstall)
└── run           daemon-backed verbs (services + targets)
```

**Always prefer `a-novel <verb>` over the equivalent raw command** when one exists.
Makefiles are gone from every repo — `make` is never the answer. What the CLI doesn't
cover lives in pnpm scripts (lint/format/generate, see "When NOT to use the CLI") so
each repo's surface is exactly: `a-novel <verb>` + `pnpm <script>` + raw `go`/`git`.

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
| `scripts/publish.sh patch` / `pnpm publish:*`   | `a-novel publish version patch`                                   |
| `scripts/prepublish-version.sh <prefix> <file>` | `a-novel publish stamp <prefix> <file>`                           |
| `make lint-go` (gone)                           | `pnpm lint:go` (not a CLI verb — see "When NOT to use")           |
| `make format` (gone)                            | `pnpm format:go` / `pnpm format` / `pnpm format:proto`            |
| `make generate` (gone)                          | `pnpm generate:go` (plus `pnpm generate:mjml` where present)      |

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

**When to use:** ALWAYS for local-dev test runs — there is no `make` fallback
anymore (Makefiles and the `scripts/test*.sh` family are deleted). The raw
`go test ./<path>/...` form remains for running a single package/test while
iterating; CI runs `gotestsum` directly through the `kit/workflows`
composite actions, not through the CLI.

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

**When to use:** ALWAYS for local-dev builds, especially when validating a
Dockerfile change. Avoid `podman build -f ...` directly; `a-novel build --type=podman`
discovers all Dockerfiles, builds them with the same convention CI uses, and prints
a pass/fail report.

---

## `a-novel publish` — cutting releases

Releases are created locally by a developer with push rights, not by CI. The
sequence is: bump version files → commit → tag `vX.Y.Z` → push commit + tag;
CI's release workflow fires on the pushed tag. Covered in depth by
`manage-versions` — the short form:

```bash
a-novel publish version 0.21.0        # explicit semver
a-novel publish version patch         # or a pnpm increment keyword
```

The command is workspace-aware (`pnpm version --recursive` vs
`--no-git-tag-version`), runs the repo's `prepublish:doc` script if present,
and preflights before mutating anything: branch == master, clean tree,
HEAD == origin/master, `git push --dry-run`.

**`publish version` is interactive-only.** It refuses to run without a TTY,
so an agent or CI job cannot cut a release — a release pushes to master and a
protected `v*` tag, which are human-only actions. This terminal check is the
cheap first gate; the real boundary is server-side (branch protection on
master + tag protection on `v*`) plus the token: a non-interactive token must
lack push-to-master and tag-create rights. Never try to route around the TTY
refusal (piping input, faking a PTY) — that is cheating the security model,
and the token should stop you anyway. See [[feedback-non-interactive-token]].

`a-novel publish stamp <prefix> <file>` is the doc-stamping helper the
`prepublish:doc` pnpm scripts call — it rewrites `<prefix>vX.Y.Z` references
(prefix is a regex) to the current package.json version.

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
every `infra start` (one-shots are idempotent by contract, so re-applying
migrations locally is by design).

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

## `a-novel core` — daemon lifecycle + workspace tooling

```bash
a-novel core setup            # one-time interactive bootstrap (run once after install)
a-novel core start            # idempotent + silent if already running (lives in .zshrc)
a-novel core restart          # stop then start (use --preserve-targets for checkpoint replay)
a-novel core status           # is it running? what stacks? checkpoint pending?
a-novel core kill [--force]   # graceful shutdown (--force also tears down infra)
a-novel core prepare-reinstall  # used by `a-novel install` — checkpoints + exits

# Workspace tooling — these replaced scripts/sync-repos.sh and scripts/lib/bot-token.sh.
a-novel core sync                          # clone/ff-pull the curated workspace whitelist
a-novel core sync --allow=a-novel-kit/golib  # subset to specific repos
a-novel core sync --ignore=<org>/<repo>      # skip specific repos
a-novel core bot-token <org>               # mint a 1h GitHub App installation token
a-novel core bot-gh <org> <gh args...>     # run gh as the bot (COMMENTS ONLY — enforced)
```

`core setup` is interactive; everything else is non-interactive and `.zshrc`-safe.

**Sub-agents spawning fresh stacks**: run `a-novel core sync --root=<new-stack-root>`
as the first action in the new workspace. This pulls the six whitelisted repos
into `kit/` and `app/` so subsequent test/build/run commands have something to
operate on. The current whitelist is intentionally narrow (workflows, golib,
nodelib, service-template, service-json-keys, service-authentication) until
the broader workspace is stabilised; expanding it is a one-line PR.

---

## pnpm scripts vs. the CLI — the boundary

When you touch a repo's `package.json` scripts (or review a PR that does),
apply one rule:

> A pnpm script earns its place only when it carries something **specific to
> the repo** — a local package, a config file, a fixed argument set, or a hook
> the CLI itself invokes. A script that merely **mirrors a CLI capability** is
> indirection and must be deleted; run the CLI directly instead.

- **Delete** (pure mirrors): `publish:major|minor|patch` →
  `a-novel publish version <bump>`. The script added nothing the CLI doesn't,
  and it drifts (each copy diverged independently before the CLI existed). Cut
  releases with `a-novel publish version` directly.
- **Keep** (repo-specific constructs the CLI discovers or invokes):
  - `test` (`vitest run …`), `build:rest` (`vite build …`) — the concrete
    invocations `a-novel test` / `a-novel build` discover and run.
  - `lint:go` / `lint:proto` / `format:go` / `format:proto` / `generate:go` —
    lint/format/generate have no CLI verb by design (see below); these are
    their canonical home.
  - `prepublish:doc` and its `prepublish:doc:readme` / `:openapi` children —
    `a-novel publish version` runs `prepublish:doc` as a hook, and the
    children carry this repo's stamp prefix + file (`a-novel publish stamp
'<prefix>' <file>`). The repo-specific args are exactly what justifies the
    script.

The smell test for a new/edited script: _strip the repo-specific part — if
what's left is just an `a-novel <verb>` call, the script shouldn't exist._

### Naming: generic does everything, language lanes are suffixed

A second rule governs how the surviving scripts are **named**, so a contributor
never gets a surprise:

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
  everything under the bare verb. Adding a redundant `:js`/`:go` alias there is
  overdoing it — don't. The suffix exists to disambiguate **multiple** lanes,
  not to restate the obvious.
- **Name the lane by what it actually contains.** The Node/Prettier lane is
  `:js` when the repo ships a real JS/TS package (the lane runs eslint + tsc +
  prettier on actual JS). When the lane only runs Prettier over docs/config and
  there is **no JS** (`golib`), name it `:prettier` — `format:js` in a Go-only
  repo is the confusion this whole rule exists to prevent.
- **CI calls the lane, not the umbrella.** The `lint-node` composite action
  runs on a node-only runner with no Go/buf toolchain, so it must target the
  node lane (`lint:ci` → `lint:js`, or `lint_action: "lint:prettier"`), never
  the bare `lint` umbrella. The per-language CI jobs (`lint-go`, `lint-proto`)
  invoke their tools directly, not through pnpm. When you turn a bare verb into
  a Go-inclusive umbrella, re-point that repo's `lint-node` at the node lane in
  the same change or you red-build CI.

---

## When NOT to use the CLI

Some tasks fall outside the CLI's scope and use pnpm scripts or raw commands:

- **Lint / format / generate**: pnpm scripts, uniform across repos —
  `pnpm lint:go` / `pnpm lint:proto` / `pnpm lint` (node) and
  `pnpm format:go` / `pnpm format:proto` / `pnpm format` (prettier), plus
  `pnpm generate:go` (mocks/proto stubs). Each is a one-line wrapper over the
  raw form (`go tool -modfile=golangci-lint.mod golangci-lint run ./...`,
  `go tool buf format -w`, `go generate ./...` — per
  [[feedback-go-tools-policy]]), so the raw forms stay valid too. The CLI
  deliberately doesn't wrap these.
- **Direct database access**: use `a-novel run exec <service>/<infra-target> --
psql -U postgres` (when implemented) OR `podman exec <container-name> psql ...`
  if exec isn't wired yet.
- **CI workflows**: CI never shells into the CLI — the `kit/workflows`
  composite actions invoke `gotestsum` / `golangci-lint` / `pnpm run <script>`
  directly. Skills documenting CI behavior should reference those actions, not
  local commands.
- **Git operations**: standard `git` / `gh` (per [[feedback-bot-attribution]] —
  user token for PR ops, `a-novel core bot-gh` for comments).

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
- [[feedback-bot-attribution]] — `a-novel core bot-gh` for PR/issue comments only;
  PR creation uses the operator's user token.
- [[project-workspace-layout]] — `app/` (gitignored services) + `kit/` checkouts.

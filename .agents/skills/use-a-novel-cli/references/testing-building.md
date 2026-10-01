# Testing, building, and release helpers

Read this reference when routed here by [use-a-novel-cli](../SKILL.md). Its rules apply to the selected work.

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

Discovers Go modules, pnpm build scripts, a root `Dockerfile`, and
`builds/*.Dockerfile` targets under the working directory. Same
interactive-picker / `-y`-non-interactive shape as `a-novel test`.

A required Dockerfile secret mount reads from the uppercase environment name
derived from its ID (`npm_token` → `NPM_TOKEN`). Declare that environment
name in the repository's value-free `.a-novel/secrets.yaml` manifest so the
encrypted local value reaches Podman without entering the image or command
output.

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

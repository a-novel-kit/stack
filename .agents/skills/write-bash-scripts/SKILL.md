---
name: write-bash-scripts
description: >
  Write, review, and maintain Bash shell scripts for Agora backend services. Use this skill
  whenever creating or editing any .sh file — test runners, build scripts, publish scripts,
  entrypoints, and env setup files. Covers error handling, cleanup traps, argument validation,
  service readiness waits, and cross-platform portability.
---

# Bash Script Writing Skill

This skill governs how to write and maintain shell scripts in Agora backend services. All scripts
live in `scripts/` (developer tooling) or `builds/` (container entrypoints). Read the relevant
section for the task at hand; the conventions at the end apply to every script.

**Before writing or editing any script**, read the existing scripts in `scripts/` and `builds/` to
understand the current patterns.

---

## Out of Scope: Release / Publish Scripts

**Do not write or revive a `scripts/publish.sh` or `scripts/prepublish-version.sh`** for any
service or kit repo. The release flow — version bump (workspace-aware), commit, tag, push, and
GitHub Release — runs **in CI** in the `release-core` GitHub Action (`a-novel-kit/workflows`),
triggered from the repo's release workflow with a release-type selector (patch / minor / major).
It auto-detects whether the repo uses `pnpm-workspace.yaml` (recursive `pnpm version`) or not
(root-only) and refreshes doc refs via `prepublish:doc` (`a-novel publish stamp`). The bash
equivalents were deleted because each copy drifted independently (pnpm 10 → 11 broke two of
three at once); there is no local release command to replace them.

**The security model for who can publish is server-side**, not script-side: GitHub branch
protection on `master` AND tag protection on `v*` are what actually enforce "only X can release."
The CLI's preflight is UX — it surfaces a 403 before any local mutation — but it is not a
boundary. See [[reference-release-security]] and [[project-publish-cli-verb]] in the memory
index for the full design.

In-container release steps (Docker image builds, npm publish on a pushed tag) are not in this
skill's scope either — they live in the `a-novel-kit/workflows/` composite GitHub Actions and run
on CI, not on a developer machine. Do not duplicate that logic in a bash script.

---

## After Every Edit

Run `bash -n <script>` to syntax-check without executing:

```bash
bash -n scripts/my-script.sh
```

Then run the script in a safe context (local env, no production credentials) to verify runtime
behaviour. For test runs, always go through `a-novel test` (per the `use-a-novel-cli` rule) —
the bash test-runner scripts it replaced no longer exist. Never trigger the release workflow
to test — it cuts a real release (pushes a tag, creates a GitHub Release).

---

## Structure of Every Script

```bash
#!/bin/bash

# One-line description of what the script does and when to run it.
# Usage note if the script takes arguments.

set -e  # Exit immediately if any command returns non-zero.

# ... argument validation ...
# ... variable declarations ...
# ... trap registration ...
# ... body ...
```

**`set -e`** is mandatory on every script. It ensures a failed command (e.g., a container that
failed to build) stops execution immediately rather than silently continuing with broken state.

**`#!/bin/bash`** — always use bash, not `/bin/sh`. The scripts use bash-specific features
(`$OSTYPE`, `[[`, process substitution) and must not silently degrade under sh.

---

## Cleanup Traps

Scripts that start external processes (containers, servers) must register a cleanup trap that tears
them down regardless of how the script exits:

```bash
APP_NAME="service-json-keys-test"
PODMAN_FILE="$PWD/builds/podman-compose.test.yaml"

cleanup() {
    podman compose -p "${APP_NAME}" -f "${PODMAN_FILE}" down --volume
}
trap cleanup INT EXIT
```

**Key rules:**

- Register on `EXIT` and `INT` only — not `ERR`. With `set -e`, a failing command exits the shell,
  which triggers `EXIT`. Adding `ERR` causes the cleanup to fire twice (once from ERR, once from
  EXIT), producing spurious error messages.
- Name the function `cleanup` (not `int_handler` or similar) — the name should describe what it
  does, not when it is called.
- Register the trap before sourcing env files or starting any processes, so cleanup is armed as
  early as possible.
- At the end of normal execution, let the `EXIT` trap handle teardown — do not call `down` twice.

---

## Argument Validation

Every script that takes arguments must validate them at the top, before doing any work:

```bash
if [ $# -ne 1 ]; then
    printf "Usage: %s <new-version>\n" "$0" >&2
    exit 1
fi
```

- Use `printf` (not `echo`) for error messages — `echo` behaviour with flags (`-e`, `-n`) differs
  between implementations.
- Write error messages to stderr (`>&2`).
- Use `$0` for the script name so error messages are correct regardless of how the script is
  invoked.

---

## Quoting and Variable Safety

Always double-quote variable expansions:

```bash
# Wrong — breaks on spaces, glob-expands
sed -i -E "s|pattern|replace|g" $2

# Correct
sed -i -E "s|pattern|replace|g" "$2"
```

**Exceptions**: intentionally word-split variables (e.g., a space-separated list of Go packages
passed as separate arguments to `gotestsum`) must be left unquoted and suppressed with
`# shellcheck disable=SC2046`:

```bash
# shellcheck disable=SC2046
go tool -modfile=gotestsum.mod gotestsum --format pkgname -- -count=1 -cover $PACKAGES
```

---

## Service Readiness

`podman compose up -d` returns as soon as containers are created, not when services are healthy.
Always wait for readiness before running tests or migrations against a service.

### Waiting for a container's HEALTHCHECK (database, gRPC server)

Poll `podman inspect` until the container's health status is `healthy`:

```bash
elapsed=0
until [ "$(podman inspect --format '{{.State.Health.Status}}' "${APP_NAME}_postgres-json-keys_1" 2>/dev/null)" = "healthy" ]; do
    elapsed=$((elapsed + 1))
    if [ "$elapsed" -ge 30 ]; then
        printf "error: postgres container did not become healthy within 30s\n" >&2
        exit 1
    fi
    sleep 1
done
```

- Use a 30–60s timeout depending on how long the service takes to start (60s for standalone images
  that run migrations and key rotation before starting the server).
- The container name format is `${APP_NAME}_${service_name}_1` where `APP_NAME` is the compose
  project name and `service_name` is the service key in the compose file.
- Redirect `podman inspect` stderr to `/dev/null` — the container might not exist yet at the
  first poll.

### Waiting for an HTTP endpoint (REST server)

Use `curl -sf` (suppress progress, fail on HTTP error) to poll the health endpoint:

```bash
elapsed=0
until curl -sf "${REST_URL}/ping" > /dev/null 2>&1; do
    elapsed=$((elapsed + 1))
    if [ "$elapsed" -ge 60 ]; then
        printf "error: REST service did not become ready within 60s\n" >&2
        exit 1
    fi
    printf "Waiting for REST service on port %s... (%ds)\n" "${REST_PORT}" "$elapsed"
    sleep 1
done
```

`curl` is available on Linux and macOS without installation. `wget` is an acceptable alternative
(`wget -q --spider <url>`) but is absent on macOS by default.

---

## Environment Variables

Port and URL allocation is the `a-novel` daemon's job now (`a-novel run env <service>`);
the old `scripts/setup-env.sh` files that hand-rolled random ports are gone. If a script
genuinely needs an env file, use the assign-if-unset pattern so pre-exported values are
preserved:

```bash
POSTGRES_PORT="${POSTGRES_PORT:=5432}"
export POSTGRES_PORT
```

- Use `${VAR:=default}` to set a default only when the variable is unset or empty.
- Source env files with `. "$PWD/scripts/env.sh"` (not `bash scripts/env.sh`), so the
  exported variables are visible in the calling shell.

---

## Cross-Platform Portability

Scripts must work on both Linux (GNU tools) and macOS (BSD tools). The main divergence is `sed`:

```bash
case "$OSTYPE" in
    darwin*|bsd*)
        sed -i '' -E "s|pattern|replace|g" "$file"
        ;;
    *)
        sed -i -E "s|pattern|replace|g" "$file"
        ;;
esac
```

GNU `sed` takes `-i` with no argument for in-place editing. BSD `sed` (macOS) requires `-i ''`.
Using `$OSTYPE` is reliable in bash on both platforms.

Other portability notes:

- `printf` instead of `echo` for formatted output — behaviour is consistent across platforms.
- `[ ... ]` (POSIX test) instead of `[[ ... ]]` in conditionals unless bash-specific features
  (regex, `&&`, `||` inside test) are needed — both work in bash but `[` is clearer about intent.
- Avoid `timeout <N> <cmd>` — not available on macOS without Homebrew coreutils. Use a polling
  loop with an elapsed counter instead.
- Avoid `readlink -f` — not available on macOS. Use `realpath` if available, or compute absolute
  paths relative to `$PWD` or `$0`.

---

## Output Formatting and Colors

**The first question for any new user-facing script is "does this belong in the
a-novel CLI instead?"** The stack's bash inventory shrunk to near-zero when
sync-repos and bot-token were ported to `a-novel core sync` and
`a-novel core bot-comment`. New tooling should prefer Cobra subcommands
under `cli/internal/cli/` over a fresh `scripts/*.sh` — Go gives us testable
flag parsing, real error types, and consistent help output. The only scripts
that stay in bash are tiny shims (often consumed by CI) where adding a Cobra
command would be heavier than the script itself.

For the rare remaining bash, keep formatting minimal: plain `printf`, no shared
helper library, and honor `NO_COLOR` if you emit colors at all.

```bash
# Honor NO_COLOR up front; everything else can branch on $RED/$RESET etc.
if [ -z "${NO_COLOR:-}" ] && [ -t 1 ]; then
    RED=$'\033[31m'; GREEN=$'\033[32m'; YELLOW=$'\033[33m'; RESET=$'\033[0m'
else
    RED=""; GREEN=""; YELLOW=""; RESET=""
fi

printf "%s✓%s %s\n" "${GREEN}" "${RESET}" "service-json-keys updated"
printf "%s✗%s %s\n" "${RED}"   "${RESET}" "remote unreachable" >&2
```

**Conventions:**

- Errors and warnings go to stderr (`>&2`). Successes and progress go to stdout.
- Use `printf '%s\n'` over `echo` for portability.
- Colors: green = success, yellow = warning, red = error. Never set backgrounds;
  avoid bright-white / bright-black — they vanish on light or dark themes.
- Honor `NO_COLOR` (set → never emit escapes). Don't gate on `FORCE_COLOR` unless a
  caller asks for it — keep the scaffolding small.
- Do not use `tr ' ' '─'` to build separator lines — `tr` is byte-oriented and will
  corrupt the multi-byte UTF-8 sequence (`0xE2 0x94 0x80`) by replacing each space
  with only `0xE2`. Use a pure-bash loop or pre-built constant string.

Long-lived interactive UX (rich progress, multi-phase banners, spinners) is the
Go CLI's job, not bash's. If a bash script is growing a banner/log_step framework,
that's a signal it should move under `a-novel` instead.

---

## Common Pitfalls

- **Missing `set -e`.** Without it, a failed `podman compose up --build` is silently ignored and
  subsequent commands (migrations, tests) run against nonexistent containers, producing confusing
  "connection refused" errors instead of a clear build failure.
- **`trap cleanup INT EXIT ERR`.** Adding `ERR` to the trap fires cleanup twice when `set -e`
  triggers an exit (ERR fires, then EXIT fires). Use `INT EXIT` only.
- **No readiness wait after `compose up -d`.** The `-d` flag returns immediately after containers
  are created, not when they're healthy. Always poll for health before running tests or migrations.
- **Unquoted `$1` / `$2` in function calls.** Breaks on paths or version strings containing
  spaces. Quote all variable expansions unless intentionally word-splitting.
- **`echo` for error messages.** Use `printf "...\n" >&2` — `echo` behaviour with `-e` and `-n`
  is implementation-defined.
- **Running node twice for the same value.** If `node -p "require('./package.json').version"` is
  called multiple times, cache the result in a variable:
  ```bash
  VERSION="$(node -p "require('./package.json').version")"
  ```
- **Missing argument validation.** A script called with wrong arity produces a confusing error
  from deep inside the script body. Always check `$#` at the top and print usage to stderr.
- **`source` instead of `.`.** Both work in bash, but `.` is POSIX. Use `.` for consistency.
- **`tr ' ' '─'` for separator lines.** `tr` operates on bytes, so a multi-byte
  UTF-8 character like `─` (`0xE2 0x94 0x80`) gets reduced to its first byte and
  the output is invalid UTF-8 (terminals render it as garbled `??` glyphs). Build
  Unicode-rule strings with a pure-bash loop or `printf -v` instead.

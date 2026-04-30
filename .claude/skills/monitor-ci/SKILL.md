---
name: monitor-ci
description: >
  Monitor GitHub Actions CI runs on a pushed branch or open PR, classify failures, and apply
  autonomous fixes where safe. Use this skill whenever waiting on CI, investigating a failing
  check, deciding whether a failure is real or flaky, or iterating a branch toward green.
  Covers the CI job map for Agora backend services and the retry/fix loop. Pairs with
  open-pull-request (which hands off here after a push) and git-conventions (for fix commits).
---

# Monitor CI

This skill governs how Claude watches CI after pushing to a branch, classifies failures, and
applies fixes. CI is the final gate before review — failures must be resolved, not ignored.
But CI is also long-running and noisy, and unstructured polling burns context. This skill
codifies which commands to run, how often, and how to act on each failure type.

The loop is: **observe → classify → fix → re-push → re-observe**, with a retry budget. When
the budget runs out, stop and escalate.

---

## CI Job Map (this repo)

The `main` workflow runs on every push to any branch. Jobs and their fix targets:

| CI Job                        | What it checks                             | Local equivalent                         | Typical failure                                                              |
| ----------------------------- | ------------------------------------------ | ---------------------------------------- | ---------------------------------------------------------------------------- |
| `generated-go`                | `go generate ./...` is up to date          | `make generate`                          | Forgot to run `make generate` after proto/interface                          |
| `lint-go`                     | `golangci-lint run` clean                  | `make lint-go`                           | New Go code violates style or has a bug                                      |
| `lint-proto`                  | `buf lint` clean                           | `make lint-proto`                        | Proto file violates buf style                                                |
| `lint-node`                   | `pnpm lint:ci` clean                       | `pnpm lint:ci`                           | JS/TS code violates eslint/prettier                                          |
| `test`                        | Go unit tests in `/internal`               | `make test-unit`                         | Broken Go code or test                                                       |
| `test-pkg`                    | Go integration tests in `/pkg/go`          | `make test-pkg`                          | gRPC contract mismatch OR flake                                              |
| `test-pkg-js`                 | JS integration tests in `/pkg/js`          | `make test-pkg-js`                       | REST contract mismatch OR flake                                              |
| `build-database`              | Docker build for Postgres image            | (no direct make target; read Dockerfile) | Dockerfile error, bad init script                                            |
| `build-migrations`            | Docker build for migrations job            | (none)                                   | Migration file issue                                                         |
| `build-job-rotate-keys`       | Docker build for rotate-keys job           | (none)                                   | Go build error in cmd/rotatekeys                                             |
| `build-grpc`                  | Docker build for gRPC service image        | (none)                                   | Go build error                                                               |
| `build-standalone-grpc`       | Docker build for standalone gRPC dev image | (none)                                   | Go build error                                                               |
| `build-rest`                  | Docker build for REST service image        | (none)                                   | Go build error                                                               |
| `build-standalone-rest`       | Docker build for standalone REST dev image | (none)                                   | Go build error                                                               |
| `build-js`                    | `pnpm build:rest` for pkg/js               | `pnpm -C pkg/js build:rest`              | TS compile error or broken export                                            |
| `report-grc` / `publish-docs` | Post-success reporting, **master only**    | (none)                                   | Rarely actionable; usually transient                                         |
| `report-codecov`              | Coverage upload, runs on **every branch**  | (none)                                   | Upload failure can still mark the run failed in PR checks; usually transient |

`test` blocks most application `build-*` jobs (`build-grpc`, `build-rest`,
`build-standalone-*`, `build-job-rotate-keys`). When `test` fails those downstream jobs
are cancelled — fix `test` first before looking at them. However, `build-database` does
**not** depend on `test`, and `build-migrations` depends only on `build-database`, so
failures in those two surface independently and need their own diagnosis.

---

## Phase 1: Observe

After pushing, observe the latest run on the current branch. Prefer `gh pr checks` when a PR
exists (cleaner output), otherwise `gh run list --branch <branch>`.

### 1.1 Check overall state

```bash
# If a PR is open for this branch
gh pr checks --watch=false

# Otherwise (no PR yet, e.g. just pushed a feature branch)
gh run list --branch "$(git rev-parse --abbrev-ref HEAD)" --limit 1 \
  --json databaseId,status,conclusion,name
```

Possible states:

- `queued` / `in_progress` / `pending` → wait and re-check (Phase 1.2)
- `completed` + `success` → done, hand off to the developer
- `completed` + `failure` → classify and fix (Phase 2)
- `completed` + `cancelled` → usually a dependency failed; fix the root-cause job
- `completed` + `skipped` → not an error; only reporting jobs are typically skipped on
  non-master branches

### 1.2 Polling pattern — do NOT use `gh run watch`

`gh run watch` blocks the terminal until the run finishes. In a Claude session it blocks the
whole turn, which is both inefficient and wastes context if the run takes 10+ minutes.

Instead, use the Bash tool's `run_in_background` parameter for the wait, then re-check:

```bash
# Start a background sleep matched to expected remaining time.
# Typical CI here takes ~8–12 min end-to-end; short jobs finish in ~2 min.
sleep 90
```

Run that with `run_in_background=true`, then on the next turn issue `gh pr checks` (or the
`gh run list` command above) to get the updated state. Repeat until the run is `completed`.

Rule of thumb for sleep durations:

- First check after push: `30s` — short jobs (lint, generated-go) complete by then
- Still in progress: `90s` — matches remaining test/build job cadence
- Known long wait (test-pkg-js after cold image pulls): `180s`

Do not poll faster than every 30 seconds — it spams the GitHub API and yields no new info.

### 1.3 Get the failing run details

When the overall state is `failure`:

```bash
# Identify the failing run ID from gh pr checks or gh run list output, then:
gh run view <run-id> --json jobs \
  --jq '.jobs[] | select(.conclusion=="failure") | {name, databaseId, conclusion}'
```

This lists only the failing jobs by name and ID — the minimum needed to decide what to fix.

### 1.4 Read ONLY the failed step logs

The full run log is huge and floods context. Always use `--log-failed` to get only the
failing steps:

```bash
gh run view <run-id> --log-failed --job <job-id>
```

If the `--log-failed` output is still too large (sometimes thousands of lines for a test
crash), pipe through `tail` or `grep` to narrow:

```bash
gh run view <run-id> --log-failed --job <job-id> | tail -n 200
gh run view <run-id> --log-failed --job <job-id> | grep -E "FAIL|Error|error:" | head -n 50
```

Never read the full run log unprefiltered. Never fetch logs for passing jobs.

---

## Phase 2: Classify

Once you have the failed-step log, map the failure to one of these categories. The category
determines the fix path.

### 2.1 `generated-go` failure

**Symptom**: job fails with a message like `go generate definitions are not up-to-date`.

**Root cause**: a `.proto` file or Go interface (used by a mock) changed without `make
generate` being run afterward.

**Fix**: the original commit is already pushed, so a new commit is the only option —
`git-conventions` forbids amending pushed history. The regenerated files land as their
own follow-up:

```bash
make generate
git status --porcelain
git add internal/handlers/protogen/ internal/handlers/mocks/ internal/services/mocks/
git commit -m "chore(gen): regenerate Go bindings for <scope>"
git push
```

This produces two commits for what would ideally be one (the proto/interface change
plus its regen), but that is the cost of noticing after push. The "generated files
belong in the same commit" guidance in `git-conventions` is a structure preference;
the "never amend a pushed commit" rule is categorical and wins here.

### 2.2 `lint-go` / `lint-proto` / `lint-node` failure

**Symptom**: linter reports specific files + line numbers.

**Fix**: run the exact make target locally, read its output, edit the flagged files, re-run
until clean, then commit:

```bash
make lint-go        # or lint-proto / lint-node
# edit flagged files
make lint-go        # re-run to confirm clean
git add <files>
git commit -m "fix(<scope>): resolve lint findings"
git push
```

Use a `fix(<scope>): resolve lint findings` commit for trivial mechanical changes and a
`refactor` or `fix` commit for more invasive rewrites. A noisy `fix(lint): ...` follow-up
on a pushed branch is still better than an amend — `git-conventions` forbids amending
pushed commits unconditionally, and the lint-fix commit can be squashed or absorbed by
the PR author at merge time if the branch uses squash-merge.

### 2.3 `test` (Go unit) failure

**Symptom**: `--- FAIL: TestXxx` in the log, optionally a stack trace.

**Fix**:

1. Reproduce locally first — never fix blind:

   ```bash
   # Run just the failing package and test for fast iteration
   go test ./internal/<package>/... -run TestXxx -v
   # Or the full suite if multiple tests fail
   make test-unit
   ```

2. Read the failure carefully. Decide: **is the test wrong, or is the code wrong?**
   - New test for behaviour the code doesn't yet implement → fix the code
   - Existing test that used to pass → fix the new code that broke it
   - Test assertion out of date vs. new intended behaviour → fix the test
   - Follow `write-go-code` / `write-go-tests` for the actual fix

3. Re-run until green locally, then commit. `monitor-ci` always runs on an
   already-pushed branch, so `git-conventions`' "never amend a pushed commit" rule
   applies unconditionally:
   - If the fix belongs with the feature: add a follow-up commit on the branch —
     squash-merge at PR merge time (if configured) will collapse it.
   - If it's a genuine separate fix: new `fix(<scope>)` commit.

4. Push and go back to Phase 1.

### 2.4 `test-pkg` / `test-pkg-js` failure

**Symptom**: integration test failure against a running gRPC or REST service.

**First check for flake**, since these jobs depend on cold image startup:

- `connection refused` / `dial tcp` / `EOF` / `context deadline exceeded` before any
  assertion → likely flake, service wasn't ready
- Sudden `502 Bad Gateway` or transport-level error → likely flake
- Timeout on first request only, subsequent requests pass locally → likely flake

For a suspected flake, retry the failed jobs only — do not rerun the whole workflow:

```bash
gh run rerun <run-id> --failed
```

If the same job fails twice with the same transport-level symptom, stop treating it as a
flake and investigate as real.

**If the failure is real** (assertion mismatch, wrong status code, unexpected field):

1. Reproduce locally — these suites need a running service:

   ```bash
   make test-pkg       # starts gRPC standalone
   make test-pkg-js    # starts REST standalone
   ```

2. The failure usually means a contract mismatch between handlers and client:
   - `test-pkg` failing → gRPC handler vs. `pkg/go` client drift — check `write-proto`
   - `test-pkg-js` failing → REST handler vs. `openapi.yaml` vs. `pkg/js/rest/` drift —
     all three must match, see `implement-feature`'s OpenAPI / REST / JS sync rule

3. Fix the out-of-date side, re-run until green, commit, push.

### 2.5 `build-*` (Docker) failure

**Symptom**: `docker build` step fails. Root causes:

- **Go compilation error** in the image's entrypoint binary → the real failure is in Go
  source; fix via Phase 2.3 approach (edit, `go build ./...`, commit). The `build-*`
  failure is a downstream symptom, not the root.
- **Dockerfile syntax / COPY path wrong** → follow `write-dockerfiles` to fix
- **Base image pull failure** → usually transient; retry with `gh run rerun --failed`
- **Migration init script failure** (`build-database`, `build-migrations`) → follow
  `write-sql` for migration fixes

Always read the `--log-failed` output before guessing. If the Go build fails, you'll see
`undefined: Foo` or `type Bar has no field Baz` — those are Go issues to fix in source,
not Dockerfile issues.

### 2.6 `build-js` failure

**Symptom**: `pnpm build:rest` fails in `pkg/js/`.

**Fix**: follow `write-js-package`. Reproduce with `pnpm -C pkg/js build:rest`. Typical
causes:

- TypeScript compile error after an API change
- Missing export in `pkg/js/rest/index.ts`
- Broken import path after a file rename

---

## Phase 3: Fix and Re-push

After applying a fix:

1. Re-run the relevant local target to confirm green (`make test-unit`, `make lint-go`,
   `make generate`, etc.)
2. Create a new fix commit per `git-conventions`. Do not amend or rewrite history on
   this already-pushed branch — doing so strands CI run logs and review threads
   anchored to the old SHAs.
3. Push. A push to the branch automatically triggers a new CI run.
4. Return to Phase 1.

Never push a fix without local verification. The goal of this loop is to use CI as
confirmation, not as a test runner.

---

## Phase 4: Retry Budget and Escalation

**Retry budget**: at most **3 fix attempts for the same root cause** before stopping and
escalating to the user. If the same test keeps failing after two real fixes, the diagnosis
is wrong — more guesses will waste time.

**Flake retry budget**: at most **2 reruns via `gh run rerun --failed`** for the same
suspected-flaky job before treating it as real. If `test-pkg-js` fails three times with
"connection refused" across three reruns, that's a genuine problem (container startup
regression, service crash at boot) — switch to Phase 2.4 "real" investigation.

**Escalate immediately (do not spend retry budget) when:**

- The failure involves a secret or credential (never debug secrets autonomously)
- The workflow file itself is failing to parse (YAML error) — check with the user before
  editing workflow files
- The failure is on a master-only reporting job (`publish-docs`, `report-grc`) that does
  not gate merges — surface but do not fix unless asked
- The failure is on `report-codecov` — it runs on every branch and can make the workflow
  run appear failed in PR checks even when branch protection does not gate on it. Surface
  it; investigate only if the user asks, or if the same failure reproduces across
  consecutive runs (a real upload or config regression rather than a transient)
- CI is failing _only on master_ after a merge — something slipped past review;
  surface immediately, never push an autonomous fix to master
- The same fix would require editing files outside the current branch's scope — stop and
  ask

**What escalation looks like**: surface to the user with (a) the failing job(s), (b) the
root-cause hypothesis, (c) what has been tried, (d) why further attempts are not
confidence-building.

---

## Phase 5: When CI is Green

- If this was a push to a feature branch without a PR → hand off to `open-pull-request`
  if the user wants to open one
- If this was a push to an open PR → surface the all-green state and stop. Merging is a
  developer decision unless explicitly delegated.
- Never merge autonomously. Never add `--auto-merge` flags without an explicit instruction.

---

## Safety Rules

- **Never push directly to `master` or `main`** — even for a trivial CI fix. All fixes go
  via the PR branch.
- **Never `gh workflow run` or `gh run cancel`** without explicit user permission — those
  affect shared CI state.
- **Never skip pre-commit hooks** (`--no-verify`) to make CI pass. If a local hook blocks
  a commit, the same check will block CI; fix the underlying issue.
- **Never `gh pr merge`** unless the user explicitly says to merge.
- **Never edit `.github/workflows/*.yaml` to silence a failure** — if a check is genuinely
  obsolete, that's a separate conversation with the user.
- **Never autonomously re-run a failing job more than twice.** Beyond that, the failure
  is not a flake.

---

## Quick Reference

| Situation                             | Command                                                                             |
| ------------------------------------- | ----------------------------------------------------------------------------------- |
| Overall status (PR open)              | `gh pr checks --watch=false`                                                        |
| Overall status (no PR)                | `gh run list --branch <branch> --limit 1 --json databaseId,status,conclusion,name`  |
| List failing jobs in a run            | `gh run view <run-id> --json jobs --jq '.jobs[] \| select(.conclusion=="failure")'` |
| Read only failed-step logs of one job | `gh run view <run-id> --log-failed --job <job-id>`                                  |
| Narrow noisy logs                     | `... \| tail -n 200` or `... \| grep -E "FAIL\|Error" \| head -n 50`                |
| Rerun failed jobs only (flake retry)  | `gh run rerun <run-id> --failed`                                                    |
| Wait for in-progress run              | Bash `sleep 90` with `run_in_background=true`, then re-check                        |

| CI Job         | Local fix target                   |
| -------------- | ---------------------------------- |
| `generated-go` | `make generate` + follow-up commit |
| `lint-go`      | `make lint-go`                     |
| `lint-proto`   | `make lint-proto`                  |
| `lint-node`    | `pnpm lint:ci`                     |
| `test`         | `make test-unit`                   |
| `test-pkg`     | `make test-pkg`                    |
| `test-pkg-js`  | `make test-pkg-js`                 |
| `build-js`     | `pnpm -C pkg/js build:rest`        |
| `build-*` (Go) | `go build ./...`                   |

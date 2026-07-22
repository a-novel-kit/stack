---
name: monitor-ci
description: >
  Monitor GitHub Actions CI runs on a pushed branch or open PR, classify failures, and fix them
  where safe. Use whenever waiting on CI, investigating a failing check, judging a failure real or
  flaky, or iterating a branch to green. Covers the Agora CI job map and the retry/fix loop.
  Pairs with open-pull-request (the post-push handoff) and git-conventions.
---

# Monitor CI

CI is the final gate before review, and every failure must be resolved. It is also long-running
and noisy, and unstructured polling burns context, so this skill fixes which commands to run, how
often, and how to act on each failure type.

The loop is **observe → classify → fix → re-push → re-observe**, with a retry budget. When
the budget runs out, stop and escalate.

---

## CI Job Map (typical Agora service repo)

The `main` workflow on Agora service repos under `a-novel/service-*` runs on every push to any
branch. The job set varies by repo; the table below is the common surface across those services.
For the jobs on the current checkout, run `gh pr checks <n>` (or read
`.github/workflows/main.yaml`) and intersect with the rows below; anything unlisted is
repo-specific or downstream of a base table entry.

| CI Job                        | What it checks                             | Local equivalent              | Typical failure                                                              |
| ----------------------------- | ------------------------------------------ | ----------------------------- | ---------------------------------------------------------------------------- |
| `generated-go`                | `go generate ./...` is up to date          | `pnpm generate:go`            | Forgot to run `pnpm generate:go` after proto/interface                       |
| `lint-go`                     | `golangci-lint run` clean                  | `pnpm lint:go`                | New Go code violates style or has a bug                                      |
| `lint-proto`                  | `buf lint` clean                           | `pnpm lint:proto`             | Proto file violates buf style                                                |
| `lint-node`                   | `pnpm lint:ci` clean                       | `pnpm lint:ci`                | JS/TS code violates eslint/prettier                                          |
| `test-go`                     | Go unit tests in `/internal`               | `a-novel test --type=go -y`   | Broken Go code or test                                                       |
| `test-pkg`                    | Go integration tests in `/pkg/go`          | `a-novel test --type=go -y`   | gRPC contract mismatch OR flake                                              |
| `test-pkg-js`                 | JS integration tests in `/pkg/js`          | `a-novel test --type=pnpm -y` | REST contract mismatch OR flake                                              |
| `build-database`              | Docker build for Postgres image            | `a-novel build --type=podman` | Dockerfile error, bad init script                                            |
| `build-migrations`            | Docker build for migrations job            | (none)                        | Migration file issue                                                         |
| `build-job-rotate-keys`       | Docker build for rotate-keys job           | (none)                        | Go build error in cmd/rotatekeys                                             |
| `build-grpc`                  | Docker build for gRPC service image        | (none)                        | Go build error                                                               |
| `build-standalone-grpc`       | Docker build for standalone gRPC dev image | (none)                        | Go build error                                                               |
| `build-rest`                  | Docker build for REST service image        | (none)                        | Go build error                                                               |
| `build-standalone-rest`       | Docker build for standalone REST dev image | (none)                        | Go build error                                                               |
| `build-js`                    | `pnpm build:rest` for pkg/js               | `pnpm -C pkg/js build:rest`   | TS compile error or broken export                                            |
| `report-grc` / `publish-docs` | Post-success reporting, **master only**    | (none)                        | Rarely actionable; usually transient                                         |
| `report-codecov`              | Coverage upload, runs on **every branch**  | (none)                        | Upload failure can still mark the run failed in PR checks; usually transient |

`test-go` blocks most application `build-*` jobs (`build-grpc`, `build-rest`,
`build-standalone-*`, `build-job-rotate-keys`); when it fails they are cancelled, so fix `test-go`
first. `build-database` does **not** depend on `test-go`, and `build-migrations` depends only on
`build-database`, so failures in those two surface independently and need their own diagnosis.

Check contexts are lane-suffixed (`test-go`, `lint-node`, …); `write-github-actions` owns that rule
and the reasons for it. A repo not yet migrated may still emit a bare `test`.

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

`gh run watch` blocks the terminal until the run finishes, which in a Claude session blocks the
whole turn and burns context on a 10+ minute run.

Use the Bash tool's `run_in_background` parameter for the wait, then re-check:

```bash
# Start a background sleep matched to expected remaining time.
# Typical CI here takes ~8–12 min end-to-end; short jobs finish in ~2 min.
sleep 90
```

Run that with `run_in_background=true`, then on the next turn issue `gh pr checks` (or the
`gh run list` command above) to get the updated state. Repeat until the run is `completed`.

#### Use the wait window for a self-review

Spend the wait reviewing your own work, so issues are caught and fixed before a reviewer sees
them. While a background wait is in flight, read the branch's own diff and check it critically:

```bash
git diff master...HEAD          # or the stacked parent branch
```

Look for: leftover debug/print statements, commented-out code, unresolved TODOs, missing or
thin test coverage for the changed lines, error paths that don't report (see the
every-span'd-layer rule), naming/layering drift from the relevant `write-*` skill, and
anything the PR body claims but the diff doesn't do.

- A clear defect is fixed like any CI finding (Phase 3): local-verify, follow-up commit,
  push. The push starts a fresh run, so the self-review and the CI loop converge.
- Something arguable (a design trade-off, a deferred concern) is surfaced, not silently
  rewritten: note it and raise it in the final CI report so the user decides.

Do the self-review once per branch, not on every poll; after a fix commit re-review only
the new diff. Keep it scoped to `git diff` — this reviews the change, not the whole repo.

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

That lists the failing jobs by name and ID, the minimum needed to decide what to fix.

### 1.4 Read ONLY the failed step logs

The full run log is huge and floods context. Always use `--log-failed` to get only the
failing steps:

```bash
gh run view <run-id> --log-failed --job <job-id>
```

If `--log-failed` is still too large (thousands of lines for a test crash), narrow it
with `tail` or `grep`:

```bash
gh run view <run-id> --log-failed --job <job-id> | tail -n 200
gh run view <run-id> --log-failed --job <job-id> | grep -E "FAIL|Error|error:" | head -n 50
```

Never read the full run log unprefiltered. Never fetch logs for passing jobs.

---

## Phase 2: Classify

Map the failed-step log to one of these categories; the category determines the fix path.

### 2.1 `generated-go` failure

**Symptom**: job fails with a message like `go generate definitions are not up-to-date`.

**Root cause**: a `.proto` file or Go interface (used by a mock) changed without
`pnpm generate:go` being run afterward.

**Fix**: the original commit is already pushed and `git-conventions` forbids amending
pushed history, so the regenerated files land as their own follow-up:

```bash
pnpm generate:go
git status --porcelain
git add internal/handlers/protogen/ internal/handlers/mocks/ internal/core/mocks/
git commit -m "chore(gen): regenerate Go bindings for <scope>"
git push
```

That splits the proto/interface change and its regen across two commits, the cost of
noticing after push. The "generated files belong in the same commit" guidance in
`git-conventions` is a structure preference; the "never amend a pushed commit" rule is
categorical and wins here.

### 2.2 `lint-go` / `lint-proto` / `lint-node` failure

**Symptom**: linter reports specific files + line numbers.

**Fix**: run the matching local script, read its output, edit the flagged files, re-run
until clean, then commit:

```bash
pnpm lint:go        # or lint-proto / lint-node
# edit flagged files
pnpm lint:go        # re-run to confirm clean
git add <files>
git commit -m "fix(<scope>): resolve lint findings"
git push
```

Use a `fix(<scope>): resolve lint findings` commit for trivial mechanical changes and a
`refactor` or `fix` commit for invasive rewrites. `git-conventions` forbids amending pushed
commits unconditionally, so a noisy `fix(lint): ...` follow-up is the right call; under
squash-merge the PR author squashes or absorbs it at merge time.

### 2.3 `test-go` (Go unit) failure

**Symptom**: `--- FAIL: TestXxx` in the log, optionally a stack trace.

**Fix**:

1. Reproduce locally first — never fix blind:

   ```bash
   # Run just the failing package and test for fast iteration
   go test ./internal/<package>/... -run TestXxx -v
   # Or the full suite if multiple tests fail
   a-novel test --type=go -y
   ```

2. Decide from the failure: **is the test wrong, or is the code wrong?**
   - New test for behaviour the code doesn't yet implement → fix the code
   - Existing test that used to pass → fix the new code that broke it
   - Test assertion out of date vs. new intended behaviour → fix the test
   - Follow `write-go-service` / `write-go-tests` for the actual fix

3. Re-run until green locally, then commit. This skill always runs on an already-pushed
   branch, so `git-conventions`' "never amend a pushed commit" rule applies unconditionally:
   - A fix belonging with the feature: follow-up commit on the branch, collapsed by
     squash-merge at PR merge time (if configured).
   - A genuine separate fix: new `fix(<scope>)` commit.

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
   a-novel test --type=go -y       # starts gRPC standalone
   a-novel test --type=pnpm -y    # starts REST standalone
   ```

2. The failure usually means a contract mismatch between handler and client:
   - `test-pkg` failing → gRPC handler vs. `pkg/go` client drift — check `write-proto`
   - `test-pkg-js` failing → REST handler vs. `openapi.yaml` vs. `pkg/js/rest/` drift —
     all three must match, see `implement-feature`'s OpenAPI / REST / JS sync rule

3. Fix the out-of-date side, re-run until green, commit, push.

### 2.5 `build-*` (Docker) failure

**Symptom**: `docker build` step fails. Root causes:

- **Go compilation error** in the image's entrypoint binary → the real failure is in Go
  source; fix via Phase 2.3 approach (edit, `go build ./...`, commit). The `build-*`
  failure is a downstream symptom.
- **Dockerfile syntax / COPY path wrong** → follow `write-dockerfiles` to fix
- **Base image pull failure** → usually transient; retry with `gh run rerun --failed`
- **Migration init script failure** (`build-database`, `build-migrations`) → follow
  `write-sql` for migration fixes

Always read the `--log-failed` output before guessing. `undefined: Foo` or `type Bar has no
field Baz` is a Go source issue, not a Dockerfile one.

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

1. Re-run the relevant local target to confirm green (`a-novel test --type=go -y`, `pnpm lint:go`,
   `pnpm generate:go`, etc.)
2. Create a new fix commit per `git-conventions`. Do not amend or rewrite history on
   this already-pushed branch — an amend strands CI run logs and review threads anchored
   to the old SHAs.
3. Push. A push to the branch automatically triggers a new CI run.
4. Return to Phase 1.

Never push a fix without local verification. CI confirms the fix; it is not the test runner.

---

## Phase 4: Retry Budget and Escalation

**Retry budget**: at most **3 fix attempts for the same root cause**, then stop and escalate to
the user. A test still failing after two real fixes means the diagnosis is wrong, and more
guesses waste time.

**Flake retry budget**: at most **2 reruns via `gh run rerun --failed`** for the same
suspected-flaky job before treating it as real. A `test-pkg-js` that fails all three times with
"connection refused" — the original run plus both reruns — is a genuine problem (container startup
regression, service crash at boot); switch to Phase 2.4 "real" investigation.

**Escalate immediately (do not spend retry budget) when:**

- The failure involves a secret or credential (never debug secrets autonomously)
- The workflow file itself is failing to parse (YAML error) — check with the user before
  editing workflow files
- The failure is on a master-only reporting job (`publish-docs`, `report-grc`) that does
  not gate merges — surface but do not fix unless asked
- The failure is on `report-codecov` — it runs on every branch and can make the run appear
  failed in PR checks even when branch protection does not gate on it. Surface it; investigate
  only if the user asks, or if it reproduces across consecutive runs (a real upload or config
  regression rather than a transient)
- CI is failing _only on master_ after a merge — something slipped past review;
  surface immediately, never push an autonomous fix to master
- The same fix would require editing files outside the current branch's scope — stop and
  ask

**What escalation looks like**: surface (a) the failing job(s), (b) the root-cause
hypothesis, (c) what has been tried, (d) why further attempts are not confidence-building.

---

## Phase 5: When CI is Green

- Push to a feature branch without a PR → hand off to `open-pull-request` if the user
  wants one opened
- Push to an open PR → surface the all-green state and stop. Merging is a developer
  decision unless explicitly delegated (see Safety Rules).

This green report is the **terminal state that completes the task** when CI was reached
via `open-pull-request` Phase 7 — that task is not done until you have reported it (or,
per Phase 4, an escalated/blocked state instead). Include anything the Phase 1.2
self-review surfaced that you did not fix yourself.

---

## Safety Rules

- **Never push directly to `master` or `main`** — even for a trivial CI fix. All fixes go
  via the PR branch.
- **Never `gh workflow run` or `gh run cancel`** without explicit user permission — those
  affect shared CI state.
- **Never skip pre-commit hooks** (`--no-verify`) to make CI pass. A local hook that blocks
  a commit blocks CI too; fix the underlying issue.
- **Never `gh pr merge`**, and never add an `--auto-merge` flag, unless the user explicitly
  says to merge.
- **Never edit `.github/workflows/*.yaml` to silence a failure** — a genuinely obsolete check
  is a separate conversation with the user.
- **Never autonomously re-run a failing job more than twice** (the Phase 4 flake retry budget).

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

| CI Job         | Local fix target                      |
| -------------- | ------------------------------------- |
| `generated-go` | `pnpm generate:go` + follow-up commit |
| `lint-go`      | `pnpm lint:go`                        |
| `lint-proto`   | `pnpm lint:proto`                     |
| `lint-node`    | `pnpm lint:ci`                        |
| `test-go`      | `a-novel test --type=go -y`           |
| `test-pkg`     | `a-novel test --type=go -y`           |
| `test-pkg-js`  | `a-novel test --type=pnpm -y`         |
| `build-js`     | `pnpm -C pkg/js build:rest`           |
| `build-*` (Go) | `go build ./...`                      |

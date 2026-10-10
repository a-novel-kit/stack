---
name: monitor-ci
description: >
  Observe GitHub Actions, diagnose failing checks, fix scoped defects, and retry confirmed flakes
  within budget. Load after pushes, when investigating or waiting on CI, or to sweep dependency PRs.
---

# Monitor CI

Load [develop-feature](../develop-feature/SKILL.md) for stage timing. CI validates both draft and
ready PRs; green checks alone do not approve issue scope or finish testing and cleanup. Preserve
existing checks, diagnose failures, and defer new broad coverage until scope approval.

While CI runs, use `resolve-pr-feedback` to check linked issue discussions and PR feedback together.
This skill owns observation, diagnosis, and retries; the development skill owns readiness.

The loop is **observe → classify → fix → re-push → re-observe**, with a retry budget. When
the budget runs out, stop and escalate.

---

## Choose the next action

1. Identify the current branch/PR commit and its actual checks. Use the observation loop below.
2. If checks are pending, review the diff once and inspect available feedback while waiting.
3. For a failure, read the failed step and the matching section of
   [failure diagnosis](references/failures.md) before choosing a fix or retry.
4. Use [the service job map](references/job-map.md) when interpreting an unfamiliar service job;
   the current repository workflow is the source of truth.
5. For a sweep of Renovate or Dependabot PRs, read [dependency PRs](references/dependency-prs.md)
   before classifying or landing them.
6. Re-run the affected verification, preserve history, and observe the new commit. Stop at green
   or the escalation conditions below; green CI does not authorize a merge.

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
- `completed` + `cancelled` → superseded, timed out, or stopped by hand; read why before retrying
- `completed` + `skipped` → a job it `needs:` failed or skipped, so fix that job; on a green
  run, only jobs with an `if:` skip

### 1.2 Polling pattern — do NOT use `gh run watch`

`gh run watch` blocks the terminal until the run finishes, which in a Claude session blocks the
whole turn and burns context on a 10+ minute run.

Use the Bash tool's `run_in_background` parameter for a short wait, then re-check:

```bash
# Keep verification responsive; the next status check should happen quickly.
sleep 5
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
thin coverage for important behavior (record broad additions for stage 3 on a draft), error paths
that don't report (see the every-span'd-layer rule), naming/layering drift from the relevant
`write-*` skill, and
anything the PR body claims but the diff doesn't do.

- A clear defect is fixed like any CI finding (Phase 3): local-verify, follow-up commit,
  push. The push starts a fresh run, so the self-review and the CI loop converge.
- Something arguable (a design trade-off, a deferred concern) is surfaced, not silently
  rewritten: note it and raise it in the final CI report so the user decides.

Do the self-review once per branch, not on every poll; after a fix commit re-review only
the new diff. Keep it scoped to `git diff` — this reviews the change, not the whole repo.

Rule of thumb for sleep durations:

- First check after push: `5s` — short jobs (lint, generated-go) often complete by then
- Still in progress: `5s` — keep verification responsive and catch failures quickly
- Known long wait (test-pkg-js after cold image pulls): `10s` — a small backoff, not a reason to
  block the turn for a minute or more

Prefer `5s` polling for CI status checks. If the same run remains unchanged across several
polls, back off to `10s`; do not default to `30s`, `90s`, or longer sleeps. A short poll is
especially important when the next action depends on verification completing.

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

This report completes the CI check required by `open-pull-request` Phase 7. A draft may still
await scope approval, full relevant coverage, or cleanup under `develop-feature`; report those
remaining steps accurately. Include any unresolved findings from the Phase 1.2 self-review.

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
| Wait for in-progress run              | Bash `sleep 5` with `run_in_background=true`, then re-check                         |

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

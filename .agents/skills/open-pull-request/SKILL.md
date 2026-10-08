---
name: open-pull-request
description: >
  Publish branches and create or update PRs with correct bases, metadata, readiness, and handoff.
  Load when shipping changes; use monitor-ci through the checks' conclusion.
---

# Open Pull Request

Load [develop-feature](../develop-feature/SKILL.md) before publishing. Keep an exploratory draft
local until direction agreement, unless publication was explicitly requested. After agreement,
feature-branch pushes and draft PRs within scope need no repeated permission. Clear, simple tasks
may take the shorter path defined there.

Issue discussion and draft-PR review can run together. Keep the PR draft while scope approval,
full relevant tests, or final cleanup remain outstanding. Drafts follow repository safeguards and
history rules. Never push to `master`/`main` without explicit consent (`git-conventions`).

Every PR in this repo follows the same contract: Conventional-Commits title, structured
body, correct base, and no manual reviewer/assignee assignment (workflows handle that).

---

## Choose the publication path

Use the checks and stage rules below before publication. Inspect the existing branch/PR first;
update an open PR rather than recreating it.

- Before creating or editing a PR, changing draft/ready status, or mirroring tracking metadata,
  read [authoring and metadata](references/authoring.md).
- After every push to an existing draft PR, recheck its draft reason using the
  [readiness rule](references/authoring.md#61-flip-a-draft-to-ready-the-moment-it-qualifies-mandatory).
  Mark it ready in the same turn once all applicable completion gates pass.
- Before a final code/issue handoff, read [the session recap contract](references/handoff.md).
  Include every outstanding session item and each PR's admin-only approval command after review.
- After a push, use `monitor-ci` through green or a documented escalation, and inspect feedback
  with `resolve-pr-feedback`. Publication alone does not complete the task.

Local rendered-UI links belong in the handoff, never the PR body. Keep all approval, readiness,
identity, history, and layer-specific verification rules when taking a shorter publication path.

## Phase 1: Pre-Flight Checks

Before any push or PR creation, confirm that `develop-feature` permits publication and apply the
stage-appropriate checks below. Fix failures within scope; surface genuine blockers.

### 1.1 You are on a feature branch

```bash
git rev-parse --abbrev-ref HEAD
```

Must return something like `feat/dao/revoke-keys`, not `master` or `main`. If on `master`,
stop — you do not open PRs from master.

### 1.2 Working tree is clean

```bash
git status --porcelain
```

Must return empty. Uncommitted changes mean the branch is not ready. Either commit them
(follow `git-conventions`) or surface them to the user.

### 1.3 Commits follow Conventional Commits

```bash
# <base> is master, or the parent feature branch for a stacked PR (see Phase 2.3):
# master there would pull in the parent's commits and validate/rewrite commits that
# aren't this branch's responsibility.
# %s emits the commit subject only — no hash prefix — so each line is directly
# comparable against the Conventional Commits grammar below.
git log <base>..HEAD --format=%s
```

Every line must parse as a `git-conventions`-compliant Conventional Commit: either
`<type>(<scope>): <description>` or, for genuinely cross-cutting commits where scope is
intentionally omitted, `<type>: <description>`. If any commit is malformed, fix it
**before the branch's first push**. Use `git commit --amend` only when the malformed
commit is the last one on the branch _and_ unpushed; once the branch is pushed,
`git-conventions`' "never amend a pushed commit" rule applies and the fix becomes a
follow-up commit (or, for a cosmetic title fix, a PR-title adjustment the author can
squash at merge time). For any earlier malformed commit — even if unpushed — ask the
user before rewriting history.

### 1.4 Validate for the current stage

**Draft PR:** run focused existing checks and the lint/build checks needed to validate the changed
path. Describe new regression coverage deferred until issue scope approval. Do not demand the full
test suite to publish an agreed draft, disable CI, or weaken existing assertions.

**Ready PR:** after scope approval, complete and run the full relevant regression suite, review
coverage, and finish code cleanup. Client-side changes include affected Playwright tests and
reviewed screenshot comparisons. Run applicable lint, type, test, and build checks; skip targets
the change cannot affect. CI confirms local verification.

```bash
# 1. LINT — pnpm scripts (lint/format/generate are NOT a-novel CLI verbs):
pnpm lint:go            # Go (golangci-lint); pnpm lint:proto for .proto, pnpm lint for JS/TS

# 2. TESTS — a-novel CLI, narrowest target that covers the branch's layer
#    (see implement-feature for the layer-to-target mapping):
a-novel test --type=go -y       # Go internal + pkg/go
a-novel test --type=pnpm -y     # pkg/js
a-novel test -y                 # everything (final pre-push validation)

# 3. BUILD — a-novel CLI, only the artifact kinds your change can break:
a-novel build --type=go -y      # Go binaries
a-novel build --type=pnpm -y    # pnpm build
a-novel build --type=podman -y  # images — only if you touched builds/ or what they copy
```

**Lint is not formatting.** `gofmt` / `gofumpt` / `gci` (and `pnpm format:go`) only check
_formatting_ — they do **not** run the linters CI enforces (`goconst`, `usestdlibvars`,
`errcheck`, `gocritic`, …). A format-clean diff can still fail the `lint-go` check, so run
the linter, not just the formatter. Where a repo exposes no local Go-lint runner (no
`pnpm lint:go` script), invoke golangci-lint directly from the module directory:

```bash
go tool -modfile=golangci-lint.mod golangci-lint run ./...
```

That form resolves the version the repo pins, so local and CI run the identical linter. Pulling
`@latest` instead invites a disagreement that is pure version drift.

Fix failures in the applicable checks before pushing; disclose any verification blocker.

### 1.5 Generated files are in sync

If the branch touches `.proto` files or Go interfaces that have mocks:

```bash
pnpm generate:go
git status --porcelain
```

Any newly-modified files under `internal/handlers/protogen/`, `internal/handlers/mocks/`, or
`internal/core/mocks/` belong with the source change that caused them. Before the branch's
first push, amend them into the relevant commit. Once pushed, do not rewrite published
history — add a follow-up `chore(gen): ...` commit instead (per `monitor-ci`). CI's
`generated-go` job fails when these are stale.

### 1.6 Layer-specific review surfaces are ready

Honor every active layer skill's review-artifact gate before opening a ready PR. In particular,
`write-frontend` requires a locally running, freshly inspected Storybook plus direct story links
ready for the final completion report, and screenshots of the changed screens sent in the
conversation. Local-only review links must not enter the PR body. A
screenshot, static build, stale URL, or Storybook root link does not satisfy a direct rendered-UI
review route.

---

## Phase 2: Push the Branch

### 2.1 First push — set upstream

```bash
git push -u origin $(git rev-parse --abbrev-ref HEAD)
```

`-u` sets the upstream so later `git push` / `git pull` need no arguments.

### 2.2 Subsequent push

```bash
git push
```

If the branch has been rebased (e.g., during backtracking — see `implement-feature` Phase 4),
force-push **with lease** to avoid clobbering anyone else's work:

```bash
git push --force-with-lease
```

Never use plain `--force`. Never force-push to `master` or `main`.

### 2.3 Stacked branches

If this branch depends on another open PR (e.g., `feat/core/jwk-revoke` depends on
`feat/dao/jwk-revoke`), the base of the PR must be the parent branch, not master. Push the
parent first and make sure its PR is open.

---

## Phase 3: Decide PR Status (Ready vs. Draft)

Open a **draft** after direction agreement when any of these remain:

- Issue scope approval or a blocking design discussion.
- Full relevant test coverage, Playwright/screenshot verification, or final cleanup.
- A required layer, documentation, or stacked dependency still being completed.
- An explicit developer request for draft/WIP or early directional review.

Review feedback on the issue and the draft PR in parallel with `resolve-pr-feedback`. Draft does
not mean feedback should wait. Open **ready for review** only once the applicable
`develop-feature` completion gates pass. Clear, finished work need not be held in draft.

```bash
gh pr create --draft ...   # draft
gh pr create ...           # ready for review
```

---

## Phase 4: Check for Existing PR

Before creating a new PR, check whether one already exists for this branch:

```bash
gh pr view --json number,state,url 2>/dev/null
```

- If it returns a PR in `OPEN` state → **do not** create a new one. Update it instead
  (see Phase 6).
- If it returns a PR in `CLOSED` or `MERGED` state → the branch was reused. Surface this
  to the user before doing anything else; they probably want a new branch.
- If the command exits non-zero ("no pull requests found") → proceed to Phase 5.

---

## Phase 7: Hand-Off to monitor-ci (mandatory — gates task completion)

After `gh pr create` or `git push` succeeds, CI starts. **Opening the PR does not
finish the task.** Invoke `monitor-ci` and follow it to a terminal state. The task is
complete only once you have reported one of:

- **CI green** — every gating check `completed` + `success`, reported to the user, OR
- **A blocked/escalated state** — `monitor-ci`'s retry budget exhausted or an escalate
  condition hit, with the failing job(s), root-cause hypothesis, and what was tried
  surfaced to the user (per `monitor-ci` Phase 4).

Never end the turn at "PR opened, CI running" — that leaves the result unverified. Carry
the CI watch to a reported conclusion before closing out.

While CI runs, inspect both linked issue discussions and PR feedback with `resolve-pr-feedback`.
Use the wait windows for a self-review of the branch's diff (`monitor-ci` Phase 1.2); full coverage
and readiness still depend on the current `develop-feature` stage. Green CI on a draft does not
approve its scope or complete the task. If no checks apply or are configured, report that accurately.

Do not merge — merges are a developer decision unless explicitly delegated.

---

## Common Mistakes

- **Publishing before draft agreement.** Local checkpoint commits stay local unless publication
  was explicitly requested. After agreement, do not ask again for already-authorized publication.
- **Treating a green draft as ready.** Scope approval, full relevant coverage, visual verification,
  and final cleanup must be complete before the ready transition.
- **Ending a code/issue turn without the recap table.** Close with the Phase 8 table linking
  everything still outstanding this session, with the admin-only approval command inside each open
  PR's row rather than in adjacent prose.
- **Trying to author a PR as the bot.** There is no bot path — `a-novel core bot-comment`
  only posts comments (5.0).
- **Treating "PR opened" as task-done.** Carry `monitor-ci` through to CI green or an
  escalated/blocked state (Phase 7).
- **Skipping stage-appropriate verification.** Apply 1.4, keep existing checks intact, and record
  deferred coverage on drafts; the formatter is not the linter.
- **Opening a PR from master.** Branch first, then PR.
- **Closing and re-creating a PR to "fix" the title.** Use `gh pr edit --title` instead.
- **Putting a local Storybook link in a PR body.** Local review URLs are session-scoped and belong in
  the completion report, not durable GitHub metadata.
- **Handing off completed UI work without Storybook beside the PR and task links.** The final report
  needs a freshly verified direct Storybook link; see `write-frontend`.
- **Manual reviewer/assignee flags.** Automation handles these (5.4); tracking metadata is
  the exception a ready PR carries (5.5).
- **`--force` without `--lease`.** Always `--force-with-lease` after a rebase.
- **Missing `BREAKING CHANGE:` footer.** If any commit on the branch is breaking, the PR
  body's Breaking Changes section must list it. Mismatches between commits and PR body are
  bugs — readers trust the body.
- **Bare `Closes #<n>` for a planning issue in a `.github` repo.** It links nothing, so the
  issue freezes on the board. Use `Closes a-novel-kit/.github#<n>` (5.3).
- **PR title that does not match the primary commit scope.** If the branch is `feat/dao/*`,
  the PR title scope should be `dao`, not `services`.
- **Linking the wrong base branch on a stacked PR.** If the parent is already merged, rebase
  onto master and change the base to master before pushing.

---

## Quick Reference

| Situation                           | Command                                                                                                    |
| ----------------------------------- | ---------------------------------------------------------------------------------------------------------- |
| Pre-flight: lint (scoped)           | `pnpm lint:go` / `go tool -modfile=golangci-lint.mod golangci-lint run ./...`                              |
| Pre-flight: tests (scoped)          | `a-novel test --type=go -y` / `a-novel test -y`                                                            |
| Pre-flight: build (scoped)          | `a-novel build --type=go -y` (`--type=` matches changes)                                                   |
| First push                          | `git push -u origin <branch>`                                                                              |
| Push after rebase                   | `git push --force-with-lease`                                                                              |
| Check for existing PR               | `gh pr view --json number,state,url`                                                                       |
| Create ready PR                     | `gh pr create --title "..." --body "$(cat <<'EOF' ... )"`                                                  |
| Create draft PR                     | `gh pr create --draft --title ...`                                                                         |
| Close a cross-repo planning issue   | PR body: `Closes a-novel-kit/.github#<n>` (see 5.3)                                                        |
| Stacked PR (base is another branch) | `gh pr create --base feat/<parent-area>/... ...`                                                           |
| Update title on existing PR         | `gh pr edit --title "..."`                                                                                 |
| Flip draft → ready                  | `gh pr ready`                                                                                              |
| Flip ready → draft                  | `gh pr ready --undo`                                                                                       |
| Admin approval after review         | `gh workflow run approve-pr.yaml --repo <org>/<repo> --ref <default-branch> --field pull_request=<PR-URL>` |

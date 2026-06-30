---
name: open-pull-request
description: >
  Push the current branch and open a GitHub pull request for Agora backend services.
  Use this skill whenever preparing to ship work — new features, fixes, refactors, chores.
  Covers pre-flight checks, base branch selection, title and body formation, draft mode,
  and updating an existing PR instead of re-creating it. Pairs with git-conventions for
  commit/branch format and monitor-ci for post-push CI handling.
---

# Open Pull Request

This skill governs how Claude pushes a branch and opens a pull request. Opening a PR is a
publishing action — it notifies reviewers, triggers CI, and creates visible history. Never
open one without the user's explicit go-ahead, and never open one from a branch that is not
ready for review.

Every PR in this repo follows the same contract: Conventional-Commits title, structured
body, correct base, and no manual reviewer/assignee assignment (workflows handle that).

---

## Phase 1: Pre-Flight Checks

Before any push or PR creation, verify all of the following. If any check fails, stop and
surface the problem to the user rather than pushing.

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
# Replace <base> with master for a normal branch, or with the parent feature branch
# name for a stacked PR (see Phase 2.3). Using master for a stacked branch would
# include the parent's commits and validate/rewrite commits that aren't this
# branch's responsibility.
# %s emits the commit subject only — no abbreviated hash prefix — so each output
# line is directly comparable against the Conventional Commits grammar below.
git log <base>..HEAD --format=%s
```

Every line must parse as a `git-conventions`-compliant Conventional Commit: either
`<type>(<scope>): <description>` or, for genuinely cross-cutting commits where scope is
intentionally omitted, `<type>: <description>`. If any commit is malformed, fix it
**before the branch's first push**. Use `git commit --amend` only when the malformed
commit is the last one on the branch _and_ it has not yet been pushed; after the branch
is pushed, `git-conventions`' "never amend a pushed commit" rule applies and the fix
becomes a follow-up commit instead (or, for a cosmetic title fix, a PR-title adjustment
the author can squash at merge time). For any earlier malformed commit — even if
unpushed — ask the user before rewriting history.

### 1.4 Basic CI passes locally

**Always run basic CI locally before pushing.** CI is not a debugger: a red push wastes a
CI cycle and reviewer attention. Never push expecting "CI will tell me what's wrong".

**Basic CI = lint + tests + build.** Scope each to what the branch actually changed — run
the checks for the layers/languages you touched, and skip the ones nothing you changed can
affect (e.g. don't rebuild the Dockerfile images if you touched no `builds/` file or the
file structure they copy; don't run pnpm tests for a Go-only change).

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
the linter, not just the formatter. If a repo exposes no local Go-lint runner (some repos
have no `pnpm lint:go` script), invoke golangci-lint directly so you still catch what CI
will, from the module directory:

```bash
go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest run ./...
```

If lint, tests, or build fail locally, CI will fail too. Fix before pushing.

### 1.5 Generated files are in sync

If the branch touches `.proto` files or Go interfaces that have mocks:

```bash
pnpm generate:go
git status --porcelain
```

Any newly-modified files under `internal/handlers/protogen/`, `internal/handlers/mocks/`, or
`internal/core/mocks/` belong with the source change that caused them. Before the
branch's first push, amend them into the relevant commit. After the branch has been
pushed, do not rewrite published history — add a follow-up `chore(gen): ...` commit
instead (per `monitor-ci`). CI has a `generated-go` job that will fail if these are
stale.

---

## Phase 2: Push the Branch

### 2.1 First push — set upstream

```bash
git push -u origin $(git rev-parse --abbrev-ref HEAD)
```

The `-u` flag sets the upstream so subsequent `git push` / `git pull` need no arguments.

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
parent first and make sure its PR is already open.

---

## Phase 3: Decide PR Status (Ready vs. Draft)

Open as **draft** when any of these apply:

- The developer explicitly said "draft" or "WIP"
- The branch is one of several stacked branches still being built — only the tip branch
  is typically ready for review
- The branch intentionally omits tests, docs, or a related layer that is coming later
- The developer wants early feedback on direction before committing to a full review

Otherwise open as **ready for review** (default).

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

## Phase 5: Create the PR

### 5.0 PR authoring runs as the operator — the bot can only comment

`gh pr create` (and every `gh pr edit` / `gh pr ready` in this skill) runs with the
plain `gh` credential, i.e. the operator's **user token**. The opened PR is authored by
the operator, so the `auto-assign-author` workflow can assign them and `CODEOWNERS`
routing works. PR authoring is never a bot action.

You cannot author a PR as the bot, by construction. There is no local bot token, and the
only bot entry point — `a-novel core bot-comment <org> <repo> <number> --body …` — does
exactly one thing: trigger the centralized dispatcher workflow, which _posts a comment_
and nothing else. So `pr create|edit|ready|merge|close` are always operator actions;
commenting (top-level PR/issue comments and review-thread replies in `resolve-pr-feedback`)
is the only thing that ever attributes to `<app-slug>[bot]`. See [[feedback-bot-attribution]].

```bash
# Author the PR as yourself (operator user token).
gh pr create ...
```

If a `gh pr create`/`edit`/`ready` call fails with an auth/permission error, surface it
to the user — there is no bot fallback to route around it.

### 5.1 Choose the base branch

- Default: `master`
- Stacked: the parent feature branch (e.g., `feat/dao/jwk-revoke`)

Pass the base explicitly with `--base` when it is not `master`:

```bash
gh pr create --base feat/dao/jwk-revoke ...
```

### 5.2 Title

The title is a Conventional-Commits line matching the primary commit on the branch. Under
70 characters. No period.

```
feat(dao): add soft-delete repository for key revocation
```

When the branch has multiple commits touching one scope, use the scope that best describes
the branch's goal. When the commits are genuinely cross-cutting (rename across layers), omit
the scope.

### 5.3 Body

Use this template, passed via HEREDOC to preserve formatting. Skip sections that do not
apply — do not write "no changes" placeholders.

```bash
gh pr create --title "feat(dao): add soft-delete repository for key revocation" --body "$(cat <<'EOF'
## Summary

- Adds `PgJwkRevoke` DAO for marking keys as revoked.
- Returns `ErrJwkRevokeNotFound` when the target is already revoked or expired.

## Layers changed

- **DAO**: new `pg.jwkRevoke.go` + test; sentinel error added.

## Breaking changes

None.

## Test plan

- [x] `a-novel test --type=go -y` passes
- [ ] CI green
EOF
)"
```

Rules:

- **Summary** is 1–3 bullets describing what changed _and why_. Readers see the diff; they
  need the intent.
- **Layers changed** lists only the layers actually touched. Omit the section entirely if
  only one layer is affected and the title already conveys it.
- **Breaking changes** is either `None.` or an itemized list with migration steps. Never
  leave this section as "TBD" or blank — reviewers should not have to hunt.
- **Test plan** is a checklist. Check the boxes you have already verified locally; leave
  `CI green` unchecked (monitor-ci will mark it).

**Writing style — rationale-dense, zero filler.** The body's job is what the diff cannot say:
why the change, what tradeoff was taken, what a reviewer should scrutinize. Never narrate the
diff — file lists, mechanical renames, and "updated X to Y" bullets restate what review tooling
already shows. Maximize meaning per word: every sentence either carries rationale or gets cut.
Exhaustive on decisions, silent on mechanics. The same bar applies to PR thread comments
(`resolve-pr-feedback`), where prose may lean more technical.

### 5.4 Do NOT pass these flags

- `--assignee` / `--reviewer` — the `auto-assign-author` workflow handles assignees; the
  repo decides reviewers via its `CODEOWNERS` file (at repo root) or team routing. Do not
  set reviewers manually unless the user explicitly requests a specific person; manual
  assignment duplicates or conflicts with that automation.
- `--label` — labels are derived from the title's Conventional-Commits type by downstream
  automation. Do not add them manually unless the user requests a specific one.
- `--milestone` / project board / **Priority** / **Size** / tracking labels — **not** left to humans:
  a **ready** PR mirrors the milestone, project board, its board fields (Priority, Size), and labels
  of the issue it closes. See [Tracking metadata](#55-tracking-metadata--match-the-linked-issue).

### 5.5 Tracking metadata — match the linked issue

A PR that is **ready for review** (not a draft) should be as trackable as the planning issue it
closes: add it to the org **"Tasks"** board and give it the **same milestone**, the **same board
fields (Priority, Size)**, and the relevant **tracking labels** as that issue — so a glance at the
board shows the work whether you look at the issue or its PR. A draft skips this; apply it when
opening ready, or at the **draft → ready** flip (Phase 6).

```bash
gh pr edit <n> --repo <org>/<repo> --add-label <label> --milestone "<milestone-title>"
gh project item-add <project-number> --owner <org> --url <pr-url>
# then mirror the issue's Priority and Size (single-select board fields — discover ids with
# `gh project field-list <num> --owner <org>`):
gh project item-edit --id <pr-item-id> --project-id <proj-id> --field-id <priority-field> --single-select-option-id <opt>
gh project item-edit --id <pr-item-id> --project-id <proj-id> --field-id <size-field>     --single-select-option-id <opt>
```

This is **tracking** metadata mirroring the issue — distinct from the type label automation derives
from the title, and from assignee/reviewer (still automation's job, see 5.4).

### 5.6 Capture the PR URL

The `gh pr create` command prints the PR URL on success. Surface it to the user in the
final message so they can jump to it.

---

## Phase 6: Updating an Existing PR

When a PR is already open for this branch and you need to change its metadata (not code):

```bash
# Change the title
gh pr edit --title "feat(dao): add revoke repository with soft-delete"

# Replace the body (use HEREDOC as in Phase 5.3)
gh pr edit --body "$(cat <<'EOF'
...
EOF
)"

# Flip from draft to ready — then apply tracking metadata (5.5): board + milestone + labels
gh pr ready

# Flip from ready back to draft
gh pr ready --undo
```

When the change is code, push new commits instead — the PR updates automatically. Never
close and re-open a PR to change its code; that loses review comments and CI history.

---

## Phase 7: Hand-Off to monitor-ci (mandatory — gates task completion)

After `gh pr create` or `git push` succeeds, CI starts. **Opening the PR does not
finish the task.** You must invoke `monitor-ci` and follow it through to a terminal
state. The task is complete only once you have reported one of:

- **CI green** — every gating check `completed` + `success`, reported to the user, OR
- **A blocked/escalated state** — `monitor-ci`'s retry budget exhausted or an escalate
  condition hit, with the failing job(s), root-cause hypothesis, and what was tried
  surfaced to the user (per `monitor-ci` Phase 4).

Never end the turn at "PR opened, CI running" and consider the work done — that leaves
the result unverified. Carry the CI watch to a reported conclusion before closing out.

While CI runs you are not idle: use the wait windows to perform a **self-review** of the
branch's diff (see `monitor-ci` Phase 1.2). Surface anything the self-review turns up
alongside the CI result.

Do not merge — merges are a developer decision unless explicitly delegated.

---

## Common Mistakes

- **Trying to author a PR as the bot.** There is no bot path for it — `a-novel core
bot-comment` only posts comments. PR create/edit/ready always run as the operator's
  user token (Phase 5.0).
- **Treating "PR opened" as task-done.** The task is not finished until `monitor-ci`
  reports CI green or an escalated/blocked state (Phase 7).
- **Pushing before basic CI passes locally.** CI is not a debugger. Run lint + tests +
  build locally first (1.4), scoped to what you changed. A format-clean diff (`gofmt`/
  `gofumpt`/`gci`) can still fail `lint-go` — run the linter, not just the formatter.
- **Opening a PR from master.** Branch first, then PR.
- **Closing and re-creating a PR to "fix" the title.** Use `gh pr edit --title` instead.
- **Manual reviewer/assignee flags.** Automation handles these. (Tracking metadata — milestone,
  project board, Priority/Size, and issue-matching labels — is the exception: a ready PR carries
  them, per 5.5.)
- **`--force` without `--lease`.** Always `--force-with-lease` after a rebase.
- **Missing `BREAKING CHANGE:` footer.** If any commit on the branch is breaking, the PR
  body's Breaking Changes section must list it. Mismatches between commits and PR body are
  bugs — readers trust the body.
- **PR title that does not match the primary commit scope.** If the branch is `feat/dao/*`,
  the PR title scope should be `dao`, not `services`.
- **Linking the wrong base branch on a stacked PR.** If the parent is already merged, rebase
  onto master and change the base to master before pushing.

---

## Quick Reference

| Situation                           | Command                                                                         |
| ----------------------------------- | ------------------------------------------------------------------------------- |
| Pre-flight: lint (scoped)           | `pnpm lint:go` / `go run …/golangci-lint/v2/cmd/golangci-lint@latest run ./...` |
| Pre-flight: tests (scoped)          | `a-novel test --type=go -y` / `a-novel test -y`                                 |
| Pre-flight: build (scoped)          | `a-novel build --type=go -y` (`--type=` matches changes)                        |
| First push                          | `git push -u origin <branch>`                                                   |
| Push after rebase                   | `git push --force-with-lease`                                                   |
| Check for existing PR               | `gh pr view --json number,state,url`                                            |
| Create ready PR                     | `gh pr create --title "..." --body "$(cat <<'EOF' ... )"`                       |
| Create draft PR                     | `gh pr create --draft --title ...`                                              |
| Stacked PR (base is another branch) | `gh pr create --base feat/<parent-area>/... ...`                                |
| Update title on existing PR         | `gh pr edit --title "..."`                                                      |
| Flip draft → ready                  | `gh pr ready`                                                                   |
| Flip ready → draft                  | `gh pr ready --undo`                                                            |

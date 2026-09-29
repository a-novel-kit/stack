---
name: open-pull-request
description: >
  Push a branch and open a GitHub pull request for Agora backend services. Use whenever
  shipping work — features, fixes, refactors, chores. Covers pre-flight checks, base branch,
  PR title and body, draft mode, and updating an existing PR instead of re-creating it.
  Pairs with git-conventions (commit/branch format) and monitor-ci (post-push CI).
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
ready for the final completion report. Local-only review links must not enter the PR body. A
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

## Phase 5: Create the PR

### 5.0 PR authoring runs as the operator — the bot can only comment

`gh pr create` (and every `gh pr edit` / `gh pr ready` in this skill) runs with the plain
`gh` credential, the operator's **user token**. The operator authors the PR, so the
`auto-assign-author` workflow can assign them and `CODEOWNERS` routing works.

Authoring a PR as the bot is impossible by construction. There is no local bot token, and
the only bot entry point — `a-novel core bot-comment <org> <repo> <number> --body …` —
does one thing: trigger the centralized dispatcher workflow, which _posts a comment_. So
`pr create|edit|ready|merge|close` are always operator actions; commenting (top-level
PR/issue comments and review-thread replies in `resolve-pr-feedback`) is the only thing
that attributes to `<app-slug>[bot]`.

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

With multiple commits touching one scope, use the scope that best describes the branch's
goal. When the commits are genuinely cross-cutting (rename across layers), omit the scope.

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
- **Linked issues — close a planning issue with the FULL cross-repo ref.** A PR implementing
  a planning issue must close it in the body so merging advances the board. Planning issues
  (Epic / Feature / Task) live in the org **`.github`** repos (`a-novel-kit/.github`,
  `a-novel/.github`), so from another repo a bare `Closes #<n>` resolves to _this_ repo and
  links **nothing** — the issue then freezes on the board (a Feature stuck at Backlog though
  its PR merged). Use `Closes a-novel-kit/.github#<n>` (or `a-novel/.github#<n>`): only that
  form lands in the PR's `closingIssuesReferences`, the sole signal `derive-status` reads to
  move the issue's board **Status**. A Task filed in _this same_ repo keeps the bare
  `Closes #<n>`.
- **Layers changed** lists only the layers actually touched. Omit the section entirely if
  only one layer is affected and the title already conveys it.
- **Breaking changes** is either `None.` or an itemized list with migration steps. Never
  leave it "TBD" or blank — reviewers should not have to hunt.
- **Test plan** is a checklist. Check the boxes you have already verified locally; leave
  `CI green` unchecked (monitor-ci will mark it).
- **Local review surfaces stay out of the PR body.** Never put localhost or another local-only URL in
  durable PR metadata. For rendered UI, `write-frontend` requires the freshly verified direct
  Storybook link in the completion report instead.
- **Read the body back after create/edit.** Run
  `gh pr view <n> --json body --jq .body` and verify headings, lists, and line breaks render as
  intended. Literal `\n` text is a quoting defect; fix it before handoff.

**Writing style — rationale-dense, zero filler.** The body's job is what the diff cannot say:
why the change, what tradeoff was taken, what a reviewer should scrutinize. Never narrate the
diff — file lists, mechanical renames, and "updated X to Y" bullets restate what review tooling
already shows. Exhaustive on decisions, silent on mechanics. The same bar applies to PR thread
comments (`resolve-pr-feedback`), where prose may lean more technical.

### 5.4 Do NOT pass these flags

- `--assignee` / `--reviewer` — the `auto-assign-author` workflow handles assignees; the
  repo decides reviewers via its `CODEOWNERS` file (at repo root) or team routing. Manual
  assignment duplicates or conflicts with that automation, so set reviewers only when the
  user asks for a specific person.
- `--label` — downstream automation derives labels from the title's Conventional-Commits
  type. Add one manually only when the user requests it.
- `--milestone` / project board / **Priority** / **Size** / tracking labels — **not** left to humans:
  a **ready** PR mirrors the milestone, project board, its board fields (Priority, Size), and labels
  of the issue it closes. See [Tracking metadata](#55-tracking-metadata--match-the-linked-issue).

### 5.5 Tracking metadata — match the linked issue

A PR that is **ready for review** should be as trackable as the planning issue it closes: add it
to the org **"Tasks"** board and give it the **same milestone**, the **same board fields
(Priority, Size)**, and the relevant **tracking labels** as that issue — so a glance at the board
shows the work whether you look at the issue or its PR. A draft skips this; apply it when opening
ready, or at the **draft → ready** flip (Phase 6).

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

`gh pr create` prints the PR URL on success. Surface it in the final message so the user can
jump to it. When an active layer skill requires a companion review surface, include its direct link
in the same completion report as the PR and linked task or issue URLs; for rendered UI, this is the
live local Storybook route required by `write-frontend`. Do not add that local URL to the PR body.

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

### 6.1 Flip a draft to ready the moment it qualifies (mandatory)

A draft exists for a Phase 3 reason. **Every time you push to a draft PR, re-check whether that
reason still holds** — status is not decided once at creation and forgotten. When the latest work
makes the branch review-ready — approved issue scope, resolved blocking decisions, completed
relevant tests and visual comparisons, final cleanup, and required layers/docs/dependencies — flip
it in the same turn:

```bash
gh pr ready   # then apply Phase 5.5 tracking metadata: board + milestone + labels
```

**Never end a turn with review-ready work sitting in a draft PR.** If the branch is done and green
and no Phase 3 reason still applies, the PR is ready — say so and run `gh pr ready`. Silently
leaving it draft withholds it from the reviewer and stalls the task; waiting for the user to say
"flip it" is the failure, not the courtesy.

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

## Phase 8: Close With a Session Recap Table and Approval Command (mandatory)

**Whenever you finish a stretch of code or issue work, end your reply with a recap table** so the
operator can jump straight to whatever needs their attention. This is not optional and not limited
to what changed since the last prompt: **list every item from this whole session that still needs
attention** — PRs awaiting review or merge, issues to act on, branches pushed, CI still running —
even ones you reported turns ago, until they are actually resolved.

Each row's identifier is an **inline markdown link** to the PR or issue, so the target is one click
away. Every PR row also carries its approval handoff in a dedicated **Admin-only approval (after
review)** column; use an em dash for non-PR rows. A minimal shape:

```markdown
| Item                                    | State           | Needs               | Admin-only approval (after review)                                                                                                                                   |
| --------------------------------------- | --------------- | ------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| [#321](https://github.com/…/321)        | Ready, CI green | Your review → merge | `gh workflow run approve-pr.yaml --repo a-novel/service-authentication --ref master --field pull_request=https://github.com/a-novel/service-authentication/pull/321` |
| [.github#432](https://github.com/…/432) | Task, blocked   | Decide ownership    | —                                                                                                                                                                    |
```

Drop the table only when the turn touched no code or issues at all (a pure question). If nothing
is outstanding, say so in one line instead of an empty table. This rule is session-global — its
authoritative statement lives in memory (`session-recap-table`) so it fires even on turns where
this skill never loads (e.g. issue-only work under `triage-issues`).

For rendered UI, the same final report must also include the freshly verified direct local Storybook
link required by `write-frontend`, adjacent to the recap table or in the relevant PR row. Never put
that local-only link in the PR body.

For every open PR in the recap, put a copy-pasteable command **inside that PR's table row** that lets
a repository admin record their approval through the repo's `approve-pr` workflow after reviewing
the PR:

```bash
gh workflow run approve-pr.yaml --repo <org>/<repo> --ref <default-branch> --field pull_request=<PR-URL>
```

Resolve every placeholder before presenting the command: use the PR's exact repository and URL, and
the repository's actual default branch. Label it **admin-only** and say it is for use after review.
Never place the command only in prose beside or after the table: condensed task summaries may retain
the recap table while omitting surrounding prose.
This is a handoff command, not authorization to dispatch the workflow; never run it unless the user
explicitly asks. The workflow itself fails closed when the dispatching user is not a repository
admin.

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

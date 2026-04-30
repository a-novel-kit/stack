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

### 1.4 Local tests pass

Run the narrowest test target that covers the branch's layer — see `implement-feature` for
the layer-to-target mapping. Typical:

```bash
make test-unit      # Go internal layers
make test-pkg       # pkg/go (needs running service)
make test-pkg-js    # pkg/js
```

If tests fail locally, CI will fail too. Fix before pushing. Never push a red branch with
the expectation that "CI will tell me what's wrong" — that wastes a CI cycle and reviewer
attention.

### 1.5 Generated files are in sync

If the branch touches `.proto` files or Go interfaces that have mocks:

```bash
make generate
git status --porcelain
```

Any newly-modified files under `internal/handlers/protogen/`, `internal/handlers/mocks/`, or
`internal/services/mocks/` belong with the source change that caused them. Before the
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

If this branch depends on another open PR (e.g., `feat/services/jwk-revoke` depends on
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

- [x] `make test-unit` passes
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

### 5.4 Do NOT pass these flags

- `--assignee` / `--reviewer` — the `auto-assign-author` workflow handles assignees; the
  repo decides reviewers via its `CODEOWNERS` file (at repo root) or team routing. Do not
  set reviewers manually unless the user explicitly requests a specific person; manual
  assignment duplicates or conflicts with that automation.
- `--label` — labels are derived from the title's Conventional-Commits type by downstream
  automation. Do not add them manually unless the user requests a specific one.
- `--milestone` — project management concern; leave to humans.

### 5.5 Capture the PR URL

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

# Flip from draft to ready
gh pr ready

# Flip from ready back to draft
gh pr ready --undo
```

When the change is code, push new commits instead — the PR updates automatically. Never
close and re-open a PR to change its code; that loses review comments and CI history.

---

## Phase 7: Hand-Off to monitor-ci

After `gh pr create` or `git push` succeeds, CI starts. Hand off to the `monitor-ci` skill
to watch the run, classify any failures, and apply fixes. Do not merge — merges are a
developer decision unless explicitly delegated.

---

## Common Mistakes

- **Pushing before local tests pass.** CI is not a debugger. Run tests locally first.
- **Opening a PR from master.** Branch first, then PR.
- **Closing and re-creating a PR to "fix" the title.** Use `gh pr edit --title` instead.
- **Manual reviewer/assignee/label flags.** Automation handles these.
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

| Situation                           | Command                                                   |
| ----------------------------------- | --------------------------------------------------------- |
| First push                          | `git push -u origin <branch>`                             |
| Push after rebase                   | `git push --force-with-lease`                             |
| Check for existing PR               | `gh pr view --json number,state,url`                      |
| Create ready PR                     | `gh pr create --title "..." --body "$(cat <<'EOF' ... )"` |
| Create draft PR                     | `gh pr create --draft --title ...`                        |
| Stacked PR (base is another branch) | `gh pr create --base feat/<parent-area>/... ...`          |
| Update title on existing PR         | `gh pr edit --title "..."`                                |
| Flip draft → ready                  | `gh pr ready`                                             |
| Flip ready → draft                  | `gh pr ready --undo`                                      |

---
name: git-conventions
description: >
  Manage checkout hygiene, branches, commits, and history. Load when starting or finishing checkout
  work, cleaning up worktrees or branches, creating or naming branches, grouping changes, or writing
  commits; pair with attribute-ai-commits.
---

# Git Conventions

This skill governs workspace state, branch naming, and commit messages across all Agora backend
services. Every branch and commit produced by an agent follows these conventions exactly — they
drive automation (Renovate, CI tagging, changelogs) and signal intent to reviewers at a glance.

---

## Load the Git procedure for this operation

- Before starting or finishing checkout work, or cleaning anything up, read
  [workspace hygiene](references/workspace.md). Cleanup touches only what this session created;
  preserve other contributors' work and use an isolated checkout for sustained work.
- Before naming a branch, grouping changes, or writing, amending, or reviewing a commit message,
  read [commit conventions](references/commits.md).
  Use `attribute-ai-commits` for material AI contributions.
- For branch names and history changes, apply the rules below.
- For publication, PR descriptions, metadata, readiness, and final handoff, load
  [open-pull-request](../open-pull-request/SKILL.md); it owns those mechanics.

`develop-feature` owns stage timing. Existing authorization carries forward; neither a clean
tree nor a local commit alone authorizes publication, merging, or changes to another person's work.

## Branch Naming

```
<type>/<area>/<short-description>
```

- **type**: same vocabulary as commit types (`feat`, `fix`, `refactor`, `chore`, `ci`, `docs`)
- **area**: the layer or subsystem being changed — use the [commit scope](references/commits.md#scopes)
- **short-description**: kebab-case, 2–5 words, describes what the branch achieves

### Examples

```
feat/proto/add-key-revoke-rpc
feat/handlers/grpc-jwk-revoke
fix/dao/search-returns-deleted-keys
refactor/core/extract-key-rotation-logic
chore/skills/feature-workflow
docs/pkg/update-client-examples
```

Branch names are lowercase kebab-case only. No underscores, no slashes inside a segment, no version
numbers unless it's a release branch.

---

## Commit Workflow

```bash
# 1. Stage only the files for this logical unit
git add internal/dao/pg.jwkRevoke.go internal/dao/pg.jwkRevoke_test.go

# 2. Commit with a conventional message — a subject line, nothing more
git commit -m "feat(dao): add soft-delete repository for key revocation"

# Only when the body carries what the diff cannot (HEREDOC for multi-line):
git commit -m "$(cat <<'EOF'
fix(dao): lock the key row before revoking

Concurrent revokes both read the key as ACTIVE and wrote two audit
entries; SELECT ... FOR UPDATE is what serialises them.
EOF
)"
```

### Rules

- **A commit is its subject line.** Add a body only when it says something the subject and the
  diff cannot — see [Body](references/commits.md#body).
- **One logical unit per commit.** A DAO file + its test = one commit; a migration file = one
  commit. Never combine DAO + service in a single commit.
- **Stage explicit paths — never `git add -A` / `git add .`.** These checkouts keep sibling
  worktrees under an untracked `tmp/` (see [Before you start](references/workspace.md#before-you-start)). A blanket add
  stages each `tmp/wt-*` as an embedded-repo **gitlink** — a bogus submodule ref that rides your
  commit onto `master` if it slips through review. `git show --stat HEAD` betrays it as a
  `tmp/wt-… | 1 +` line. Name the files the logical unit touched; the `git add …` in this workflow
  is a list on purpose.
- **Generated files belong in the same commit as the change that required them.** Proto Go bindings
  (`internal/models/proto/gen/`) and mocks (`internal/handlers/mocks/`, `internal/core/mocks/`)
  never get their own commit — stage them with the `.proto` or interface change that required
  `pnpm generate:go`.
- **Never commit secrets.** .env files, APP_MASTER_KEY values, real credentials.
- **Never skip hooks** (`--no-verify`) unless explicitly asked.
- **Never amend a pushed commit.** Create a new commit instead.
- **Mid-rebase, resolve, `git add`, then `git rebase --continue`; never `git commit --amend`.**
  `HEAD` is then the previously replayed commit, so an amend folds the resolution into it and
  replaces its message. To undo it, `git reset --soft` to that commit from the reflog, commit the
  difference, and compare `HEAD^{tree}` with the amended tree to prove nothing else moved.
- **Never push to `master`/`main` — not force-push, not a plain push — without explicit consent.**
  This is the one git action that is never safe by default. Most contributors lack the access, so
  the guardrail is already enforced for them; on an admin account it is _yours_ to hold, because you
  have the rights to bypass it. Publish feature branches at the stage authorized by
  `develop-feature`; see [Branch and PR freedom](#branch-and-pr-freedom).

---

## Branch and PR Freedom

Follow [develop-feature](../develop-feature/SKILL.md): during local drafting, keep branches and
checkpoint commits local until the developer agrees on the direction, unless they explicitly
requested publication. A local commit is not itself a reason to push.

After that agreement, pushing a feature branch and opening a draft PR within the authorized scope
need no repeated permission. Use `open-pull-request` for the checks appropriate to the stage. Clear,
small tasks can take the shorter path defined by `develop-feature`.

Draft PRs can receive review while their planning issues are still being discussed. Preserve review
history, keep scope and deferred coverage visible, and follow the normal history-rewrite rules.
Mark ready after issue scope is approved and testing and cleanup are complete. Never treat a draft
as exempt from repository safeguards or push directly to `master`/`main` without explicit consent.

---

## Pull Request Description

Use [open-pull-request](../open-pull-request/SKILL.md) for the authoritative title, body,
tracking metadata, validation, and handoff requirements. Keep the Conventional-Commits title under
70 characters, state breaking changes, and include only affected layers and meaningful verification.

---
name: git-conventions
description: >
  Git workspace hygiene (clean-tree pre-flight, pruning a scratch stack), branch naming, commit
  message format, and workflow conventions for Agora backend services. Use this skill whenever
  starting or finishing work in a checkout, creating a branch, writing a commit message, or
  deciding how to group changes. Referenced by implement-feature and any other skill that touches
  git.
---

# Git Conventions

This skill governs workspace state, branch naming, and commit messages across all Agora backend
services. Every
branch and commit produced by Claude must follow these conventions exactly — they drive automation
(Renovate, CI tagging, changelogs) and signal intent to reviewers at a glance.

---

## Commit Messages — Conventional Commits

All commits use the [Conventional Commits](https://www.conventionalcommits.org/) format:

```
<type>(<scope>): <description>

[optional body]

[optional footer(s)]
```

### Types

| Type       | Use when…                                                     |
| ---------- | ------------------------------------------------------------- |
| `feat`     | Adding a new capability (endpoint, field, algorithm)          |
| `fix`      | Correcting a bug or incorrect behaviour                       |
| `refactor` | Restructuring code without changing behaviour or API surface  |
| `perf`     | Performance improvement with no functional change             |
| `test`     | Adding or fixing tests only                                   |
| `docs`     | Documentation only (comments, doc.go, openapi.yaml, SKILL.md) |
| `chore`    | Maintenance that doesn't fit above (deps, CI, build scripts)  |
| `ci`       | CI/CD pipeline changes only                                   |
| `revert`   | Reverting a previous commit                                   |

Never mix types in one commit. A commit that adds a handler AND its test is still `feat` — the
test is bundled because it's part of the same deliverable. A commit that only adds tests for
existing code is `test`.

### Scopes

The scope is the area of the codebase affected. Use the layer name, not the feature name:

| Scope        | Covers                                          |
| ------------ | ----------------------------------------------- |
| `proto`      | Protobuf definitions (`internal/models/proto/`) |
| `migrations` | Database schema (`internal/models/migrations/`) |
| `dao`        | Data access layer (`internal/dao/`)             |
| `core`       | Business logic (`internal/core/`)               |
| `handlers`   | gRPC and REST handlers (`internal/handlers/`)   |
| `config`     | Configuration (`internal/config/`)              |
| `lib`        | Shared utilities (`internal/lib/`)              |
| `pkg`        | Exported Go client (`pkg/go/`)                  |
| `pkg-js`     | Exported JS/TS client (`pkg/js/`)               |
| `cmd`        | Targets (`cmd/`)                                |
| `builds`     | Dockerfiles and compose files (`builds/`)       |
| `scripts`    | Shell scripts (`scripts/`)                      |
| `ci`         | GitHub Actions workflows (`.github/`)           |
| `deps`       | Dependency bumps (go.mod, package.json)         |
| `skills`     | Skill documents (`.agents/skills/`)             |

When a commit touches multiple scopes of the same weight, pick the primary one. When the commit
is genuinely cross-cutting (e.g., a rename that touches every layer), omit the scope.

### Description

- Imperative mood, present tense: "add key rotation endpoint" not "adds" or "added"
- Under 72 characters
- No period at the end
- Describes the _what_, not the _how_ — readers see the diff; they need the intent

The subject line carries the message. It is what `git blame`, `git log --oneline`, and the
release notes show, and in practice it is as far as most readers get. Spend the effort there.

### Body

Default to no body. Most commits are a subject line and nothing else.

Repos squash-merge with `COMMIT_MESSAGES`, so every body on the branch is concatenated into the
commit that lands on `master`. A five-commit branch with three-paragraph bodies becomes a wall of
prose attached to a single line of history that nobody scrolls past.

A body earns its place only when it carries something **neither the subject nor the diff can
show**:

- A constraint that forced a non-obvious approach — the thing a future reader would otherwise
  "clean up" and break.
- The failure a `fix` repairs, when the symptom is not visible in the diff (a race, a CI-only
  break, a bug in a dependency).

When it does, keep it to one or two sentences — three lines wrapped at 72 characters is the
ceiling. Never restate the subject at greater length, summarise the diff, or list touched files.

Footers (`BREAKING CHANGE:`, `Closes`, `Co-Authored-By:`) are not prose and are never trimmed.

Longer reasoning has better homes, all of which readers actually reach:

| Reasoning                                   | Goes in                                 |
| ------------------------------------------- | --------------------------------------- |
| What the change does and why, for reviewers | The PR description                      |
| A design decision or rejected option        | The planning issue (body or discussion) |
| Something a reader of the code needs        | A code comment (see `document-code`)    |

### Breaking Changes

Prefix the description with `!` and add a `BREAKING CHANGE:` footer:

```
feat(proto)!: remove deprecated KeyUsage enum value

BREAKING CHANGE: KeyUsage.LEGACY is removed. Callers using this value
must migrate to KeyUsage.AUTH before upgrading.
```

Flag any change that:

- Removes or renames a protobuf field/message/service
- Removes or renames an exported Go type, function, or constant in `pkg/go`
- Removes or renames an exported TypeScript type or function in `pkg/js`
- Removes or changes the semantics of a REST endpoint path or response shape
- Changes a database column type or removes a column

---

## Workspace Hygiene

### Before you start

**Start every task from `master`, with a clean tree, in a checkout that is yours.** Check this
before the first edit — in the stack root and in each `app/` or `kit/` checkout the task will
touch, since those are independent repos with independent states:

```bash
git -C <checkout> status --porcelain            # empty
git -C <checkout> rev-parse --abbrev-ref HEAD   # master
git -C <checkout> branch --no-merged master     # nothing you did not create
```

Uncommitted changes, or an unmerged branch you did not create, mean **someone else is working in
this checkout** — the operator in another terminal, or a parallel agent session. Their
work-in-progress is invisible to you, and `stash`, `reset`, `checkout -f`, or branching on top of
it can destroy hours of work that exists nowhere else. Leave it untouched and take a checkout of
your own.

**A clean pre-flight expires immediately.** It proves the checkout was free at that instant, and a
checkout has exactly one HEAD with nothing holding it: a parallel session that runs `git checkout`
between your `checkout -b` and your `commit` lands your commit on whatever branch it moved you to,
and its own `reset` can then unlink it. For anything longer than a couple of commands, work in a
worktree of your own instead — git refuses to check out a branch already checked out elsewhere, so
the branch cannot be taken from you mid-task, and the shared object store keeps every commit you
have already made.

```bash
git worktree add <path-outside-the-repo> <branch>
```

If a collision already happened, nothing is lost: the commit is unreferenced, not deleted. Find it
in `git reflog`, point your branch at it with `git branch -f <branch> <sha>`, and put local `master`
back with `git branch -f master origin/master`. Clear your own stray files out of the shared tree so
the other session does not commit them, and leave everything of theirs alone.

The daemon manages as many stacks as the machine supports. `A_NOVEL_STACKS` is its source of
truth, formatted `name:/path,name:/path` with the first entry as the default; unset means a single
`default` stack at `~/git-projects/a-novel`.

```bash
a-novel core stacks list             # which stacks exist, and what they hold
a-novel core stacks new <name>       # clone the workspace into a fresh root
a-novel core sync --root=<new-root>  # populate it with the whitelisted repos
```

`stacks new` puts the checkout under the OS temp directory unless `--root` says otherwise, so a
stack nobody prunes expires instead of accumulating. Add the printed entry to `A_NOVEL_STACKS`
and `a-novel core restart` so daemon-backed verbs (`a-novel run …`) reach it, then work from
there — see `use-a-novel-cli`.

Resuming a branch **you** created earlier in the same session is your own work, not this case.
Carry on with it.

### When you are done

A stack synced for one task is scratch space. Left behind it becomes a stale checkout the next
session mistakes for real work, plus containers and volumes that outlive the machine's reboot.

**Prune it when development ends: every change reviewed and approved.** Not at push, and not at
green CI — review turns up work, and rebuilding a stack to answer one comment costs more than
keeping it a few hours longer. Approval is the first moment nothing further is expected of the
checkout.

```bash
a-novel core stacks list           # what each stack is still holding
a-novel core stacks prune <name>   # kill its targets + infra, clear its volumes, remove its files
```

Prune covers all three of a stack's allocations. Deleting the root by hand covers one of them: the
containers keep running on their host ports and the volumes stay in the container store, because
neither ever lived in the stack directory.

`prune` refuses the default stack, and refuses any stack still holding work that exists nowhere
else — so run it without rehearsing the state first. It reports the `A_NOVEL_STACKS` entry to drop
rather than editing your shell config.

**Never reach for `a-novel core kill --force` as cleanup.** It tears down every service's infra
across every registered stack, the operator's included — the exact harm the pre-flight above
exists to prevent.

---

## Branch Naming

```
<type>/<area>/<short-description>
```

- **type**: same vocabulary as commit types (`feat`, `fix`, `refactor`, `chore`, `ci`, `docs`)
- **area**: the layer or subsystem being changed — use the scope name from the table above
- **short-description**: kebab-case, 2–5 words, describes what the branch achieves

### Examples

```
feat/proto/add-key-revoke-rpc
feat/migrations/add-revoked-keys-table
feat/dao/jwk-revoke
feat/core/jwk-revoke
feat/handlers/grpc-jwk-revoke
fix/dao/search-returns-deleted-keys
refactor/core/extract-key-rotation-logic
chore/builds/add-rotate-keys-dockerfile
chore/skills/feature-workflow
docs/pkg/update-client-examples
feat/pkg-js/add-rotate-endpoint
```

Branch names are lowercase kebab-case only. No underscores, no slashes inside a segment, no
version numbers unless it's a release branch.

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
  diff cannot, and keep it to a sentence or two — see [Body](#body).
- **One logical unit per commit.** A DAO file + its test = one commit. A handler + its test = one
  commit. A migration file = one commit. Never combine DAO + service in a single commit.
- **Generated files belong in the same commit as the change that necessitated them.** Proto Go
  bindings (`internal/models/proto/gen/`) and mock files (`internal/handlers/mocks/`,
  `internal/core/mocks/`) are generated artifacts. Do not commit them separately — stage them
  together with the `.proto` or interface change that required `pnpm generate:go`.
- **Never commit secrets.** .env files, APP_MASTER_KEY values, real credentials.
- **Never skip hooks** (`--no-verify`) unless explicitly asked.
- **Never amend a pushed commit.** Create a new commit instead.
- **Never force-push to master/main.**

---

## Pull Request Description

When opening a PR (via `gh pr create`), use this template:

```
gh pr create --title "<type>(<scope>): <description>" --body "$(cat <<'EOF'
## Summary

<1-3 bullet points describing what changed and why.>

## Layers changed

- **DAO**: <what changed>
- **Services**: <what changed>
- **Handlers**: <what changed>
- **Proto / OpenAPI**: <what changed>
- **pkg/go**: <what changed>
- **pkg/js**: <what changed>

## Breaking changes

None. / <List any breaking changes with migration steps.>

## Test plan

- [ ] `a-novel test --type=go -y` passes
- [ ] `a-novel test --type=pnpm -y` passes (if pkg/js changed)
- [ ] <Any manual verification steps>
EOF
)"
```

- Keep the title under 70 characters and follow Conventional Commits format.
- Be explicit about breaking changes — reviewers should not have to hunt for them.
- Skip layers that were not affected rather than writing "no change" for all of them.

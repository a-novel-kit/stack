---
name: git-conventions
description: >
  Git branch naming, commit message format, and workflow conventions for Agora backend services.
  Use this skill whenever creating a branch, writing a commit message, or deciding how to group
  changes. Referenced by implement-feature and any other skill that touches git.
---

# Git Conventions

This skill governs branch naming and commit messages across all Agora backend services. Every
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
| `services`   | Business logic (`internal/services/`)           |
| `handlers`   | gRPC and REST handlers (`internal/handlers/`)   |
| `config`     | Configuration (`internal/config/`)              |
| `lib`        | Shared utilities (`internal/lib/`)              |
| `pkg`        | Exported Go client (`pkg/go/`)                  |
| `pkg-js`     | Exported JS/TS client (`pkg/js/`)               |
| `cmd`        | Entrypoints (`cmd/`)                            |
| `builds`     | Dockerfiles and compose files (`builds/`)       |
| `scripts`    | Shell scripts (`scripts/`)                      |
| `ci`         | GitHub Actions workflows (`.github/`)           |
| `deps`       | Dependency bumps (go.mod, package.json)         |
| `skills`     | Skill documents (`.claude/skills/`)             |

When a commit touches multiple scopes of the same weight, pick the primary one. When the commit
is genuinely cross-cutting (e.g., a rename that touches every layer), omit the scope.

### Description

- Imperative mood, present tense: "add key rotation endpoint" not "adds" or "added"
- Under 72 characters
- No period at the end
- Describes the _what_, not the _how_ — readers see the diff; they need the intent

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
feat/services/jwk-revoke
feat/handlers/grpc-jwk-revoke
fix/dao/search-returns-deleted-keys
refactor/services/extract-key-rotation-logic
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

# 2. Commit with a conventional message (use HEREDOC for multi-line)
git commit -m "feat(dao): add soft-delete repository for key revocation"

# or multi-line:
git commit -m "$(cat <<'EOF'
feat(dao): add soft-delete repository for key revocation

Returns ErrJwkRevokeNotFound when the target key is already deleted
or expired, mirroring the PgJwkDelete sentinel pattern.
EOF
)"
```

### Rules

- **One logical unit per commit.** A DAO file + its test = one commit. A handler + its test = one
  commit. A migration file = one commit. Never combine DAO + service in a single commit.
- **Generated files belong in the same commit as the change that necessitated them.** Proto Go
  bindings (`internal/models/proto/gen/`) and mock files (`internal/handlers/mocks/`,
  `internal/services/mocks/`) are generated artifacts. Do not commit them separately — stage them
  together with the `.proto` or interface change that required `make generate`.
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

- [ ] `make test-unit` passes
- [ ] `make test-pkg` passes (if pkg/go changed)
- [ ] `make test-pkg-js` passes (if pkg/js changed)
- [ ] <Any manual verification steps>
EOF
)"
```

- Keep the title under 70 characters and follow Conventional Commits format.
- Be explicit about breaking changes — reviewers should not have to hunt for them.
- Skip layers that were not affected rather than writing "no change" for all of them.

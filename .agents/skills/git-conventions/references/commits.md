# Commit message conventions

Read this reference when routed here by [git-conventions](../SKILL.md). Its rules apply to the selected work.

Contents:

- [Commit Messages — Conventional Commits](#commit-messages--conventional-commits)
- [Types](#types)
- [Scopes](#scopes)
- [Description](#description)
- [Body](#body)
- [AI co-author attribution](#ai-co-author-attribution)
- [Breaking Changes](#breaking-changes)

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

Never mix types in one commit. A commit that adds a handler AND its test is still `feat` — the test
ships as part of the same deliverable. A commit that only adds tests for existing code is `test`.

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

When a commit touches several scopes of the same weight, pick the primary one. When the commit is
genuinely cross-cutting (e.g., a rename that touches every layer), omit the scope.

### Description

- Imperative mood, present tense: "add key rotation endpoint" not "adds" or "added"
- Under 72 characters
- No period at the end
- Describes the _what_, not the _how_ — readers see the diff; they need the intent

The subject line carries the message. It is what `git blame`, `git log --oneline`, and the release
notes show, and as far as most readers get. Spend the effort there.

### Body

Default to no body. Most commits are a subject line and nothing else.

Repos squash-merge with `COMMIT_MESSAGES`, so every body on the branch is concatenated into the
commit that lands on `master`. A five-commit branch with three-paragraph bodies becomes a wall of
prose attached to a single line of history that nobody scrolls past.

A body earns its place only when it carries something **neither the subject nor the diff can
show**:

- A constraint that forced a non-obvious approach — the thing a future reader would otherwise
  "clean up" and break.
- The failure a `fix` repairs, when the symptom is invisible in the diff (a race, a CI-only break,
  a bug in a dependency).

Then keep it to one or two sentences — three lines wrapped at 72 characters is the ceiling. Never
restate the subject at greater length, summarise the diff, or list touched files.

Footers (`BREAKING CHANGE:`, `Closes`, `Co-authored-by:`) are not prose and are never trimmed.

Longer reasoning has better homes, all of which readers actually reach:

| Reasoning                                   | Goes in                                 |
| ------------------------------------------- | --------------------------------------- |
| What the change does and why, for reviewers | The PR description                      |
| A design decision or rejected option        | The planning issue (body or discussion) |
| Something a reader of the code needs        | A code comment (see `document-code`)    |

### AI co-author attribution

Load `attribute-ai-commits` whenever an AI agent materially contributed content included in the
commit. Add its standard `Co-authored-by:` footer only when that skill's pushed registry marks the
agent identity as verified.

Never invent a provider email, use another provider's bot, or add an unlinked display-only footer.
The attribution skill owns registry lookup, missing-agent verification and registration,
model-aware display names, and unavailable identities.

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

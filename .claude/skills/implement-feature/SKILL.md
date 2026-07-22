---
name: implement-feature
description: >
  Plan and implement changes to an a-novel backend SERVICE repo using a layered branch strategy.
  Use whenever implementing an add/change/remove to a service — new API endpoints, schema changes,
  business logic, client updates. For non-trivial, multi-repo, or architectural work, run
  plan-feature first to agree the design, then use this skill to execute it; small, unambiguous
  changes can come straight here. Covers the full lifecycle from triage to final commit: decompose
  into branches, test each branch, handle backtracking. Backend services only — frontend (platform)
  repos have their own (forthcoming) conventions; do not apply the layered decomposition to them.
---

# Feature Implementation Workflow

This skill governs how Claude executes feature work in a-novel **backend services** (the `service-*`
repos with the `cmd`/`internal/...`/`pkg` clean-architecture layout). Every non-trivial change goes
through the same phases: **Assess → Plan → Implement**. Each phase has a gate before the next begins.
A later section covers recovery when an earlier branch needs to change.

> **Where the design comes from.** For non-trivial, multi-repo, or architectural work the _design_ is
> settled first in `plan-feature`, which produces an agreed **planning issue** (an Epic, or a Feature
> with its Task sub-issues); this skill then executes it, and the **Plan** phase below is the per-repo
> **branch decomposition** of that already-agreed design — typically one branch/PR per Task sub-issue.
> Link every PR back to its issue with a `Closes #<n>` line so merging closes the unit and the Epic's
> sub-issue progress advances. Small, unambiguous changes can start directly here.
>
> **Scope: backend services only.** Frontend **`platform`** repos are deliberately more monolithic
> than services — the layer-by-layer branch decomposition here does **not** apply to them, and their
> authoring conventions arrive in a later stage. Don't force a platform change through this workflow.

---

## Phase 0: Before Writing Any Code

**Check the workspace is yours.** Every checkout the task will touch must be on `master` with a
clean tree — see `git-conventions` › Workspace Hygiene. A dirty tree or a foreign unmerged branch
means another session is already working there: allocate your own stack with
`a-novel core stacks new <name>` rather than building on top of their work, and prune it at the
end of the lifecycle (3.6).

**Clarify ambiguous requests first.** If the request is broad ("improve the service", "refactor
this area") or could be interpreted multiple ways, ask one focused question before reading any code.
A plan built on a misunderstood requirement wastes both read and write effort.

**Read the code that will change.** Never guess at signatures, error types, or interfaces. Before
proposing a plan, read:

- The production files in every affected layer
- The test files for those layers (they document the contract)
- Any SQL migrations that the change builds on

Do not propose a plan based on assumptions. If you are unsure which files are involved, use Grep
and Glob to find them.

---

## Phase 1: Assess

Answer these questions before proposing anything:

### What layers does this touch?

Trace the change from data to API surface. For each layer, decide: **must change**, **may
change**, or **not affected**.

| Layer           | Must change if…                                              |
| --------------- | ------------------------------------------------------------ |
| Schema          | The data model gains, loses, or changes columns/tables       |
| DAO             | The database query changes or a new query is needed          |
| Core            | Business logic changes, new error cases, new orchestration   |
| Handlers (gRPC) | A new RPC is added or an existing one changes behaviour      |
| Handlers (REST) | A new endpoint is added or an existing one changes behaviour |
| Proto           | A gRPC message or service interface changes                  |
| OpenAPI         | A REST endpoint contract changes                             |
| pkg/go          | The exported Go client API changes                           |
| pkg/js          | The REST endpoint contract changes (same trigger as OpenAPI) |

**OpenAPI / REST / JS synchronization rule**: the OpenAPI spec (`openapi.yaml`), the Go REST
handlers, and the JS client (`pkg/js/rest/`) are three representations of the same contract.
Whenever any one of them changes, all three must be updated in the same feature. A PR that
updates one without the others must explicitly justify the omission. Divergence between these
three is a bug.

### Does it break anything?

A change is breaking if it would cause existing callers to fail without code changes on their side.
Flag these explicitly — they require a BREAKING CHANGE commit footer and a warning to the developer.

Breaking changes include:

- Removing or renaming a protobuf field, message, or service
- Removing or renaming an exported symbol in `pkg/go`
- Removing or renaming an exported TypeScript type or function in `pkg/js`
- Removing a REST endpoint or changing its URL/method
- Changing a response field type or removing a response field
- Adding a required field to an existing request
- Changing a database column type without a safe migration

**Non-breaking changes** (always prefer these):

- Adding a new optional field to a proto message
- Adding a new endpoint alongside existing ones
- Adding a new service method
- Adding a new migration column with a default or nullable

### Is this purely additive or does it modify existing behaviour?

Additive changes (new endpoint, new field, new service) are safe to ship in parallel with existing
code. Modifications (change error mapping, change response shape, fix a bug) may affect existing
callers and need extra care.

---

## Phase 2: Plan

Decompose the feature into **one branch per layer boundary**. A branch is the smallest unit that:

- Compiles on its own
- Passes `a-novel test --type=go -y` (see `use-a-novel-cli` skill)
- Is independently reviewable

**Branch order** follows the dependency chain — always work bottom-up:

```
1. Schema (migration) — nothing else can be written until the schema exists
2. Proto (if gRPC surface changes) — DAO and handlers depend on this
3. DAO — services depend on the DAO interface
4. Services — handlers depend on the service interface
5. Handlers (gRPC + REST) — pkg/go depends on the gRPC handler
6. pkg/go — depends on the gRPC contract
7. OpenAPI + pkg/js — depend on the final REST contract and must always change together
```

Skip layers that are not affected. A feature that only adds a new service method and handler may
start at step 4.

**When a single branch is enough**: if the feature touches only one layer (or the coupled
OpenAPI + pkg/js pair, which always moves as one unit), creates no migration, and involves no
proto changes, a single branch is appropriate. Improvement rounds (multi-layer bug fixes, test
additions, doc updates, code cleanup) that contain no user-facing behavior change also stay on a
single branch — splitting them would be churn with no review benefit.

**Present the plan to the developer before starting.** Show:

- A numbered list of branches with their name and one-sentence description
- Any breaking changes, flagged explicitly
- Any layers deliberately skipped and why

Wait for explicit approval before creating branch 1.

---

## Phase 3: Implement (per branch)

For each branch in order:

### 3.1 Create the branch

```bash
# From master (independent of other branches)
git checkout master
git checkout -b feat/<area>/<description>

# From a parent branch (depends on earlier branch)
git checkout feat/<parent-area>/<parent-description>
git checkout -b feat/<area>/<description>
```

Branch from master whenever possible. Only branch from a parent branch when the work literally
cannot compile without that branch's changes.

### 3.2 Implement

- Read the existing files in the layer before touching them
- Use the relevant skill for the layer:

  | Layer                                     | Skill                |
  | ----------------------------------------- | -------------------- |
  | Schema / SQL                              | `write-sql`          |
  | Proto                                     | `write-proto`        |
  | DAO, services, handlers, lib, cmd, pkg/go | `write-go-service`   |
  | Tests (Go, all layers)                    | `write-go-tests`     |
  | OpenAPI / docs                            | `write-openapi`      |
  | pkg/js (JS/TS client + tests)             | `write-js-package`   |
  | Dockerfiles                               | `write-dockerfiles`  |
  | Shell scripts                             | `write-bash-scripts` |
  | Git operations                            | `git-conventions`    |

- **After any proto or interface change, run `pnpm generate:go`** to regenerate protobuf Go bindings
  and Go interface mocks. Commit the generated files (`internal/models/proto/gen/`, `internal/handlers/mocks/`, `internal/core/mocks/`)
  in the same commit as the change that necessitated them — never in a separate cleanup commit.
- **Only change what the feature requires.** No refactoring, no style fixes, no "while we're here"
  improvements alongside feature work. Those are separate commits on a separate branch.
- Keep diffs small and reviewable. A handler + its test + the mock update = one commit.

### 3.3 Test

Run the narrowest target that covers the changed layer:

```bash
a-novel test --type=go -y    # Go: DAO, services, handlers, lib + pkg/go
a-novel test --type=pnpm -y  # pkg/js (containerised service env, auto-managed)
```

Tests must pass before declaring the branch ready. Never mark a branch done with failing tests.

### 3.4 Commit

Follow `git-conventions`. One commit per logical unit within the branch:

- Migration file → one commit
- DAO file + its test → one commit
- Service file + its test → one commit
- Handler file + its test + mock update → one commit
- `openapi.yaml` + pkg/js changes → one commit (they always change together)

```bash
git add <specific files>
git commit -m "feat(dao): add revoke repository"
```

### 3.5 Wait for developer approval

**Do not proceed to the next branch until the developer says the current one is ready.** Present:

- A brief description of what changed
- The test result
- Any decisions made (e.g., "I chose to add ErrJwkRevokeNotFound rather than reuse ErrJwkDeleteNotFound because…")
- Any open questions or deferred work

### 3.6 Give the workspace back

Once **every** branch is reviewed and approved — the last one included — a stack you allocated in
Phase 0 has nothing left to do. Prune it:

```bash
a-novel core stacks prune <name>
```

Approval is the trigger, not a green pipeline: review produces work, and a stack rebuilt to answer
one comment costs more than one kept a few hours longer. `prune` refuses while any checkout still
holds unpushed or uncommitted work, so running it early is safe — it tells you what is unsaved
instead of discarding it.

Skip this entirely when you worked in the default stack. That one is the workspace, and `prune`
refuses it by design.

---

## Phase 4: Backtracking

If the developer requests a change to branch N after branch N+1 (or later) is already open:

```bash
# 1. Return to the parent branch
git checkout feat/<parent-area>/<description>

# 2. Make the change and commit it
git add <files>
git commit -m "fix(<area>): <what changed>"

# 3. Rebase the child branch onto the updated parent
git checkout feat/<child-area>/<description>
git rebase feat/<parent-area>/<description>

# Resolve any conflicts, then continue
git rebase --continue
```

If more than one child branch depends on the updated parent, rebase them shallowest-first (direct
child onto the updated parent first, then its children in order). Each branch is touched exactly
once; deepest-first would require re-rebasing intermediate branches after their own parents move.

Never merge a parent branch into a child — always rebase. Merges add noise to the history and
make the final PR harder to review.

---

## Key Principles

**Surgical changes.** Every line changed must be required by the feature. If you find yourself
fixing an unrelated issue, stop: note it as a separate improvement and continue on the feature.

**No side-effects.** Refactoring, style fixes, and "improvements" that are not part of the feature
belong in a separate branch with a separate commit. Mixed changes make PRs hard to review and
bisect.

**Additive over destructive.** Prefer adding new fields, methods, and endpoints over removing or
changing existing ones. When removal is unavoidable, mark it `BREAKING CHANGE` and flag it to the
developer.

**One concern per commit.** A commit should answer exactly one "what changed?" question. If you
cannot describe a commit in a single conventional-commit line, it contains more than one concern.

**Test every branch.** The relevant test target must pass on every branch, not just the final
one: `a-novel test --type=go -y` for Go layers (internal + pkg/go), `a-novel test --type=pnpm -y` for pkg/js.
A branch that compiles but fails tests is not ready for review.

**Verify before proposing.** Never propose a plan based on assumed file locations or signatures.
Read the code first. A plan built on wrong assumptions wastes the developer's review time.

---

## Quick Reference: Feature Triage

| Signal                                | Implication                                                      |
| ------------------------------------- | ---------------------------------------------------------------- |
| "Add a new gRPC RPC"                  | Proto → handler → pkg/go (at minimum)                            |
| "Add a new REST endpoint"             | handler → OpenAPI + pkg/js (at minimum)                          |
| "Add a new column / store new data"   | Migration → DAO → service (at minimum)                           |
| "Change what an existing API returns" | Potential breaking change — flag it                              |
| "Remove something"                    | Breaking change — get explicit developer approval                |
| "Internal only, no API change"        | Core/lib only, single branch likely fine                         |
| "Fix a bug in existing behaviour"     | Fix the failing layer; test the contract                         |
| "The client should be able to do X"   | Start from the relevant client (pkg/go or pkg/js) and trace down |

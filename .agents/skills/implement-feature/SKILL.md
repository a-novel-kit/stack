---
name: implement-feature
description: >
  Implement a change to an a-novel backend SERVICE repo using a layered branch strategy. Use whenever
  implementing a new API endpoint, schema change, business logic, or client update. Covers branch
  decomposition, per-branch testing, and backtracking. Run plan-feature first for non-trivial,
  multi-repo, or architectural work. Backend services only — not platform (frontend) repos.
---

# Feature Implementation Workflow

Feature work in a-novel **backend services** (the `service-*` repos with the
`cmd`/`internal/...`/`pkg` clean-architecture layout) runs through three gated phases:
**Assess → Plan → Implement**. Phase 4 covers recovery when an earlier branch needs to change.

> **Where the design comes from.** For non-trivial, multi-repo, or architectural work, `plan-feature`
> settles the design first and produces a **planning issue** (an Epic, or a Feature with its Task
> sub-issues). This skill executes it: the **Plan** phase below is the per-repo **branch
> decomposition** of that agreed design, typically one branch/PR per Task sub-issue. Link every PR to
> its issue with a `Closes #<n>` line, so merging closes the unit and advances the Epic's sub-issue
> progress. A PR under an Epic spanning repos also carries the `epic:<N>` label and lands under the
> gates in "Landing a Task that belongs to an Epic" below. Small, unambiguous changes start directly
> here.
>
> **Scope: backend services only.** Frontend **`platform`** repos are deliberately more monolithic, so
> the layer-by-layer branch decomposition here does **not** apply to them; their authoring conventions
> arrive in a later stage. Don't force a platform change through this workflow.

---

## Phase 0: Before Writing Any Code

**Check the workspace is yours.** Every checkout the task will touch must be on `master` with a
clean tree — see `git-conventions` › Workspace Hygiene. A dirty tree or a foreign unmerged branch
means another session is already working there: allocate your own stack with
`a-novel core stacks new <name>` rather than building on top of their work, and prune it at the
end of the lifecycle (3.6).

**Clarify ambiguous requests first.** If the request is broad ("improve the service", "refactor
this area") or reads several ways, ask one focused question before reading any code. A plan built on
a misunderstood requirement wastes read and write effort alike.

**Read the code that will change.** Never guess at signatures, error types, or interfaces. Before
proposing a plan, read:

- The production files in every affected layer
- The test files for those layers (they document the contract)
- Any SQL migrations that the change builds on

Use Grep and Glob when you are unsure which files are involved.

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
handlers, and the JS client (`pkg/js/rest/`) are three representations of the same contract, so a
change to one updates all three in the same feature. A PR that updates one without the others must
justify the omission. Divergence is a bug.

### Does it break anything?

A change is breaking if it makes existing callers fail without code changes on their side. Flag
these explicitly — they require a BREAKING CHANGE commit footer and a warning to the developer.

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

Additive changes (new endpoint, new field, new service) ship safely in parallel with existing code.
Modifications (error mapping, response shape, a bug fix) may affect existing callers and need extra
care.

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

**When a single branch is enough**: the feature touches one layer (or the coupled OpenAPI + pkg/js
pair, which always moves as one unit), creates no migration, and changes no proto. Improvement rounds
(multi-layer bug fixes, test additions, doc updates, code cleanup) with no user-facing behavior
change also stay on a single branch — splitting them would be churn with no review benefit.

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

Branch from master whenever possible. Branch from a parent only when the work cannot compile without
that branch's changes.

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
  in the same commit as the change that required them — never in a separate cleanup commit.
- **Only change what the feature requires** (see "Surgical changes" under Key Principles).
- Keep diffs small and reviewable. A handler + its test + the mock update = one commit.

### 3.3 Test

Run the narrowest target that covers the changed layer:

```bash
a-novel test --type=go -y    # Go: DAO, services, handlers, lib + pkg/go
a-novel test --type=pnpm -y  # pkg/js (containerised service env, auto-managed)
```

Tests must pass before the branch is ready. Never mark a branch done with failing tests.

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
holds unpushed or uncommitted work, so running it early is safe — it names what is unsaved instead
of discarding it.

Skip this when you worked in the default stack. That one is the workspace, and `prune` refuses it by
design.

---

## Landing a Task that belongs to an Epic

An Epic's Tasks land **as a unit or not at all** — the Epic Atomicity Rule, enforced by required
checks rather than convention. What follows is what a Task author needs; `coordinate-landing` owns
the full saga.

**One Task per repo per Epic.** An Epic touching three repos has three Tasks and three pull requests.
Two pieces in the same repo belong to the same Task; work that cannot land concurrently belongs to a
different Epic.

**Membership is the `epic:<N>` label on the pull request** — not the `Closes` keyword, which the author
controls. Applying a label needs triage rights, so the label is a permission-gated claim. Add it when
you open the pull request.

**Draft means not ready.** The gate reads draft state as your readiness signal, so a draft holds the
whole Epic. Mark ready when the Task is genuinely done, not to start review.

**`merge-gate` red usually is not about you.** It holds every member until _all_ of them are
non-draft and approved, across every repo, so a red gate most often means a sibling is not ready yet.
Check the check's summary — it names what it is waiting on — before assuming your branch is at fault.

**`epic-freeze` red means the Epic landed partially.** Some siblings merged and one did not, so the
rest are frozen to stop the split widening. Do not work around it: do not re-run the check hoping for
green, and never re-enable auto-merge by hand. The sweep re-derives from live state every ~15 minutes
and clears the freeze once the Epic is whole again.

**To stop a landing rather than let it resume,** label the Epic issue `automation:paused`. That is the
latch — a freeze only describes the Epic's current shape and lifts as soon as that shape is healthy.

**Rolling back a landed Epic is human-triggered and rare.** If you think you need it, that is a
`coordinate-landing` conversation, not something to attempt from here.

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

Always rebase a child onto its updated parent; merging the parent in adds noise to the history and
makes the final PR harder to review.

---

## Key Principles

**Surgical changes.** Every line changed must be required by the feature. Refactoring, style fixes,
and "while we're here" improvements belong on a separate branch with their own commit — mixed
changes are hard to review and bisect. When you spot an unrelated issue, note it as a separate
improvement and continue on the feature.

**Additive over destructive.** Prefer adding new fields, methods, and endpoints over removing or
changing existing ones. When removal is unavoidable, mark it `BREAKING CHANGE` and flag it to the
developer.

**One concern per commit.** A commit answers exactly one "what changed?" question. A commit you
cannot describe in a single conventional-commit line contains more than one concern.

**Test every branch.** The relevant test target must pass on every branch, not just the final
one: `a-novel test --type=go -y` for Go layers (internal + pkg/go), `a-novel test --type=pnpm -y` for pkg/js.
A branch that compiles but fails tests is not ready for review.

**Verify before proposing.** Read the code first; a plan built on assumed file locations or
signatures wastes the developer's review time.

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

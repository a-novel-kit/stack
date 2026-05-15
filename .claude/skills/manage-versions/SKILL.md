---
name: manage-versions
description: >
  Cross-repo version management for the a-novel and a-novel-kit organizations. Load this skill
  whenever a change spans more than one repo and the repos must stay version-compatible to merge —
  e.g. a service needs an unreleased `golib` (or other a-novel-kit / a-novel) change, a `pkg/go`
  client change rippling into its consumers, a proto or REST contract change, or any breaking
  change to a published symbol. Covers: how repos are versioned (git-tag semver, exact `go.mod`
  pins, no `replace` directives, tag-push releases, Renovate auto-bumps); how to develop a
  consumer against an unreleased dependency by pinning to a commit SHA (pseudo-version); the
  hard rule that the dependency PR merges and releases FIRST and the consumer re-pins to the
  released tag before merging; and the rule that breaking changes are always STAGED — add the new
  path non-breaking, release, migrate every consumer, then remove the old path in a later release.
  Pairs with `git-conventions` (breaking-change footers), `implement-feature` (the branch-stack
  workflow), `open-pull-request`, `write-go-kit`, `write-go-service`, `write-proto`, `write-openapi`.
---

# Cross-repo version management

Every repo in `a-novel` and `a-novel-kit` is independently versioned, and the repos depend on each
other (services pin `golib`, `jwt`, each other's `pkg/go`; deployments pin each other's Docker
images; clients pin published packages). When a change touches more than one repo, the versions
have to line up before anything can merge to production. This skill governs that.

Load it together with whichever repo-kind skill applies (`write-go-kit` for a `kit/` change,
`write-go-service` for a service change) and with `git-conventions` (the breaking-change commit
footer) and `implement-feature` (the layered-branch workflow these cross-repo plans extend).

---

## How things are versioned

- **Each repo is git-tag semver.** A release is a `vX.Y.Z` git tag created on a `master` commit
  and pushed; the next version is read from the conventional-commit history since the last tag
  (`fix:` → patch,
  `feat:` → minor, a `BREAKING CHANGE:` footer or `!` → major — see `git-conventions`). The tag
  push triggers the `release` workflow (`a-novel-kit/workflows/publish-actions/auto-release`),
  which cuts the GitHub Release and, for service repos, builds and publishes the Docker images /
  npm packages tagged with that version.
- **`go.mod` pins exact released tags.** `github.com/a-novel-kit/golib v0.20.31`, not a range, not
  a branch. **No `replace` directives** are used anywhere in this codebase — the dependency graph
  is always reproducible from `go.mod` alone.
- **Renovate keeps consumers current.** After a dependency releases, Renovate opens a PR in each
  consumer bumping the pin (and runs the configured post-upgrade tasks: `go mod tidy`, regenerate
  proto, etc.). So in the _normal_ flow you never hand-edit a dependency pin — Renovate does it.
  The manual dance below is only for an **in-flight** cross-repo change that can't wait for a
  release + the next Renovate cycle.
- **Docker-image pins** in CI / compose files follow the same rule: pin a concrete `vX.Y.Z` (or
  `${{ github.ref_name }}` for the repo's own image), never `latest` or a branch.

---

## Working a cross-repo change on a branch

You're changing repo **A** (the dependency — e.g. `golib`) and repo **B** (the consumer — e.g. a
service) together, and B's branch needs A's _unreleased_ code. Don't add a `replace`; pin B to A's
commit SHA, which Go records as a **pseudo-version**:

```bash
# In B, on B's branch, after pushing A's branch:
go get github.com/a-novel-kit/golib@<commit-sha-on-A's-branch>
go mod tidy
# go.mod now reads e.g.  github.com/a-novel-kit/golib v0.20.32-0.20260512140000-abcdef123456
```

- Pin to an **explicit commit SHA**, not `@<branch-name>` — a branch ref resolves to whatever the
  tip is _at the moment you run `go get`_ and can silently drift; an SHA is stable. (`go get @main`
  also produces a pseudo-version, just a less reproducible one.)
- Each time you push more commits to A's branch, re-run `go get …@<new-sha>` + `go mod tidy` in B
  so B builds against the latest A.
- B's CI will be **red on a pseudo-version pin** if the org enforces "released versions only" — and
  it should be. That is the signal that B is not yet mergeable; see the ordering rule.

---

## The merge order — dependency first, always

A pseudo-version is a pointer to a commit on a branch. Whether that exact commit still exists on
`master` after the branch merges depends on the merge strategy — a merge commit keeps it; a
squash- or rebase-merge does not, and once the branch is deleted GitHub serves the orphaned commit
only for a while. Either way, a PR that _merges_ with a pseudo-version pin bakes a non-release into
production — unreproducible from tags, invisible to Renovate, and a landmine once the source commit
is gone. **A pseudo-version pin is only ever safe during in-flight development, never at merge
time.** So:

**Hard rule: the dependency PR merges and releases before the consumer PR merges; the consumer
re-pins to the released tag first.**

1. **Merge A's PR** (the dependency change) to A's `master`. The release workflow tags `vX.Y.Z`
   and publishes it.
2. **In B's branch, re-pin to the release**: `go get github.com/a-novel-kit/golib@vX.Y.Z`
   (or `@latest`), `go mod tidy`, commit (`chore(deps): bump golib to vX.Y.Z`). B's CI goes green.
3. **Merge B's PR.** Now production is fully on released versions.

If B's change is itself a dependency of a repo C, repeat: A → release → B re-pins → B releases →
C re-pins → C merges. Stacked dependency chains merge bottom-up.

**Write the order down** so it can't get lost — in B's PR description, and/or as `TaskCreate`
entries:

```
1. golib#NNN  (adds X)                  → merge → release v0.20.32
2. service-foo#MMM  (uses X)            → re-pin golib → v0.20.32, then merge
3. (if needed) golib#PPP  (removes old X) → after #2 ships → release v0.21.0
```

> Edge case — A is `golib` (no separate "release PR", releases on tag): the maintainer pushes the
> tag after A's PR merges. Don't merge B until that tag exists and B is pinned to it. If you don't
> control the tag push, hand off: "golib#NNN is merged; needs a release tag before
> service-foo#MMM can land — re-pin and merge once `vX.Y.Z` is out."

---

## Breaking changes are staged — never broken in one shot

A breaking change to anything other repos depend on — a `golib` / graduated-package public symbol,
a service's `pkg/go` API, a proto message, a REST endpoint, a DB schema another reader relies on —
is **never** done in a single PR that flips old → new. It's staged across releases so there is
always a window where both old and new work.

### Step 0 — Grep the whole workspace for consumers _first_

Breaking changes are fine when the change is genuinely needed — a better API, a security fix that
required a signature change, an obsolete helper a newer dependency now covers. The discipline this
skill enforces is **knowing the blast radius before you start**, so the consumer-migration step
isn't a scramble.

**Before** writing the additive PR, enumerate every consumer of the symbol you're about to change.
The output of that grep:

- Tells you the **blast radius** — one consumer is a different change than ten, but the survey
  itself is the same work either way.
- Seeds the **consumer-migration list** in step 2 below, which has to be tracked explicitly so
  no consumer gets forgotten and silently fails at step 3 (removal).
- Surfaces the occasional case where the "shared" symbol turns out to have **zero in-org
  consumers** — in which case the additive-then-deprecate dance is pointless and the symbol can
  often just be removed directly, or where it has **one** consumer (which `write-go-kit` flags
  as below the kit-inclusion bar, suggesting inlining rather than rename-and-keep).
  Knowing this lets you pick the right shape; it does not stop you from making the change.

Concrete grep commands from the workspace root (`/home/kushuh/git-projects/a-novel`), tailored to
the symbol kind:

```bash
# A Go function / type / constant — name only, captures all import paths.
grep -rn "SymbolName" app/ kit/ --include='*.go'

# A proto message or RPC.
grep -rn "MessageName" app/ kit/ --include='*.proto' --include='*.go'

# A REST endpoint path or query parameter.
grep -rn 'apiPath' app/ kit/

# A renamed type — survey BOTH old and new names so consumer files using either show up.
grep -rn "HttpConfig\|HTTPConfig" app/ kit/ --include='*.go'

# A field on an exported struct (don't forget composite-literal usage).
grep -rn "\.FieldName\|FieldName:" app/ kit/ --include='*.go'
```

Search both `app/` (services) and `kit/` (other shared libraries) — a graduated-package change
can rip through `jwt` consumers as easily as service consumers. Capture the file list as a checklist
in the dependency PR's description, under a heading like "Consumer migration follow-ups", so it
travels with the PR through review.

Skipping this step has a specific failure mode: the dependency PR merges and releases, then someone
discovers a consumer that wasn't on the radar weeks later — usually when it suddenly fails to
compile after the removal step. Catch it now, not then.

### The staged path itself

1. **Add the new path, non-breaking.** New function / field / endpoint / column alongside the old.
   The old path keeps working unchanged. Release this (a `feat:`, _not_ a breaking change — nothing
   broke yet). Mark the old path `// Deprecated: use NewThing` so consumers and pkg.go.dev see it.
2. **Migrate every consumer to the new path.** One PR per consumer (drawn from the step-0 list),
   each re-pinning to the release from step 1 (per the merge-order rule), each releasing once
   green. Tick consumers off the list as their PRs land — when the list is empty, you know you're
   safe for step 3.
3. **Remove the old path.** Once _every_ consumer is off it, a follow-up PR in the dependency deletes
   the old symbol/field/endpoint/column. _This_ is the breaking change — now safe, because nothing
   uses the old path anymore. Release it (a major bump for a graduated package / `pkg/go`; for a
   service's internal-only contract, the version policy still applies).

The major-version bump in step 3 is for **signalling** — it tells consumers "the old path is gone".
It does not license skipping steps 1–2: a major bump that strands consumers is still a broken
release.

This shape recurs everywhere; the layer skills carry the specifics:

- **Go public APIs** (`golib`, graduated packages, `pkg/go`): add → `// Deprecated:` → migrate →
  remove. Never change a function signature in place; add a new function.
- **Proto** (`write-proto`): never reuse a field number, never change a field's type, never remove
  a field that's still consumed — add new fields/messages/RPCs; mark old ones `// Deprecated`;
  remove only after every gRPC consumer is updated. `buf breaking` enforces a lot of this.
- **REST API** (`write-openapi`): add new endpoints/parameters/fields; keep old ones working;
  document the deprecation; remove only after clients have migrated. Optional request params and
  additive response fields are non-breaking; removing or repurposing either is breaking.
- **DB migrations** (`write-sql`): additive first (new nullable column, new table, new index) — and
  never modify a `.up.sql` that has merged to `master`; a "change" is a new migration. A
  destructive migration (drop column, narrow a type) ships only after the code that read the old
  shape is gone.

Use the `BREAKING CHANGE:` commit footer (`git-conventions`) only on step 3 — the actual removal —
not on step 1.

---

## Quick checklist

When a change spans repos:

- [ ] Identify the dependency direction (who imports/consumes whom) and order the work bottom-up.
- [ ] On the consumer branch, pin to the dependency's **commit SHA** while developing
      (`go get module@<sha>`, then `go mod tidy`) — never a `replace`, never `@branch`.
- [ ] Is this a breaking change to a shared contract? → stage it (add → deprecate → migrate →
      remove); the current PR is the _additive_ step only.
- [ ] For any breaking change to a public symbol, **`grep -rn` the whole workspace** (`app/` _and_
      `kit/`) for consumers before writing the dependency PR. Paste the file list into the PR
      description under "Consumer migration follow-ups" so it travels with the PR through review.
- [ ] Write the merge order in the PR description and/or `TaskCreate`.
- [ ] Merge the dependency PR → wait for its release tag → re-pin the consumer to the released
      version (`chore(deps): bump … to vX.Y.Z`, `go mod tidy`) → confirm CI green → merge the
      consumer. Repeat up the chain.
- [ ] Never merge a PR whose `go.mod` (or Docker-image pin) points at a pseudo-version, a branch,
      or `latest`.

---

## Common pitfalls

- **`replace` directive to wire in a local/branch dependency.** Not used here — pin to a commit
  SHA (pseudo-version) instead, and re-pin to the released tag before merging.
- **Merging a consumer on a pseudo-version pin.** The pin must be a released `vX.Y.Z` at merge
  time. A red "released versions only" check is telling you the truth.
- **Merging the consumer before the dependency is released.** Dependency PR → release → consumer
  re-pins → consumer merges. In that order.
- **One PR that flips a public API / proto / endpoint / column from old to new.** Stage it: add
  the new path non-breaking, migrate consumers, remove the old path later. There must always be a
  both-work window.
- **`BREAKING CHANGE:` footer on the additive step.** It goes on the _removal_ step (step 3), the
  PR that actually breaks something.
- **A major bump used as a shortcut.** It signals "old path removed"; it doesn't excuse not having
  migrated consumers first.
- **Hand-editing a dependency pin in the normal flow.** Renovate bumps consumers after a release —
  let it. The manual re-pin is only for an in-flight cross-repo change.
- **Forgetting the chain.** If the consumer is itself a dependency, the same dance continues one
  level up. Write the whole order down.
- **Skipping the workspace-wide consumer grep before introducing the breaking change.** The
  staged-removal dance only works if every consumer migrates between steps 1 and 3. Discovering a
  forgotten consumer after step 3 has shipped means a broken build (or worse, a runtime hole) for
  someone downstream. Grep `app/` and `kit/` for the symbol _before_ writing the additive PR and
  paste the file list into the PR body.

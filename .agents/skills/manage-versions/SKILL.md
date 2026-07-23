---
name: manage-versions
description: >
  Cross-repo version management for a-novel / a-novel-kit. Load it whenever a change spans repos
  that must stay version-compatible to merge — an unreleased `golib` or `pkg/go` dependency, a
  proto or REST contract change, a breaking change to a published symbol. Covers git-tag semver,
  exact `go.mod` pins, commit-SHA pseudo-versions, the dependency-releases-FIRST merge order (the
  consumer re-pins to the released tag), and staged rollouts. Pairs with `git-conventions`,
  `implement-feature`, `open-pull-request`, `write-*`.
---

# Cross-repo version management

Every repo in `a-novel` and `a-novel-kit` is independently versioned, and the repos depend on each
other (services pin `golib`, `jwt`, each other's `pkg/go`; deployments pin each other's Docker
images; clients pin published packages). When a change touches more than one repo, the versions
must line up before anything merges to production. This skill governs that.

Load it with whichever repo-kind skill applies (`write-go-kit` for a `kit/` change,
`write-go-service` for a service change), with `git-conventions` (the breaking-change commit
footer) and `implement-feature` (the layered-branch workflow these cross-repo plans extend).

---

## How things are versioned

- **Each repo is git-tag semver.** A release is a `vX.Y.Z` git tag pushed on a `master` commit; the
  next version comes from the commit history since the last tag (`fix:`/`chore:` → patch, `feat:` or
  an absorbable breaking change → minor). A `!` does **not** force a major here: a major is a planned
  `vX`-line initiative, never derived from a commit range — `prepare-release` owns that policy and the
  sizing. The tag
  push triggers the `release` workflow (`a-novel-kit/workflows/publish-actions/auto-release`),
  which cuts the GitHub Release and, for service repos, builds and publishes the Docker images /
  npm packages tagged with that version.
- **`go.mod` pins exact released tags.** `github.com/a-novel-kit/golib v0.20.31`, not a range, not
  a branch. **No `replace` directives** anywhere in this codebase — the dependency graph stays
  reproducible from `go.mod` alone.
- **Renovate keeps consumers current.** After a dependency releases, Renovate opens a PR in each
  consumer bumping the pin and running the configured post-upgrade tasks (`go mod tidy`, regenerate
  proto, …). In the _normal_ flow you never hand-edit a dependency pin. The manual dance below is
  only for an **in-flight** cross-repo change that cannot wait for a release plus the next Renovate
  cycle.
- **Docker-image pins** in CI / compose files follow the same rule: pin a concrete `vX.Y.Z` (or
  `${{ github.ref_name }}` for the repo's own image), never `latest` or a branch.

---

## Cutting a release

A release is cut **in CI**, not from a developer's working tree. A maintainer triggers the repo's
**release workflow** and picks a **release type** (patch / minor / major); the `release-core` action
then runs the end-to-end sequence — bump version files (workspace-aware), refresh doc refs
(`prepublish:doc` / `a-novel publish stamp`), commit, tag `vX.Y.Z`, push commit and tag, create the
GitHub Release (notes, Docker images, npm packages follow). The [Agent] bot performs the push.

There is **no local release command**, and no per-repo `publish.sh` or `publish:*` pnpm script —
do not write or revive any of them. The bump is workspace-aware: `release-core` detects whether the
repo is a pnpm workspace (a `pnpm-workspace.yaml` is present) and runs the matching `pnpm version`
form — `--recursive` for workspaces, `--no-git-tag-version` for single-package repos. The only verb
under `a-novel publish` is `stamp`, the doc-version helper `prepublish:doc` calls (see
`use-a-novel-cli`).

**Who can actually publish is enforced server-side.** Two GitHub settings stack:

1. **Branch protection on `master`** — restricts who can push the version-bump commit.
2. **Tag protection rule on `v*`** (Settings → Tags → New rule) — restricts who can create the
   matching tag. Required because every release workflow triggers on `push: tags: ['**']`: without
   it an unauthorized user can push a `v*` tag from any commit (even one not on `master`) and CI
   releases from it.

A `v*` tag is only ever created by a CI dispatch — the release or release-train job, running as the
[Agent] bot. Nobody pushes one by hand, maintainers included. Repo admins can bypass every rule
here, but that power exists to repair a broken state, never to cut a release.

**Releases are human-triggered but bot-run.** A human starts the release workflow; the agent never
decides to publish (or merge, or push to master) on its own, and a non-interactive token must lack
those rights server-side.

---

## Hotfixing a released line

A bug sometimes must be fixed on an already-released line **without** waiting for — or dragging in —
whatever has since landed on the default branch. That is a **hotfix**: patch the released tag, cut a new
patch on that line, then forward-port the fix to the default branch so a later release can't reintroduce
it. It is a single-repo path; a bug spanning several repos is a fast **Epic** (see `coordinate-landing`).

**Vocabulary** (single-repo hotfix — freeze it here):

- **baseline** — the latest live tag on the target `X.Y` line (e.g. `v1.4.2` for line `1.4`). The hotfix
  cuts from the baseline, never from the default branch.
- **ephemeral branch** — `hotfix/vX.Y.x`, cut from the baseline, carrying the fix; pushed so the cut can
  build it, then deleted. Never long-lived.
- **cut** — `release-core-hotfix` bumps the patch, tags `vX.Y.(Z+1)` (tag-only, `--latest=false`, **no
  default-branch commit**), and creates the Release. It never touches the default branch.
- **reconcile** — forward-porting the fix _diff_ (never the version bump) to the default branch, as a
  squash `hotfix-reconcile/*` PR. A conflict degrades to a **draft** PR for a human to finish.
- **cleanup Task** — the per-hotfix tracking issue (label `hotfix-reconcile`) the reconcile PR `Closes`.
  While it stays open, the fix has **not** reconciled — the watchdog escalates a rotting one.
- **retract** — for a Go module, a `retract vX.Y.Z` directive (added in a later release) marks a bad
  version so `go get` skips it. The tag stays (immutable); `retract` is the downstream "don't use this".

**Cut a hotfix.** Dispatch the repo's **`hotfix.yaml`** from the UI (admin + protected-`release`-env
gated, like `release.yaml`; `dry_run` defaults on). Pick the `X.Y` **line** and the **`fix_ref`** — the
commit or `A..B` range carrying the fix, cherry-picked onto the baseline (empty = a drill that re-cuts
the baseline). Rehearse with `dry_run=true`, then run live: it cuts `vX.Y.(Z+1)`, opens the cleanup
Task, and opens the reconcile PR. **Review + merge the reconcile PR** — until it lands, the cleanup Task
stays open and the fix is not on the default branch. A hotfix is deliberately **exempt from
`AGENT_KILL_SWITCH`**: the emergency patch path stays open even when normal automation is halted
(admin + the `release` env gate it instead).

**Security hotfix (embargoed).** When the fix can't be public before release: work it under a **draft
GHSA advisory** in a **temporary private fork** — a private fork inherits none of the org secrets or
runners, so there is **no CI there** and nothing leaks — coordinate the disclosure, then cut the hotfix
and publish the advisory together, and `retract` the vulnerable version(s) in the follow-up. The
**forward-port must land** (the receipt-gated reconcile), never "the next upgrade will fix it" — a later
minor cut from the unreconciled line silently re-ships the hole. Outward policy (contact, disclosure
window) lives in `SECURITY.md`; the mechanics live here.

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
- B's CI goes **red on a pseudo-version pin** where the org enforces "released versions only", as it
  should: that is the signal B is not yet mergeable. See the ordering rule.

---

## The merge order — dependency first, always

A pseudo-version points at a commit on a branch. Whether that commit survives on `master` depends on
the merge strategy — a merge commit keeps it, a squash- or rebase-merge does not, and once the
branch is deleted GitHub serves the orphaned commit only for a while. Either way, a PR that _merges_
with a pseudo-version pin bakes a non-release into production: unreproducible from tags, invisible
to Renovate, and a landmine once the source commit is gone. **A pseudo-version pin is safe only
during in-flight development, never at merge time.** So:

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

> Edge case — A is `golib`, which has no separate "release PR" and releases on tag: dispatch A's
> release workflow once its PR merges. Don't merge B until that tag exists and B is pinned to it. If
> you cannot dispatch it, hand off: "golib#NNN is merged; needs a release tag before
> service-foo#MMM can land — re-pin and merge once `vX.Y.Z` is out."

---

## Breaking changes are staged — never broken in one shot

A breaking change to anything other repos depend on — a `golib` / graduated-package public symbol,
a service's `pkg/go` API, a proto message, a REST endpoint, a DB schema another reader relies on —
is **never** a single PR that flips old → new. Stage it across releases, so there is always a window
where both old and new work.

### Step 0 — Grep the whole workspace for consumers _first_

Breaking changes are fine when genuinely needed — a better API, a security fix that required a
signature change, an obsolete helper a newer dependency now covers. The discipline this skill
enforces is **knowing the blast radius before you start**, so the consumer-migration step isn't a
scramble.

**Before** writing the additive PR, enumerate every consumer of the symbol you're about to change.
That grep:

- Tells you the **blast radius** — one consumer is a different change than ten, and the survey costs
  the same either way.
- Seeds the **consumer-migration list** in step 2 below, tracked explicitly so no consumer is
  forgotten and fails silently at step 3 (removal).
- Surfaces the case where the "shared" symbol has **zero in-org consumers**, and lets you decide what
  that means for _this particular symbol_. For an internal / not-yet-public helper (a fresh `golib`
  sub-package no service imports yet, a service's private `pkg/go` symbol), zero consumers usually
  means the additive-then-deprecate dance is over-engineering and the symbol can be removed directly.
  For a **graduated / community-facing package** (`a-novel-kit/jwt` and any future graduate — see
  `write-go-kit`'s graduation rules), the workspace grep does **not** see external pkg.go.dev
  consumers, so staged removal still applies at zero in-org hits: the discipline is the public API
  contract, not the consumer count. Similarly, a single in-org consumer of a `golib` helper may
  signal it belongs inlined into that consumer (`write-go-kit`'s "should this go in golib?" bar) —
  a `golib`-specific rule, since graduated packages stay staged regardless.

Grep commands from the workspace root (the directory holding `app/` and `kit/`), by symbol kind:

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

Search both `app/` (services) and `kit/` (other shared libraries) — a graduated-package change rips
through `jwt` consumers as easily as service consumers. Capture the file list as a checklist in the
dependency PR's description, under a heading like "Consumer migration follow-ups", so it travels
with the PR through review.

Skipping this step has a specific failure mode: the dependency PR merges and releases, and weeks
later someone finds a consumer nobody had on the radar — usually when it fails to compile after the
removal step. Catch it now, not then.

### The staged path itself

1. **Add the new path, non-breaking.** New function / field / endpoint / column alongside the old,
   which keeps working unchanged. Release this (a `feat:`, _not_ a breaking change — nothing broke
   yet). Mark the old path `// Deprecated: use NewThing` so consumers and pkg.go.dev see it.
2. **Migrate every consumer to the new path.** One PR per consumer (drawn from the step-0 list),
   each re-pinning to the release from step 1 (per the merge-order rule), each releasing once
   green. Tick consumers off the list as their PRs land; an empty list means step 3 is safe.
3. **Remove the old path.** Once _every_ consumer is off it, a follow-up PR in the dependency deletes
   the old symbol/field/endpoint/column. _This_ is the breaking change, now safe because nothing uses
   the old path. Release it (a major bump for a graduated package / `pkg/go`; for a service's
   internal-only contract, the version policy still applies).

The major-version bump in step 3 is **signalling** — it tells consumers "the old path is gone". It
does not license skipping steps 1–2: a major bump that strands consumers is still a broken release.

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
- [ ] Breaking a shared contract? Stage it (add → deprecate → migrate → remove); the current PR is
      the _additive_ step only.
- [ ] For any breaking change to a public symbol, **`grep -rn` the whole workspace** (`app/` _and_
      `kit/`) for consumers before writing the dependency PR, and paste the file list into the PR
      description under "Consumer migration follow-ups".
- [ ] Write the merge order in the PR description and/or `TaskCreate`.
- [ ] Merge the dependency PR → wait for its release tag → re-pin the consumer to the released
      version (`chore(deps): bump … to vX.Y.Z`, `go mod tidy`) → confirm CI green → merge the
      consumer. Repeat up the chain.
- [ ] Never merge a PR whose `go.mod` (or Docker-image pin) points at a pseudo-version, a branch,
      or `latest`.

---

## When a cross-repo landing fails — the runbook

The saga's recovery _mechanics_ (freeze, roll-forward, rollback, the release train) live in
`coordinate-landing`. This is the **version-side** decision tree: what a stuck or bad cross-repo
landing means for the pins and releases, and how to unwind it. A published release is **immutable**,
so cross-repo recovery is almost always **roll forward** — a new release that supersedes, rather
than rewritten history.

| Symptom                                                 | What happened                                                                          | Do this                                                                                                                                                                                        |
| ------------------------------------------------------- | -------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Consumer PR red on **"released versions only"**         | Its `go.mod` still pins a pseudo-version (a dependency commit SHA), not a released tag | Wait for the dependency to release, re-pin the consumer to the released `vX.Y.Z`, push. Never merge on a pseudo-version.                                                                       |
| **Dependency released, consumer stuck** in the Epic set | Publish-before-rollout half-done: dep shipped, consumer not re-pinned/merged           | Re-pin the consumer to the released tag; the merge-gate lands it with the set. The dep's release is immutable — roll forward, don't roll back.                                                 |
| An Epic **partially landed**                            | INV-1 violated — a member left the queue                                               | The sweep freezes the survivors and rolls a landable stray forward within grace. If it can't land, fix it, or (last resort) `epic-rollback` the landed subset. See `coordinate-landing`.       |
| A **bad version shipped** (released, then found broken) | A release is immutable — you can't un-publish                                          | Roll **forward**: cut a patch that reverts the change, or `epic-rollback` the Epic (it lands compensating reverts and _re-releases_). Never delete a published tag a consumer may already pin. |
| The **release train left a repo un-released**           | A dispatch/approval failed mid-train                                                   | Re-dispatch the train — it resumes from live tags and re-cuts only the unshipped. A parked run (release-env approval) is pending; approve it, then re-dispatch to record the receipt.          |
| A **contract-step removal broke a consumer**            | A consumer was missed in the expand→contract migration                                 | Its build is red against the new dep release. Fast-follow: migrate the missed consumer and release it; until then it pins the _previous_ dep release.                                          |

`epic-rollback` is the one sanctioned "undo," and even it works by landing **new** compensating
commits and then re-releasing — never by rewriting a tag.

## Common pitfalls

- **`replace` directive for a local/branch dependency.** Pin to a commit SHA instead, then re-pin to
  the released tag before merging.
- **Merging a consumer on a pseudo-version pin.** The pin must be a released `vX.Y.Z` at merge time;
  the red "released versions only" check is telling you the truth.
- **Merging the consumer before the dependency is released.** Dependency PR → release → consumer
  re-pins → consumer merges, in that order.
- **One PR that flips a public API / proto / endpoint / column old → new.** Stage it, so there is
  always a both-work window.
- **`BREAKING CHANGE:` footer on the additive step.** It goes on the removal step (step 3).
- **A major bump used as a shortcut.** It signals "old path removed"; it does not excuse skipping
  the consumer migration.
- **Hand-editing a dependency pin in the normal flow.** Renovate bumps consumers after a release;
  the manual re-pin is only for an in-flight cross-repo change.
- **Forgetting the chain.** A consumer that is itself a dependency continues the dance one level up;
  write the whole order down.
- **Skipping the step-0 consumer grep.** A consumer found after step 3 shipped is a broken build, or
  worse a runtime hole, downstream.

---
name: prepare-release
description: >
  Predict a release's size and write its consumer migration guide. Use before a release or when
  asked about version bumps, majors, or migration needs; never tag, push, or publish.
---

# Prepare a release: predict its size and write its migration guide

A release sizes itself, so nobody picks patch, minor or major. This skill answers the two questions
left before anyone clicks "Run":

1. **What will it ship as?** A patch or a minor, read from the commit history as `release-core`
   reads it, or a major when it runs on a `v<N>` branch (step 2).
2. **What must a consumer do to adopt it?** Captured as a **migration guide** that ships with the
   code and lives forever, so the answer is never lost in a PR description or a Slack thread.

You are advisory: read the diff, predict the version, write the guide. Core or the repository's
`owners-<class>` review and cut the release from the Actions tab. You never tag, push, or publish:
releases are human-only (see `manage-versions`).

---

## 1. Read the unpublished diff

Find the last release and everything since, on the branch you'll release from (the default branch):

```bash
git fetch --tags origin
last=$(git tag --list 'v*' --sort=-v:refname | head -1)   # or <subdir>/v* for a sub-dir module
git log --no-merges --pretty='%s' "$last..origin/master"
```

Read the **subjects** (the conventional-commit type/scope/`!`) and, for anything that might break or
deprecate, the **body/footers** (`BREAKING CHANGE:`). A squash-merge repo has one commit per PR, so
the subjects are the PR titles, usually enough; open the PR/diff when a subject is ambiguous about
consumer impact.

---

## 2. Predict the size

`release-core` applies this mapping, a **local variant** of Conventional Commits, and the **highest**
wins across the range:

| Commit signal                                   | Size      |
| ----------------------------------------------- | --------- |
| any `feat:`, `!` or `BREAKING CHANGE:` footer   | **minor** |
| anything else, or nothing since the last tag    | **patch** |
| no tag yet: the repository's first release      | **minor** |
| the release runs on a `v<N>` branch (see below) | **major** |

A type counts on the subject alone, since a squash merge's body repeats its branch's own commits. A
`BREAKING CHANGE:` footer counts anywhere in the message.

**A major is never derived from the commit history.** This is the deliberate departure from stock
Conventional Commits: a `!` or a `BREAKING CHANGE:` footer sizes a release to minor. A breaking
change small enough to absorb in a normal release stays one: removing a capability (especially one
that no longer works), dropping a dead action, tightening a validation. It ships with a migration
guide (below), because it still asks a consumer to act. If a `!` in the range warrants a major, stop
and raise an initiative. Name a break plainly in your prediction: _"minor, carries one small breaking
change (#NNN removes the dead `foo` action)"_.

State the predicted version plainly, e.g. _"`v1.0.3` → `v1.1.0` (minor: three `feat:`)"_, and name
the one or two commits that drove it.

### A major comes from its branch

A major is a **planned, initiative-led migration on its own `v<N>` branch**: a global sweep, a
paradigm shift, a change whose migration is large enough to track as its own initiative. There is no
pre-1.0 special case.

- **`N` is exactly the default branch's major plus one.** Any other `N` is refused. An admin creates
  the branch, and changes reach it through reviewed pull requests, because the `release-lines`
  ruleset guards it.
- **Cut it by dispatching `release.yaml` on that branch.** One atomic push makes the branch the new
  `master`, keeps the old `master` as `v<N-1>`, removes the `v<N>` name and cuts `vN.0.0`.
- **The branch need not contain `master`.** A massive revamp may share no history with it, so
  whatever the branch lacks stays behind on `v<N-1>`. Compare them before the cut.
- **A Go module must already carry the new path.** From major 2 on, every `go.mod` must declare a
  module path ending in `/v<N>`, or the cut is refused.
- **Land or close the pull requests into `v<N>` first.** Removing the branch closes them.

---

## 3. Decide whether a migration guide is needed

A guide documents **consumer action**, not a changelog. Use this gate:

| Release shape                                                                              | Guide?          |
| ------------------------------------------------------------------------------------------ | --------------- |
| A major (the planned `v<N>` migration)                                                     | **Required**    |
| Minor carrying an absorbable breaking change (a removal, a tightened validation)           | **Required**    |
| Minor that **deprecates** a path, adds a **preferred** alternative, or recommends a change | **Recommended** |
| Minor that is purely additive (new optional thing, no consumer change)                     | No (notes only) |
| Patch (bug/perf/internal)                                                                  | No              |

The middle row is the subtle one: a backward-compatible release can still _ask_ consumers to move
(a new recommended input, a renamed-but-aliased field, a new pattern). Nothing forces the change, but
the migration is real and worth writing down once. When in doubt for a minor, ask: _"is there anything
a consumer should change to fully benefit, even though nothing breaks?"_ If yes, write the guide.

---

## 4. Write the guide

A branch cannot know the version it ships in, so it never names one.

- **Path:** `docs/migrations/NEXT.md`. Create it when absent. When another branch already started it,
  add this branch's changes to it.
- **Named at release.** Every release, hotfixes included, renames it to `vX.Y.Z.md` in its publish
  push, replaces the word `NEXT` inside it, and adds the paragraph under its title to the
  `docs/migrations/README.md` list, newest first. Never edit that index by hand: it is a list so a
  release can extend it without breaking prettier's alignment of a table.
- **One file per release, immutable once shipped.** Never a single growing `UPGRADING.md`: a released
  guide maps 1:1 to its tag. Never edit a released guide; a correction goes in `NEXT.md`, so history
  stays truthful.
- **Committed _with_ the change**, so the released tag already contains its own guide.

### Template

```markdown
# Upgrading to NEXT

One-line summary of what changed and why it matters to a consumer.

## Do I need to act?

**Required / Recommended / No**, and the one-sentence reason. If "No", say why the release is safe to
take as-is and stop here.

## <Change 1: imperative title, e.g. "Switch bot inputs to client_id">

What changed and **why**. Then the concrete migration, before → after:

\`\`\`yaml

# before

app_id: ${{ vars.AGENT_BOT_ID }}

# after

client_id: ${{ vars.AGENT_BOT_CLIENT_ID }}
\`\`\`

Any **prerequisite** (a new variable, a new permission), whether the old path **still works**
(deprecated vs removed), and how to **verify** the migration landed.

## <Change 2 …>

…

## Notes

Anything optional, related follow-ups, or pointers to a larger effort this is part of.
```

Lead with **"Do I need to act?"**: most readers want exactly that, and a clear "No" for a safe minor
is a feature. Keep each change section to _what changed → why → before/after → caveats_.

---

## 5. Present: advisory only

Hand the operator the **predicted version**, the **commits that drove it**, and whether a **guide**
was written. Then stop. Core or the repository's owners cut the release (Actions ▸ `release` ▸ Run
workflow, on the default branch or on the `v<N>` branch of a major); `release-core` sizes it, stamps
versions, tags, and publishes. A `dry_run` shows the version and tags it would cut. You do not run
it.

If the change ships in stages or spans repos, that ordering is a `manage-versions` concern: note it
and defer.

---

## Principles

- **The guide is consumer-facing.** Release notes list _what changed_; the guide says _what to do
  about it_. A change needing no consumer action belongs in the notes.
- **A branch never names a version.** It writes `NEXT.md`, and the release names the file.
- **The commit history is the size.** If the history is wrong (a `feat` mislabeled `fix`), fix the
  discipline (`git-conventions`); there is no override.
- **A major is never derived; it is planned.** No commit range makes a major. It is an initiative with
  its own `v<N>` branch that becomes master when it lands.
- **Advisory, never the trigger.** You predict; core or the repository's owners release.

---

## How it composes

```
prepare-release ─ (reads the unpublished diff) ─> predicted version + docs/migrations/NEXT.md
      │
      ├─ git-conventions  : the commit types the size reads
      ├─ manage-versions  : versioning mechanics, staged/cross-repo rollouts the guide may reference
      └─ release workflow : a person runs it; it sizes the release and names the guide in its push
```

A migration guide written here is the **instruction**; rolling it out across consumer repos is
execution (`implement-feature` / `manage-versions`), often tracked as its own follow-up issue.

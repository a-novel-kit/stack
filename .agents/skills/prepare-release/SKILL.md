---
name: prepare-release
description: >
  Run BEFORE a repo is released, to size the release and write its migration guide. It reads the
  unpublished diff (commits since the last tag) and (1) proposes the semver bump from the
  conventional-commit history — `fix`→patch, `feat`→minor, `BREAKING CHANGE`/`!`→major — and (2) drafts
  a per-version migration guide (`docs/migrations/vX.Y.Z.md`) when the release warrants one: a breaking
  change, a deprecation, or a change consumers should follow (a new preferred input, a moved path, a new
  recommended pattern). Use it whenever you are about to cut a release, are asked "is this patch / minor
  / major?", "what changed since the last release?", or "does this need a migration guide?", or when a
  change adds/deprecates something consumers depend on. It is ADVISORY: it proposes the version and
  writes the guide; the human cuts the actual release (the UI release workflow). Pairs with
  `manage-versions` (versioning mechanics + staged rollouts), `git-conventions` (the commit→bump
  mapping), and the release workflow (`release-core`). It does not publish, tag, or push.
---

# Prepare a release — size it, and write its migration guide

A release has two questions this skill answers before anyone clicks "Run":

1. **How big is it?** — patch, minor, or major, derived from the conventional-commit history, not a guess.
2. **What must a consumer do to adopt it?** — captured as a **migration guide** that ships with the
   code and lives forever, so the answer is never lost in a PR description or a Slack thread.

You are advisory. You read the diff, propose the version, and draft the guide; the human reviews and
cuts the release from the Actions tab. You never tag, push, or publish — releases are human-only (see
`manage-versions`).

> This skill is the manual stand-in for a future agent that runs it automatically before every
> release. Until then, invoke it deliberately when a release is near.

---

## 1. Read the unpublished diff

Find the last release and everything since, on the branch you'll release from (the default branch):

```bash
git fetch --tags origin
last=$(git tag --list 'v*' --sort=-v:refname | head -1)   # or <subdir>/v* for a sub-dir module
git log --no-merges --pretty='%s' "$last..origin/master"
```

Read the **subjects** (the conventional-commit type/scope/`!`) and, for anything that might break or
deprecate, the **body/footers** (`BREAKING CHANGE:`). A squash-merge repo has one commit per PR, so the
subjects are the PR titles — usually enough; open the PR/diff when a subject is ambiguous about consumer
impact.

If the repo has **no prior tag**, this is the first release (`v0.1.0` or `v1.0.0` per the team's call) —
there's nothing to migrate _from_, so a guide is rarely needed.

---

## 2. Propose the bump

Apply the `git-conventions` / Conventional Commits mapping; the **highest** wins across the range:

| Commit signal                                  | Bump      |
| ---------------------------------------------- | --------- |
| `BREAKING CHANGE:` footer, or `!` after type   | **major** |
| any `feat:`                                    | **minor** |
| only `fix:` / `perf:` / `refactor:` / `chore:` | **patch** |

Pre-1.0 caveat: while a repo is `0.y.z`, a breaking change is conventionally a **minor** bump, not a
major — confirm the repo's stance with the operator if it matters. Docs/CI-only ranges (`docs:`, `ci:`,
`chore:`) are a **patch** (still a release if you want the notes), or skip the release entirely.

State the proposed version plainly — e.g. _"`v1.0.3` → `v1.1.0` (minor: three `feat:`, no breaking
footer)"_ — and name the one or two commits that drove it. That version is also the **release-type the
human picks in the release workflow** and the **filename of the guide**.

---

## 3. Decide whether a migration guide is needed

A guide documents **consumer action**, not a changelog. Use this gate:

| Release shape                                                                              | Guide?          |
| ------------------------------------------------------------------------------------------ | --------------- |
| Major / any `BREAKING CHANGE` / `!`                                                        | **Required**    |
| Minor that **deprecates** a path, adds a **preferred** alternative, or recommends a change | **Recommended** |
| Minor that is purely additive (new optional thing, no consumer change)                     | No (notes only) |
| Patch (bug/perf/internal)                                                                  | No              |

The middle row is the subtle one: a backward-compatible release can still _ask_ consumers to move
(a new recommended input, a renamed-but-aliased field, a new pattern). That's a **recommended** guide —
the change isn't forced, but the migration is real and worth writing down once. When in doubt for a
minor, ask: _"is there anything a consumer should change to fully benefit, even though nothing breaks?"_
If yes, write the guide.

---

## 4. Write the guide

**One immutable file per version**, never a single growing `UPGRADING.md`. This is the convention
every mature migration system uses (Flyway `V1__…`, Rails / GitLab timestamped migrations) for the same
reason: a single appended file is a merge-conflict magnet across parallel release branches and blurs
which change maps to which release. Per-version files are additive (no conflicts) and map 1:1 to a tag.

- **Path:** `docs/migrations/vX.Y.Z.md`, named by the version that introduces the change. (A sub-dir Go
  module — e.g. `cli/` — keys the file by its version too; a repo releases one module, so the version
  is unambiguous.)
- **Immutable once shipped.** Never edit a released guide; a correction goes in the _next_ version's
  guide, so history stays truthful.
- **Indexed.** Keep `docs/migrations/README.md` as a table, newest-first, linking each guide.
- **Linked from the release.** The release notes for `vX.Y.Z` should point at its guide (add the link
  to the generated notes / the GitHub Release body).
- **Committed _with_ the change**, before the release is cut — so the released tag already contains its
  own guide.

### Template

```markdown
# Upgrading to vX.Y.Z

One-line summary of what changed and why it matters to a consumer.

## Do I need to act?

**Required / Recommended / No** — and the one-sentence reason. If "No", say why the release is safe to
take as-is and stop here.

## <Change 1 — imperative title, e.g. "Switch bot inputs to client_id">

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

Lead with **"Do I need to act?"** — most readers want exactly that, and a clear "No" for a safe minor is
a feature, not a cop-out. Keep each change section to _what changed → why → before/after → caveats_.

---

## 5. Present — advisory only

Hand the operator: the **proposed version**, the **commits that drove it**, whether a **guide** was
written (and where), and the **release-type to pick** in the workflow. Then stop. The human cuts the
release (Actions ▸ `release` ▸ pick the bump); `release-core` stamps versions, tags, and publishes.
You do not run it.

If the change ships in stages or spans repos, that ordering is a `manage-versions` concern — note it
and defer.

---

## Principles

- **The guide is consumer-facing, not a changelog.** Release notes list _what changed_; the guide says
  _what to do about it_. If a change needs no consumer action, it belongs in the notes, not the guide.
- **One file per version, immutable.** Additive, conflict-free, 1:1 with a tag. Never rewrite a shipped
  guide.
- **Derive, don't guess, the version.** The bump comes from the commit history; if the history is wrong
  (a `feat` mislabeled `fix`), fix the discipline (`git-conventions`), don't override the math silently.
- **Advisory, never the trigger.** You propose; the human releases. No tagging, pushing, or publishing.

---

## How it composes

```
prepare-release ─ (reads the unpublished diff) ─> proposed bump + docs/migrations/vX.Y.Z.md
      │
      ├─ git-conventions  — the commit→bump mapping it applies
      ├─ manage-versions  — versioning mechanics, staged/cross-repo rollouts the guide may reference
      └─ release workflow — the human cuts the release with the proposed bump; the guide ships in the tag
```

A migration guide written here is the **instruction**; rolling it out across consumer repos is execution
(`implement-feature` / `manage-versions`), often tracked as its own follow-up issue.

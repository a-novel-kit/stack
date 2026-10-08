# Planning issue structure and staging

Read this reference when routed here by [plan-feature](../SKILL.md). Its rules apply to the selected work.

Contents:

- [The planning issue](#the-planning-issue)
- [Where the issue lives](#where-the-issue-lives)
- [Anatomy](#anatomy)
- [Breakdown & staging — sub-issues and dependencies](#breakdown--staging--sub-issues-and-dependencies)
- [Keep the body clean; discuss in comments](#keep-the-body-clean-discuss-in-comments)
- [Body template](#body-template)
- [Completion handling — let GitHub track state](#completion-handling--let-github-track-state)
- [Side quests — file them, don't absorb them](#side-quests--file-them-dont-absorb-them)

## The planning issue

### Where the issue lives

| Situation                                                                      | Where the planning issue goes                                                                                                                                                                                                                                                             |
| ------------------------------------------------------------------------------ | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **Single repo**                                                                | One issue in that repo.                                                                                                                                                                                                                                                                   |
| **Multiple repos within one org**                                              | An **Epic** in that org's `.github` repo, with **Task sub-issues** in each member repo. Sub-issue progress rolls up to the Epic.                                                                                                                                                          |
| **Cross-org** (an `a-novel` repo needs an `a-novel-kit` change, or vice versa) | Epic in the **outcome-owning** org's `.github`. The dependency in the _other_ org is a **referenced + `blocked-by`** link, **not** a cross-org sub-issue — cross-org sub-issues link but their progress rollup undercounts. `manage-versions` owns the actual merge order.                |
| **Broad, multi-release / multi-Epic effort**                                   | An **Initiative** — in the repo itself when single-repo, or the outcome-owning org's `.github` when cross-repo — with **one Epic per release/capability** under it (Task sub-issues under each Epic), grouped by a goal-named milestone (per repo — see **Milestone naming & grouping**). |

The two `.github` repos (`a-novel/.github`, `a-novel-kit/.github`) are the natural home for
cross-repo Epics and Initiatives; per-repo work lives in the repo it touches.

### Anatomy

- **Type** (org-level issue type — the "kind" axis, shared across all repos in the org). GitHub's
  model is **Initiative → Epic → Feature → Task** (+ **Bug**):

  | Type           | Use for                                                                                                                                                                                                                                                                                                                                                  |
  | -------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
  | **Initiative** | The durable umbrella for a **broad, multi-stage or multi-release effort spanning several Epics**. Its active state is the **Tracking** status. Reach for it — not one oversized Epic — whenever the work is more than a single shippable capability (e.g. a non-breaking hardening pass _then_ a breaking cleanup).                                      |
  | **Epic**       | **One shippable capability / release** — the parent of Task sub-issues, and the unit that typically maps to a single version bump. An Epic is about a **goal, never a version** (don't name it `v1.2.0`; the version is its _target_). For a multi-release effort it sits under an **Initiative**; for standalone cross-repo work it lives in `.github`. |
  | **Feature**    | One shippable capability, usually one repo (possibly a few branches).                                                                                                                                                                                                                                                                                    |
  | **Task**       | A branch-sized unit of work — ≈ one PR. The sub-issues of an Epic or Feature.                                                                                                                                                                                                                                                                            |
  | **Bug**        | A defect.                                                                                                                                                                                                                                                                                                                                                |

  Set it with `gh issue create --type <Initiative\|Epic\|Feature\|Task\|Bug>`. The type carries the
  kind, so there is **no `bug`/`enhancement` label** any more. An effort spanning several releases is
  an **Initiative** with **one Epic per release/capability** underneath, all sharing **one**
  goal-named milestone (see the Milestone rule below), never a milestone per stage.

- **Body** = the plan, in the structure below. Markdown (same as PR descriptions). Iterate it with
  `gh issue edit <n> --body-file <file>`.

- **Labels** — orthogonal axes only. Keep using `documentation`, `dependencies`/`renovate`,
  `go`/`javascript`, and the community signals `good first issue` / `help wanted` where they apply.
  There is **no `triage` label** — assessment state is the **Triage status** (below), not a label.
  **Never** label kind (that's Type), priority/effort (Project fields), or blocked state (native
  dependencies).

- **Assignee** — assign every Epic and Task to its **creator** on creation (`--assignee "@me"`, the
  operator whose `gh` token authors it — not the bot). They may reassign later; a default owner keeps
  the board triageable as more contributors arrive, and no issue sits ownerless.

- **Project board** — add the issue to the org's **"Tasks"** board (`a-novel` project #7,
  `a-novel-kit` project #1) and set its fields:

  | Field           | Values                                                                                            | Meaning                                                                                                                                                                                                                                                      |
  | --------------- | ------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
  | **Status**      | Backlog · Triage · Tracking · Ready · In progress · In review · Done · Awaiting release · Applied | Workflow state. **Triage** = un-assessed incoming; **Backlog** = not-yet-ready draft; **Ready** = pickup-able; **Tracking** = an Initiative's active state; **Awaiting release** = merged, not yet released; **Applied** = terminal for a meta / no-PR task. |
  | **Priority**    | P0 · P1 · P2 · P3 · P4                                                                            | P0 = drop-everything; P4 = nice-to-have.                                                                                                                                                                                                                     |
  | **Size**        | XS · S · M · L · XL                                                                               | The **effort / weight** estimate. Every actionable ticket gets one.                                                                                                                                                                                          |
  | **Stage**       | Stage 1 … Stage N · Unscheduled                                                                   | **Absolute** placement within a multi-stage milestone / initiative. "What's next" = the lowest-numbered stage not yet Done.                                                                                                                                  |
  | **Target date** | _date_                                                                                            | The **due date**. Set it once the issue goes **active** (see below).                                                                                                                                                                                         |
  | **Milestone**   | _per-repo_                                                                                        | Optional goal-scoped grouping toward a deliverable. Cannot span repos. Give it a **due date** (`due_on`). Name and group it per **Milestone naming & grouping** below.                                                                                       |

- **When to set each field — weight, priority, due dates.** Set **Size** (weight) and **Priority** at
  **creation when the scope is clear** — you usually know a planned Task's rough size and urgency —
  otherwise **during the triage pass** (`triage-issues`). Leave the **Target date** (due date) empty
  until the issue becomes **active** — it has an open PR linked against it — then **agree a due date
  with the operator**; an active issue without a due date is a triage smell. Give every **milestone**
  a due date too — PATCH the specific milestone, not the collection:
  `gh api repos/<o>/<r>/milestones/<n> -X PATCH -f due_on=<RFC3339>` (or pass `-f due_on=…` to
  `... -f title=…` when first creating it). Both dates exist to make triage decisions.

- **Set every field at creation — `--project` is not enough (recurring footgun).** Boarding an issue
  (`--project "Tasks"`) and setting its **milestone** and **board fields** are _independent_ actions:
  boarding sets neither Milestone, Priority, Size, nor Stage — those default to empty. Treat each
  `gh issue create` as a two-part act: (1) create it **with** `--milestone` when it belongs to one (a
  milestone can only be set by `--milestone` / `gh issue edit --milestone`, never by `--project`),
  then (2) set **Priority / Size / Stage** in the same breath via `gh project item-edit`. Never leave
  an item half-fielded. **Verify after any batch create** with a one-line board scan — _a Stage-tagged
  item with no Milestone is the tell_ that `--project` was passed but `--milestone` was forgotten.
  Confirm every field write landed rather than assuming it did.

- **Milestone naming & grouping.** A milestone is a **goal-scoped grouping, not a version tag**. Name
  it for the repo + what it delivers (`JWT: security hardening & API modernization`), **never a bare
  version** — the org boards render every repo's milestones in one list, so `v1.2.0` is meaningless
  out of context. Put the **release version on the Epic** (its title/body/target), and group
  **several Epics under one** goal-named milestone rather than one milestone per release stage.
  Milestones are **per-repo and cannot span repos**; to group a cross-repo deliverable, give each repo
  a milestone with the **identical (goal) name** so the board's milestone view groups them, and let
  the **Initiative → Epic** graph be the real cross-repo glue.

### Breakdown & staging — sub-issues and dependencies

This replaces both the old `- [ ]` work-breakdown checkboxes and the stepped `plan-X-1.md` files.

- **Sub-issues = the hierarchy.** Break an Epic (or a large Feature) into Task sub-issues, each
  ≈ one branch/PR. Create them with `gh issue create --parent <epic-url-or-#>`; progress rolls up to
  the parent automatically (within one org — see the cross-org caveat above). Up to 100 sub-issues
  per parent, 8 levels deep.

- **Dependencies = the ordering.** Express "stage n+1 is conditioned by stage n" with the **native
  blocked-by relationship**, not a label: `gh issue create --blocked-by <n>` or
  `gh issue edit <m> --add-blocked-by <n>`. Blocked issues show an indicator on the board. Up to 50
  per relationship type.

- **Draft future stages.** For a staged plan, open the later stages **ahead of time** as Task
  sub-issues in Status `Backlog`, each `blocked-by` its predecessor, so the whole shape is visible and
  pick-up-able later. Drafts are cheap — **if the plan changes, delete the draft**
  (`gh issue delete <n> --yes`; deletion needs an owner/admin token). Never delete an issue that has
  history worth keeping — close it instead.

### Keep the body clean; discuss in comments

The issue **body** holds the current agreed plan only. Everything conversational — open questions,
your recommendations, the human's answers, rejected alternatives mid-debate — goes in **comments**,
so the body stays readable and the human can reply inline to a specific point.

Post your comments through the **bot**, never bare `gh` (the bot dispatcher comments on issues as it
does on PRs — issues and PRs share one number sequence):

```bash
a-novel core bot-comment <org> <repo> <issue-number> --body "$(cat <<'EOF'
**Open question — token TTL.** The spec allows 15m or 60m. I recommend **15m** because <reason>.
Your call before I freeze it in the body.
EOF
)"
```

When a question is settled, fold the decision into the body (`gh issue edit`). Responding to the
human's replies under an issue is the `resolve-pr-feedback` loop applied to issues — load that skill
for the classify/reply/close mechanics.

> **Identity.** You **create and edit** issues with the operator token (so the issue is authored by
> the human you're working with), exactly as you open PRs. You **comment** through the bot, so the
> agent's voice is distinct from the human's in the thread. The bot can only comment — it cannot
> author or edit an issue.

### Body template

Adapt to fit, but cover these. (This is also the shape of the `task.yml` planning template in each
org's `.github` repo — not this repo; open questions live in **comments**, never here.)

```markdown
## Goal

What outcome, and why it matters — in plain language a non-technical stakeholder can follow.

## Scope

**In:** what this delivers.
**Out:** what is deliberately excluded.

## Context & findings

Current state, the relevant code (with file paths), internet research (with sources), constraints.

## Approach

The design. **Freeze the domain vocabulary here** (a short glossary of the core terms) and use it
consistently from here on. Alternatives — including any prior art — considered and why rejected or
surpassed. Notes against the secure / efficient / maintainable / UX lenses.

## Client/server boundary (when applicable)

Server authority and atomic capabilities, client-owned workflow and ergonomics, stable envelope
versus evolvable payload, failure/recovery, and the measured network budget. Follow
`plan-client-server-boundary` for the full decision table.

## Cross-repo & rollout

Repos and repo kinds touched, dependency order, and whether it ships in stages
(backward-compatible first, cleanup later). Delegates mechanics to manage-versions.

## Work breakdown

The Task sub-issues, in order — each ≈ one branch/PR. Tracked natively as sub-issues + blocked-by
links (not checkboxes), but list them here for the reader:

1. `repo-a` — <one line> (#<task>)
2. `repo-b` — <one line>, blocked by #1

## Risks & security

Failure modes, security considerations, things that could go wrong.

## Out of scope / future

Deferred ideas worth remembering.
```

### Completion handling — let GitHub track state

The old "keep the plan file from rotting" discipline is now mostly automatic:

- Each Task closes when its PR merges (`Closes #<task>`); the Epic's **sub-issue progress** advances
  on its own. When the PR lives in a different repo than the planning issue (a Feature/Task filed in a
  `.github` repo, closed by a service PR), the close ref must be the **full cross-repo form**
  `Closes a-novel-kit/.github#<n>` or `derive-status` never moves it and it freezes on the board — see
  `open-pull-request`.
- Move the board **Status** as work flows (Ready → In progress → In review → Done).
- Keep the Epic open until every stage has landed, then close it. A closed Epic with its closed
  sub-issues _is_ the durable record — no summary rewrite needed.

### Side quests — file them, don't absorb them

A good analysis surfaces more work than the plan should carry. File each find as an **orphan
issue**: no parent, no milestone, boarded on the org "Tasks" project with **Status = Triage**, the
un-assessed incoming queue `triage-issues` drains (Backlog is for planned-but-not-ready work).
Folding the find into the current Epic bloats its scope and delays it; leaving it in chat loses it at
the next context reset.

File it in the repo it would land in, typed normally, and verify it against the code first — an
inferred defect that turns out to be already fixed costs whoever picks it up their whole slot. The
body carries what was spotted, the evidence, and why it matters, written to survive without the
conversation that produced it.

When a pass surfaces a **batch** meant to be picked up as one unit, give them a shared goal-named
milestone — still no parent, still Triage. A milestone cannot span repos, so use the identical name
in each repo. Without one, a parallel session has no single handle to pick the batch up by.

Batch the filing at the end of the pass rather than interrupting the analysis, and name the side
quests in the plan's "Out of scope / future" section so a reader knows they were considered.

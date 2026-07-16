---
name: triage-issues
description: >
  The manual triage / grooming pass over the open GitHub issue set across the a-novel / a-novel-kit
  orgs, run from Claude Code during a planning session. Use it whenever the user asks to "run triage",
  "do a triage pass", "groom the backlog", "prioritise the issues", "prep the planning meeting", or to
  review and refine open issues and drafts. It surveys both org "Tasks" boards, drains the `Triage`
  status queue, assigns and refines Priority and Size (weight), sets due dates on active issues and on
  milestones, advances Status, and firms up Backlog drafts that are about to enter production. Trigger
  it MANUALLY — it is not a scheduled job. Pairs with plan-feature (which creates the issues and owns
  their bodies), resolve-pr-feedback (issue / PR discussion), and manage-versions (cross-repo staging).
---

# Triage & groom the issue set

Planning creates issues; **triage keeps them honest**. Over time, priorities drift, scopes change,
drafts pile up, active work loses its due date, and the `Triage` status fills with un-assessed reports.
This skill is the recurring pass that fixes all of that — run **manually**, as a planning meeting with
the operator, not on a timer. A weekly cadence is recommended, but the human pulls the trigger.

The companion to `plan-feature`: that skill sets a ticket's fields at **creation**; this one keeps
them honest **over time**.

---

## Posture

- **Scope to the work in hand.** "Clean the board" means the milestone — or the single task — this
  session is already about, not every open item. Survey what that work touches, fix what drifted
  there, and leave the rest alone: an unrelated ticket belongs to someone else's pass, and reporting
  it is noise dressed as diligence. Sweep the whole board only when the operator asks for that in so
  many words.
- **This is a planning conversation, not a batch job.** Propose field values; the operator confirms —
  especially **due dates**, which are commitments. Use the `resolve-pr-feedback` issue-comment loop to
  ask and record decisions where useful.
- **Lead with priority.** Work the set in Priority order (P0 → P4). The output of a pass is a clear
  "what's next": the P0/P1 items that aren't already moving.
- **Keep the board honest.** The invariants a pass enforces: every actionable ticket has a **Type**, a
  **Priority**, and a **Size**; **every item that belongs to a milestone actually carries it** — boarding
  an item (`--project`) never sets its Milestone, so scan for a **Stage-tagged item with an empty
  Milestone** (the tell that `--milestone` was forgotten at creation) and add it; no **active** issue
  lacks a **due date**; no items lingering in `Triage`; no rotting drafts.

---

## The pass

### 1. Survey both boards

```bash
gh project item-list 7 --owner a-novel --format json --limit 200
gh project item-list 1 --owner a-novel-kit --format json --limit 200
```

Both orgs, because one milestone's work is usually split across them. Then **filter to the work in
hand** (see Posture). A milestone is per-repo, so its copies all share a title: selecting on that
gathers every repo's share of it in one pass.

```bash
… | jq '.items[] | select(.milestone.title == "<the goal>")'
```

Drop the filter only for a full sweep the operator has asked for.

For each item in scope read: Type, Priority, Size, Status, Target date, Milestone, linked PRs,
blocked-by, and sub-issue progress. The **Triage queue** is every board item sitting in the `Triage`
**status** — an un-assessed incoming issue lands there; filter the item-list above by
`Status == Triage`.

### 2. Drain the triage queue (un-assessed → assessed)

For each item in the `Triage` status, with the operator:

- Confirm or assign its **Type** (Epic / Feature / Task / Bug).
- Assign **Priority** and **Size** (weight).
- Add it to the org board if it isn't already (`gh project item-add`).
- Assess it and move its **Status** out of `Triage` — `Backlog` for a not-yet-ready draft, `Ready`
  when it can be picked up (`gh project item-edit`).
- An empty `Triage` status is the goal of a **full sweep** — no item should linger un-assessed (the
  cli/cli "First Responder" model). In a scoped pass, drain only what belongs to the work in hand; an
  unrelated arrival is worth one line as a signal, not a detour.
- **Escalation tickets** (label `escalation`) are a distinct intake — the governance automation files
  them in `Triage` when something needs a human (a stuck hotfix reconcile, a stale required check, a
  failed emergency path; see `coordinate-landing`). Don't size them as planning work: act on the
  underlying condition, then let the `escalate` action resolve the ticket (Status → `Applied`) as it
  self-heals, or close it once handled. A pile of open `escalation` tickets is an ops signal.

### 3. Prioritise

Order the actionable set by **Priority** and surface the **P0/P1 items that are not yet In progress** —
that is the planning focus. Re-balance priorities with the operator where reality has drifted (a P2
that's now blocking a release becomes P1; a P1 nobody will touch becomes P3).

### 4. Weights

Every actionable ticket gets a **Size** (XS–XL). Assign where missing; **re-estimate** where the
scope changed since creation. A ticket whose size keeps growing is a signal to split it into
sub-issues (hand back to `plan-feature`).

### 5. Due dates — with the operator

- **Active issues** — those with an **open linked PR** — **must** have a **Target date**. Agree it
  with the operator. _An active issue with no due date is the single thing this pass exists to catch._
- **Milestones** get a **due date** too, set/adjusted to the release plan:

  ```bash
  gh api repos/<owner>/<repo>/milestones/<number> -X PATCH -f due_on=2026-07-15T00:00:00Z
  ```

- Treat both as **soft commitments** that drive the _next_ pass — a slipped date is information, not a
  failure. The point is that every in-flight thing has a date to slip _from_.

### 6. Refine drafts nearing production

The Backlog drafts `plan-feature` stages ahead (future stages, `blocked-by` their predecessors) need
grooming as they approach activation:

- For a draft about to go active: **firm up the body** (is the approach still right?), set its
  **Size/Priority**, confirm its **blocked-by** is now satisfied, and move **Backlog → Ready**.
- **Delete drafts the plan has overtaken** (`gh issue delete <n> --yes`) — a not-yet-started draft is
  cheap to discard, and a stale one is worse than none. Don't let drafts rot.

### 7. Status is single-writer

The board's bot derives Status from what happened to a Pull Request, so `In progress`, `In review`, and
`Awaiting release` are not yours to set: a manual edit will not stick. Your part is the `Triage → Ready`
move from the drain above, plus a **meta task**'s final `Applied`, which has no Pull Request to read
from. Everything else follows the work, so don't fight it.

### 8. Keep the Stage field accurate (absolute scheme)

The `Stage` board field is **absolute** — `Stage 1 … Stage N` (six on the board today), plus
`Unscheduled` for work not yet placed in a stage. A staged milestone's epics and their child tasks each carry their stage's number,
set once at creation (per the `plan-feature` field habit) and **never shifted**: an item at `Stage 3`
stays `Stage 3` for good. "What's next" is a derivation, not a label — the lowest-numbered stage that
is not yet done. A finished stage keeps its number; its epic follows the normal Done → archive
lifecycle rather than being relabelled.

So this pass has nothing to shift. Check only that new items carry the right stage number — a human
call about which stage the work belongs to — and that an epic and its tasks agree on it. For now no
automation reads the field: you set it by hand at creation, and you read "what's next" off the numbers
yourself.

### 9. Report

Close the pass with a tight summary for the operator:

- The prioritised **P0/P1 "what's next"** list.
- What was **triaged** (queue drained), **re-prioritised**, and **re-sized**.
- **Due dates** set (issues + milestones) and any the operator still needs to commit.
- Drafts **promoted** (Ready) or **deleted**.
- Anything still **needing the operator's decision**.

---

## gh quick reference

| Action                        | Command                                                                                             |
| ----------------------------- | --------------------------------------------------------------------------------------------------- |
| List board items              | `gh project item-list <7\|1> --owner <org> --format json --limit 200`                               |
| Find the triage queue         | filter `gh project item-list` output to items with `Status == Triage`                               |
| Set Priority / Size / Status  | `gh project item-edit --id <item> --project-id <proj> --field-id <f> --single-select-option-id <o>` |
| Set a due date (Target date)  | `gh project item-edit --id <item> --project-id <proj> --field-id <date-field> --date YYYY-MM-DD`    |
| Set a milestone due date      | `gh api repos/<o>/<r>/milestones/<n> -X PATCH -f due_on=<RFC3339>`                                  |
| Leave Triage (assess an item) | move Status `Triage → Backlog`/`Ready` via `gh project item-edit`                                   |
| Promote a draft               | move Status `Backlog → Ready` via `gh project item-edit`                                            |
| Delete an overtaken draft     | `gh issue delete <n> --repo <o>/<r> --yes`                                                          |

Field / option / item IDs come from `gh project field-list <num> --owner <org>` and
`gh project item-list`. Scopes: `repo` + `project` (see `plan-feature` → _Token scopes_); on an
authorization failure, stop and ask the operator to grant the missing scope.

---

## How this composes

```
plan-feature (creates issues, sets fields at creation)
      │
      ▼
triage-issues (recurring manual pass: prioritise · weigh · due-date · refine drafts · drain triage)
      │
      ├─ hands a near-ready draft back to plan-feature when it needs real (re)design
      ├─ uses resolve-pr-feedback's comment loop to ask/record operator decisions on an issue
      └─ feeds the P0/P1 "what's next" into implement-feature / manage-versions
```

---

## Principles

- **Scope before sweep.** A pass covers the work the session is about. The whole board only on request.
- **Lead with priority.** A pass is judged by whether "what's next" is unambiguous afterwards.
- **Honest invariants.** Every actionable ticket: Type + Priority + Size. Active ⇒ a due date. No
  items lingering in `Triage`.
- **Dates are for triage, not theatre.** They exist to make the next decision; a slip is information.
- **Drafts are living.** Refine the ones about to go active; delete the ones the plan has overtaken.
- **Propose, the operator commits.** Especially dates — triage is a conversation, not an edict.

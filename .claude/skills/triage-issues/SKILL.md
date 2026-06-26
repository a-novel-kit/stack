---
name: triage-issues
description: >
  The manual triage / grooming pass over the open GitHub issue set across the a-novel / a-novel-kit
  orgs, run from Claude Code during a planning session. Use it whenever the user asks to "run triage",
  "do a triage pass", "groom the backlog", "prioritise the issues", "prep the planning meeting", or to
  review and refine open issues and drafts. It surveys both org "Tasks" boards, drains the `triage`
  label queue, assigns and refines Priority and Size (weight), sets due dates on active issues and on
  milestones, advances Status, and firms up Backlog drafts that are about to enter production. Trigger
  it MANUALLY — it is not a scheduled job. Pairs with plan-feature (which creates the issues and owns
  their bodies), resolve-pr-feedback (issue / PR discussion), and manage-versions (cross-repo staging).
---

# Triage & groom the issue set

Planning creates issues; **triage keeps them honest**. Over time, priorities drift, scopes change,
drafts pile up, active work loses its due date, and the `triage` queue fills with un-assessed reports.
This skill is the recurring pass that fixes all of that — run **manually**, as a planning meeting with
the operator, not on a timer. A weekly cadence is recommended, but the human pulls the trigger.

The companion to `plan-feature`: that skill sets a ticket's fields at **creation**; this one keeps
them honest **over time**.

---

## Posture

- **This is a planning conversation, not a batch job.** Propose field values; the operator confirms —
  especially **due dates**, which are commitments. Use the `resolve-pr-feedback` issue-comment loop to
  ask and record decisions where useful.
- **Lead with priority.** Work the set in Priority order (P0 → P4). The output of a pass is a clear
  "what's next": the P0/P1 items that aren't already moving.
- **Keep the board honest.** The invariants a pass enforces: every actionable ticket has a **Type**, a
  **Priority**, and a **Size**; no **active** issue lacks a **due date**; no stale `triage` labels;
  no rotting drafts.

---

## The pass

### 1. Survey both boards

```bash
gh project item-list 7 --owner a-novel --format json --limit 200
gh project item-list 1 --owner a-novel-kit --format json --limit 200
```

For each item read: Type, Priority, Size, Status, Target date, Milestone, linked PRs, blocked-by,
and sub-issue progress. The **`triage` queue** is every open issue still carrying the `triage`
label (`gh search issues --owner a-novel --label triage --state open`, and likewise for
`a-novel-kit`) — those are un-assessed.

### 2. Drain the triage queue (un-assessed → assessed)

For each `triage`-labelled issue, with the operator:

- Confirm or assign its **Type** (Epic / Feature / Task / Bug).
- Assign **Priority** and **Size** (weight).
- Add it to the org board if it isn't already (`gh project item-add`).
- Set an initial **Status** (`Backlog` if not ready, `Ready` if it can be picked up).
- **Drop the `triage` label** once assessed (`gh issue edit <n> --remove-label triage`). The empty
  triage queue is the goal of every pass (the cli/cli "First Responder" model).

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

### 7. Advance Status

Move each item's **Status** to match reality (`Ready` when unblocked, `In progress` when a PR opens,
`In review`, `Done`). Once the board's built-in automations are enabled (see `plan-feature` →
_Operating the board_), `Done` is handled for you on merge/close — don't fight the automation.

### 8. Report

Close the pass with a tight summary for the operator:

- The prioritised **P0/P1 "what's next"** list.
- What was **triaged** (queue drained), **re-prioritised**, and **re-sized**.
- **Due dates** set (issues + milestones) and any the operator still needs to commit.
- Drafts **promoted** (Ready) or **deleted**.
- Anything still **needing the operator's decision**.

---

## gh quick reference

| Action                       | Command                                                                                             |
| ---------------------------- | --------------------------------------------------------------------------------------------------- |
| List board items             | `gh project item-list <7\|1> --owner <org> --format json`                                           |
| Find the triage queue        | `gh search issues --owner <org> --label triage --state open`                                        |
| Set Priority / Size / Status | `gh project item-edit --id <item> --project-id <proj> --field-id <f> --single-select-option-id <o>` |
| Set a due date (Target date) | `gh project item-edit --id <item> --project-id <proj> --field-id <date-field> --date YYYY-MM-DD`    |
| Set a milestone due date     | `gh api repos/<o>/<r>/milestones/<n> -X PATCH -f due_on=<RFC3339>`                                  |
| Drop the triage label        | `gh issue edit <n> --repo <o>/<r> --remove-label triage`                                            |
| Promote a draft              | move Status `Backlog → Ready` via `gh project item-edit`                                            |
| Delete an overtaken draft    | `gh issue delete <n> --repo <o>/<r> --yes`                                                          |

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

- **Lead with priority.** A pass is judged by whether "what's next" is unambiguous afterwards.
- **Honest invariants.** Every actionable ticket: Type + Priority + Size. Active ⇒ a due date. No
  stale `triage` labels.
- **Dates are for triage, not theatre.** They exist to make the next decision; a slip is information.
- **Drafts are living.** Refine the ones about to go active; delete the ones the plan has overtaken.
- **Propose, the operator commits.** Especially dates — triage is a conversation, not an edict.

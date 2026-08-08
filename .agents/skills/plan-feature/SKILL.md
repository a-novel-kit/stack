---
name: plan-feature
description: >
  The planning and technical-design gate before any non-trivial implementation in the a-novel /
  a-novel-kit workspace. Use it when a change spans multiple repos, touches an architecture, data
  model, or client/server boundary, introduces a service, platform or library, weighs build-vs-buy,
  or is ambiguous about what to build. It captures the agreed design as a GitHub planning **issue**
  (Initiative / Epic / Task sub-issues), then hands off to repo-kind implementation skills. Skip
  trivial single-repo edits.
---

# Plan & design before you build

You are the tech lead on this change, not an order-taker. Turn the request — which may be vague,
partial, or even wrong — into a technical plan that is **exhaustive, secure by design, efficient, and
maintainable**, and get the human to agree to it before a line of production code is written. A plan
built on a misunderstanding wastes far more time than the planning itself costs.

The output is an agreed **planning issue** (below). This skill decides _what_ and _why_. Backend
service execution (`implement-feature`), frontend authoring (`write-frontend` and companions), and
cross-repo versioning (`manage-versions`) decide _how_; delegate those mechanics to them.

> **Why issues, not plan files.** Plans used to live in gitignored `plan-*.md` files at the workspace
> root. A gitignored file has **no backup** (one was lost, which is why this workflow exists), and a
> local file can't carry **type, labels, priority/effort, sub-issue structure, dependencies, or PR
> links**. A GitHub issue survives context resets, hosts the human's replies, and links directly to
> the PRs that implement it. The plan is a **typed, linked, trackable issue graph**.

---

## When to use this skill — and when to skip it

Invoke it whenever the implementation steps are not already 100% clear, or the change is large
enough that getting it wrong is expensive:

| Signal                                                        | Plan first?                             |
| ------------------------------------------------------------- | --------------------------------------- |
| Spans more than one repo, or needs an unreleased dependency   | **Yes**                                 |
| Introduces a new service / platform / library, or a new layer | **Yes**                                 |
| Changes an architecture, data model, or public contract       | **Yes**                                 |
| Involves a build-vs-buy / new-dependency decision             | **Yes**                                 |
| Migrates or re-architects existing code                       | **Yes**                                 |
| Ambiguous, broad ("improve X"), or you'd have open questions  | **Yes**                                 |
| Small, unambiguous, single-repo edit with obvious steps       | No — go straight to `implement-feature` |
| A typo, a one-line fix, a rename you can see end-to-end       | No                                      |

When in doubt, plan: a short plan costs minutes, building the wrong thing costs the whole change plus
the rework. (The roadmap direction is that **every** PR — even a one-liner — eventually traces to an
issue; for now, the table above is the gate for the full planning ritual.)

---

## Your posture

- **Propose, don't just ask.** Every open question you raise carries your recommendation. You are
  paid for judgment, not for a menu.
- **Challenge the request — technically and on UX.** Humans miss context or ask for the second-best
  thing. If a different direction is better — more robust _or_ more ergonomic for the people who'll
  use it — say so and explain why. Fill what is missing; push back on a mistake before it becomes
  code.
- **Stand your ground, then yield gracefully.** Defend your reasoning, but the human owns the final
  call: once they've decided against you, comply cleanly and capture the decision in the issue so it
  isn't relitigated. You can be wrong too.
- **Speak to the reader.** Issues are read by busy people, sometimes non-technical, and by a
  technical reviewer before execution — serve both: concise, concrete, jargon defined, decision
  first. An issue body is a prose surface, so `document-code`'s **Prose economy** section governs
  it — most of all "write the choice, not the rejected alternative". Record what we decided; the
  alternatives you weighed belong in the discussion comments.

---

## The phases

### 1. Frame the problem

Restate the goal in one or two plain sentences — _what_ outcome, and _why_ it matters — and the
explicit scope boundaries (what is in, what is deliberately out). If the request is ambiguous,
resolve that **now**, before research: one focused clarifying question beats a plan built on a guess.

### 2. Research — the three axes

Never plan from assumptions. Cover all three axes below; skip one only when it genuinely doesn't
apply to the change, and say why. Cite what you relied on so the human can verify:

- **a. Community standards & prior art — how the world already solves this.** For any non-trivial
  problem, search the web and read how it is handled _outside_ our walls: official docs and specs
  first, then how **major public organizations** solve the same thing (their open-source repos,
  engineering blogs, RFCs, conference talks), reputable standards bodies, and well-regarded
  write-ups, informational posts included. Prefer recent, primary sources over hearsay. **Default to
  the established community standard over inventing our own:** a widely-adopted pattern is
  battle-tested, familiar to contributors, and cheaper to maintain. Deviate only with a thorough
  justification, and derive the deviation _from_ a proven standard (the way we run a few **macro**
  services instead of micro/nano — a deliberate, defended departure, not a bespoke invention).
- **b. Our own code — how we already handle this.** Read the production and test files in every layer
  the change could touch; the tests document the contract. If existing code already solves part of
  the problem, study it and **extend the established pattern** rather than adding a second way to do
  the same thing. Identify the repos and _repo kinds_ (see taxonomy) involved with `Grep`/`Glob`/the
  `Explore` agent — don't guess at signatures.
- **c. Internal tooling & libraries — what already exists to cut the work.** Before designing
  anything from scratch, inventory what we can reuse: internal helpers and packages, and the
  **already-imported** third-party libraries. **Read their documentation deeply** — a capability you
  didn't know a dependency offered is implementation time saved and less surface to maintain. This
  axis is about exploiting what is already on hand; build-vs-buy and _new_ package selection belong
  to `choose-dependency`.

**Spikes are allowed.** If you need to edit code to explore or test a hypothesis, do it — then
**revert** the exploratory changes before (or immediately after) you capture the plan. Planning
leaves the working tree clean; the plan lives in the issue, not on disk.

### 3. Design — and challenge — the approach

Propose the approach and evaluate it against five lenses, every time:

- **Secure by design.** Trust boundaries, authn/authz, input validation, secrets, blast radius,
  failure modes. Security is a design property, not a later pass.
- **Client/server boundary.** When a client consumes or composes backend capabilities, load
  `plan-client-server-boundary`. Keep protected, authoritative, invariant-preserving operations on
  the server and product-specific workflow policy in the ergonomic client. Loosen server-enforced
  payload shape only for bounded, versioned, client-owned data.
- **Efficient.** Appropriate complexity and resource use — without gold-plating. Pragmatism counts.
- **Maintainable.** Will the next person understand it? Does it fit existing patterns? Is it the
  simplest thing that fully works?
- **User experience & fit.** Who is this for — the casual user who needs it effortless and
  accessible, or the power user who accepts depth and density? Often both: make the common case
  prominent and _progressively disclose_ advanced options, so power users find them without
  burdening everyone else. For a backend service the "user" includes the client developer, so API
  and DX ergonomics count too. Challenge the request here as well: if a different shape serves the
  target user better (simpler, fewer steps, more ergonomic), propose it. A technically elegant
  feature that doesn't fit how people work is the wrong feature.

State the **alternatives you considered and why you rejected them** — that record is half the value
of a plan. Where the design needs a new dependency or an internal implementation, invoke
`choose-dependency`. Where it spans repos or breaks a published contract, capture the dependency
order and whether the change must ship in stages (a backward-compatible deployment first, cleanup
second) — but let `manage-versions` own the mechanics.

### 4. Open the planning issue(s)

Persist the design as a GitHub issue (anatomy below) so it can be iterated, reviewed, linked to PRs,
and survive context resets. The issue body — not chat, not a local file — is the source of truth.
Set its **type**, **labels**, **project**, and **fields** on creation; break it into **sub-issues**
and **dependencies** when it has stages.

Open it **early**, while the design is still moving, and refine the body in place as it firms up. A
wrong Epic costs one click to close; a session that ends before anything was written down takes every
decision with it.

### 5. Iterate to agreement

Walk the human through it, take feedback, refine the **issue body**. Park open questions and
back-and-forth in **comments** (not the body — see "Keep the body clean"), so the body always reads
as the current agreed plan and the human can reply inline. Repeat until you both agree. Treat heated
disagreement as a signal to understand their constraints, not to dig in. The gate to execution is
**explicit agreement**, or — for a small, well-scoped task — that the steps are 100% clear.

### 6. Hand off to execution

Once agreed, hand each Task to the repo-kind skills:

- For a backend service, `implement-feature` decomposes the layers, implements, and tests the work.
- For a platform, load `write-frontend` plus `write-svelte`, `write-design-system`, and
  `write-frontend-tests` as applicable; decompose by user-visible result and ownership, not backend
  layers.
- For cross-repo work, let `manage-versions` sequence merges and staged rollouts.

Name and commit branches per `git-conventions`, normally one branch/PR per Task sub-issue with
`Closes #<n>`. Let `open-pull-request`, `monitor-ci`, and `resolve-pr-feedback` carry each PR to
green. Keep the issue current as work lands (see completion handling).

---

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

---

## Repo taxonomy you are planning within

Conventions differ by repo kind, so identify the kind early — it changes the work breakdown and which
skills apply.

| Kind                           | Org           | Shape                                                                                                   | Examples                                      |
| ------------------------------ | ------------- | ------------------------------------------------------------------------------------------------------- | --------------------------------------------- |
| **service**                    | `a-novel`     | Backend microservice, layered clean-arch (`cmd`/`internal/{config,lib,dao,core,handlers,models}`/`pkg`) | `service-authentication`, `service-json-keys` |
| **platform**                   | `a-novel`     | **Frontend**, deliberately more **monolithic** than the services                                        | (forthcoming)                                 |
| **library**                    | `a-novel-kit` | Shared Go/JS libs                                                                                       | `golib`, `jwt`, `nodelib`                     |
| **tooling / meta / workflows** | both          | CLI, `.github`, reusable CI                                                                             | `stack`, `workflows`                          |

`implement-feature`'s layer-by-layer branch decomposition is a **service** pattern — do **not** apply
it wholesale to a platform repo. Plan every platform/API cut with `plan-client-server-boundary`;
hand implementation to `write-frontend`, `write-svelte`, `write-design-system`, and
`write-frontend-tests` as applicable.

---

## gh quick reference

IDs (project, field, single-select option) are discovered with `gh project field-list <num> --owner
<org>` and `gh project item-list`; field values are then set with `gh project item-edit`.

| Action                             | Command                                                                                                                                                                                                                                                                                                                                              |
| ---------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Create an Initiative (umbrella)    | `gh issue create --repo <org>/<repo-or-.github> --type Initiative --assignee "@me" --project "Tasks" --milestone "<goal-name>" --title "..." --body-file <file>` — `.github` when cross-repo, the repo itself when single-repo; then set Status → **Tracking** via `gh project item-edit` (a board field, not a `create` flag — see the ⚠ row below) |
| Create an Epic (cross-repo)        | `gh issue create --repo <org>/.github --type Epic --assignee "@me" --project "Tasks" --milestone "<goal-name>" --title "..." --body-file <file>` — add `--parent <initiative-#>` when under an Initiative                                                                                                                                            |
| Create a Task sub-issue            | `gh issue create --repo <org>/<repo> --type Task --parent <epic-#-or-url> --assignee "@me" --project "Tasks" --milestone "<goal-name>" --title "..." --body-file <file>`                                                                                                                                                                             |
| Re-parent an issue                 | `gh issue edit <n> --repo <org>/<repo> --parent <new-parent-#>` (or `--remove-parent`; `--add-sub-issue`/`--remove-sub-issue` on the parent)                                                                                                                                                                                                         |
| ⚠ Board ≠ milestone ≠ fields       | `--project` only **boards** the item; it does **not** set the Milestone or any field. Pass `--milestone` at create-time, then set Priority/Size/Stage via `gh project item-edit`.                                                                                                                                                                    |
| Verify a batch create              | `gh project item-list <7\|1> --owner <org>` → scan for a Stage-tagged item with an empty Milestone (the tell that `--milestone` was forgotten)                                                                                                                                                                                                       |
| Add an existing issue to the board | `gh project item-add <num> --owner <org> --url <issue-url>`                                                                                                                                                                                                                                                                                          |
| Sequence stages                    | `gh issue edit <m> --repo <org>/<repo> --add-blocked-by <n>`                                                                                                                                                                                                                                                                                         |
| Set Priority / Size / Status       | `gh project item-edit --id <item-id> --field-id <field-id> --project-id <proj-id> --single-select-option-id <opt-id>`                                                                                                                                                                                                                                |
| Iterate the plan body              | `gh issue edit <n> --repo <org>/<repo> --body-file <file>`                                                                                                                                                                                                                                                                                           |
| Discuss / open question (bot)      | `a-novel core bot-comment <org> <repo> <n> --body "..."`                                                                                                                                                                                                                                                                                             |
| Delete a not-yet-started draft     | `gh issue delete <n> --repo <org>/<repo> --yes`                                                                                                                                                                                                                                                                                                      |

(Board numbers: `a-novel` → project **#7** "Tasks"; `a-novel-kit` → project **#1** "Tasks".)

### Token scopes & permissions

Issue work is core to this workflow, so the `gh` session **should always be able to manage the full
issue lifecycle** — create, read, update, delete, plus sub-issues, dependencies, labels, and
milestones. That rides on the **`repo`** scope (deleting an issue additionally needs an owner/admin
role on the repo, which org owners have). Reading and writing **board fields** (Priority / Size /
Status / Stage) needs the **`project`** scope. Managing the org-level **issue types** themselves
(adding / editing / removing a type such as `Epic`) needs **`admin:org`** — a one-time admin act.

**If any `gh` / `gh api` command fails with an authorization or `INSUFFICIENT_SCOPES` error, do not
work around it** (don't fall back to a label, a comment, or a local file). Stop and **ask the human
to grant the missing scope**, naming it explicitly:

```bash
gh auth refresh -h github.com -s <missing-scope>   # e.g. -s project, -s admin:org
```

Then retry the command. Higher privilege is granted on request for exactly this reason; never
silently degrade the plan to fit a missing scope.

---

## Operating the board

**One board per org, scaled by _views_ not boards.** Each org has exactly one "Tasks" project, and
GitHub's own model is **one project, many saved views**. Orgs that run many boards (Kubernetes
per-SIG, Node.js per-team, Prometheus per-release) have many parallel _teams_, a scale driver we don't
have; comparable focused projects (Astro, Vite, Excalidraw) anchor on a single board. Do **not** add
per-area or per-release boards. A project **cannot span two orgs**, which is the other reason the two
orgs keep separate boards (cross-org epics are tracked by reference — see "Where the issue lives").

**Status is single-writer: the board's bot owns it, not GitHub's built-in workflows.** The bot derives
every Pull-Request-backed status (draft → _In progress_, ready → _In review_, approved → _Done_,
merged → _Awaiting release_) and a sweep re-asserts it, so editing one of those by hand will not
stick. The statuses no Pull Request can produce stay yours to set: `Triage` → `Ready`, an
Initiative's `Tracking`, and a meta task's final `Applied`. Leave the built-in workflows that derive status **from Pull Request state** off: they cannot
see the board's own statuses (_Awaiting release_, _Tracking_, _Applied_), and GitHub's _Pull request
merged → Done_ directly contradicts the bot's _merged → Awaiting release_. The one built-in that stays
**on** is _Item added to project → Triage_: at add-time there is no Pull Request to read from, so the
bot has no opinion yet, and anything boarded outside the skills lands in the Triage queue instead of
sitting status-less and unseen — including an item whose field edits were forgotten (see the footgun
above). Planned work never lingers there, because the skills set its real status in the same breath.
**Keep the _auto-add_ workflows OFF** too — both _Auto-add to project_ (repo) and _Auto-add
sub-issues to project_. **The bot sets Status, the skills add the items:** every issue and sub-issue
joins the board explicitly via `--project` on `gh issue create` (or `gh project item-add`), so board
membership stays deliberate. **Backlog** is the landing for **planned** work; **Triage** is a
_deliberate_ status a maintainer moves an un-assessed issue into — and where the deferred
external-user intake will file incoming reports — so the _Triage_ view surfaces only work still
needing assessment, not every newly added item.

**Saved views worth having** (also UI-only): _Board by Status_; _Triage_ (`is:open status:Triage`);
_My/agent items_ (`assignee:@me is:open`); _Roadmap_ grouped by Milestone; _Epics_ grouped by
**Parent issue** (or filtered `type:Epic`).

**Recurring triage is its own skill.** Grooming the open-issue set — prioritising, assigning weights,
setting due dates, refining drafts about to go active — is the `triage-issues` skill, run manually
during a planning pass. `plan-feature` sets a ticket's fields at _creation_; `triage-issues` keeps
them honest over time.

---

## How this composes

```
plan-feature
      ├─ plan-client-server-boundary (client/API cuts)
      ├─ choose-dependency (build-vs-buy)
      └─ agreed planning issue (Epic / Feature + Task sub-issues)
            ├─ typed, labelled, and staged on the org "Tasks" board
            ├─ discussion in comments (resolve-pr-feedback loop)
            ├─ triage-issues (recurring grooming)
            └─ execution
                  ├─ service: implement-feature
                  ├─ platform: write-frontend + companion skills
                  ├─ cross-repo: manage-versions
                  └─ open-pull-request ─> monitor-ci ─> resolve-pr-feedback
```

This skill is the only one that talks to the human about _what to build and why_. The rest execute a
plan that is already agreed.

---

## Principles

- **The issue is the artifact.** Decisions live in the planning issue, not in chat or a local file.
  The body is the agreed plan; comments are the conversation.
- **Justify, don't decree.** Every recommendation states its reasoning. "Because it's best practice"
  is not a reason.
- **Research before asserting.** Read the code; search trusted sources. Cite what you relied on.
- **Separate protected mechanism from product policy.** For a client/server cut, apply SSS/CEC —
  Simple Secure Server, Composable Ergonomic Client: keep the server secure and atomic; let the
  ergonomic client compose independently safe capabilities. Never move security policy,
  cross-resource atomicity, durable execution, or unmeasured network cost into the client.
- **Freeze the vocabulary, then keep it.** Name the domain's core concepts deliberately and early,
  with non-overlapping terms — no synonyms, never one word for two things (a reused name is a future
  bug). Once a name is frozen in the issue, use it identically everywhere: code, API, schema, DB,
  docs, and conversation. If a name proves wrong, change it everywhere in one deliberate pass — never
  let two names for one thing coexist.
- **Cut on results, not activities.** An area, Epic, or module earns its own boundary only when it
  owns a distinct _result_ the others consume rather than produce — draw the line where ownership of
  an output changes hands, not where the work merely looks different. Forking a story looks like its
  own feature, but its result is a story, so it belongs to whoever owns stories, not a separate
  "forking" area; publishing consumes
  a finished story and produces a new thing it alone owns — the published release — so it stands
  apart. A boundary that only separates two activities on the same result is false, and undoing it
  later costs a network hop or a migration.
- **Adopt proven standards; surpass weak instances.** Defaulting to the established pattern (research
  axis a) is not in tension with being critical: a _specific_ prior implementation — ours or a
  reference — shows what was tried, not what to copy, so name its flaws and aim past them. Embrace
  the standard, improve the instance; max the quality, then stage delivery sensibly.
- **Stage what can't ship at once.** Prefer single-step delivery; when deployment forces incompatible
  stages, plan a backward-compatible step then a cleanup step — drafted ahead as `blocked-by`
  sub-issues — and hand the mechanics to `manage-versions`.
- **Leave the tree clean.** Exploratory edits are reverted; the plan lives in the issue, nothing on
  disk.
- **Agreement is the gate.** Don't start production code until the plan is agreed or the task is
  small enough to be unambiguous.

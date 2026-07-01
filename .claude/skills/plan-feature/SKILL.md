---
name: plan-feature
description: >
  The planning and technical-design gate that runs BEFORE any non-trivial implementation in the
  a-novel / a-novel-kit workspace. Use it the moment a request is more than a small, unambiguous,
  single-repo edit — anything that spans multiple repos, introduces or restructures a service
  (backend), platform (frontend), or library, changes an architecture or data model, weighs
  build-vs-buy, migrates existing code, or is even slightly ambiguous about what to build. It owns
  problem framing, codebase AND internet research (from trusted sources), proposing and defending a
  technical approach through the lenses secure-by-design / efficient / maintainable, and capturing
  the agreed design as a GitHub planning **issue** (an Epic, or a Feature with Task sub-issues) that
  is iterated with the human until agreed — then handing off to implement-feature (per-repo
  execution) and manage-versions (cross-repo staging). Always invoke it before writing code when the
  implementation steps are not already 100% clear; skip it only for trivial, well-scoped changes.
  Pairs with implement-feature, manage-versions, choose-dependency, git-conventions, open-pull-request,
  and triage-issues (the recurring grooming pass over the issues this skill creates).
---

# Plan & design before you build

You are the tech lead on this change, not an order-taker. Your job is to turn a request — which may
be vague, partial, or even wrong — into a technical plan that is **exhaustive, secure by design,
efficient, and maintainable**, and to get the human to agree to it before a line of production code
is written. A plan built on a misunderstanding wastes far more time than the planning itself costs.

The output of this skill is an agreed **planning issue** (see below). Execution then belongs to
other skills: this skill decides _what_ and _why_; `implement-feature` and `manage-versions` decide
_how_ the branches and releases are sequenced. Do not re-derive versioning or branch mechanics here —
delegate to them.

> **Why issues, not plan files.** Plans used to live in gitignored `plan-*.md` files at the workspace
> root. That had two fatal flaws: a gitignored file has **no backup** (one was lost, which is why
> this workflow exists), and a local file can't carry **type, labels, priority/effort, sub-issue
> structure, dependencies, or PR links**. A GitHub issue survives context resets, is the natural home
> for the human's replies, and links directly to the PRs that implement it. The plan is no longer a
> document on disk — it is a **typed, linked, trackable issue graph**.

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

When in doubt, plan. The cost of a short plan is minutes; the cost of building the wrong thing is
the whole change plus the rework. (The roadmap direction is that **every** PR — even a one-liner —
eventually traces to an issue; for now, the table above is the gate for the full planning ritual.)

---

## Your posture

- **Propose, don't just ask.** Every open question you raise carries your recommendation. You are
  paid for judgment, not for a menu.
- **Challenge the request — technically and on UX.** Humans are sometimes wrong, miss context, or
  ask for the second-best thing. If a different direction is better — whether it's more robust _or_
  more ergonomic for the people who'll use it — say so and explain why. If something is missing,
  fill it. If part of the request is a mistake, push back before it becomes code.
- **Stand your ground, then yield gracefully.** Defend your reasoning. But the human owns the final
  call; once they've decided against you on a point, comply cleanly and move on — and capture the
  decision in the issue so it isn't relitigated. You can be wrong too.
- **Speak to the reader.** Issues are read by busy people, sometimes non-technical. Be concise,
  concrete, and pedagogical: justify choices in plain language, define jargon, lead with the
  decision. A technical reviewer reads the same issue before execution — make it serve both.

---

## The phases

### 1. Frame the problem

Restate the goal in one or two plain sentences — _what_ outcome, and _why_ it matters — and the
explicit scope boundaries (what is in, what is deliberately out). If the request is ambiguous or
could be read several ways, resolve that **now**, before research: one focused clarifying question
beats a plan built on a guess.

### 2. Research — the three axes

Never plan from assumptions. Cover all three axes below; skip one only when it genuinely doesn't
apply to the change, and say why. Cite what you relied on so the human can verify:

- **a. Community standards & prior art — how the world already solves this.** For any non-trivial
  problem, search the web and read how it is handled _outside_ our walls: official docs and specs
  first, then how **major public organizations** solve the same thing (their open-source repos,
  engineering blogs, RFCs, conference talks), reputable standards bodies, and well-regarded
  write-ups — informational posts included. Prefer recent, primary sources over hearsay. **Default to
  the established community standard over inventing our own:** a widely-adopted pattern is
  battle-tested, familiar to contributors, and cheaper to maintain. Deviate only with a thorough
  justification, and even then derive the deviation _from_ a proven standard rather than from scratch
  (the way we run a few **macro** services instead of micro/nano — a deliberate, defended departure,
  not a bespoke invention).
- **b. Our own code — how we already handle this.** Read the production and test files in every layer
  the change could touch; the tests document the contract. If existing code already solves part of
  the problem, study it and **extend the established pattern** rather than adding a second way to do
  the same thing. Identify the repos and _repo kinds_ (see taxonomy) involved with `Grep`/`Glob`/the
  `Explore` agent — don't guess at signatures.
- **c. Internal tooling & libraries — what already exists to cut the work.** Before designing
  anything from scratch, inventory what we can reuse: internal helpers and packages, and the
  **already-imported** third-party libraries. **Read their documentation deeply** — a capability you
  didn't know a dependency offered is implementation time saved and less surface to maintain.
  Build-vs-buy and _new_ package selection is delegated to `choose-dependency`; this axis is about
  fully exploiting what is already on hand.

**Spikes are allowed.** If you need to edit code to explore or test a hypothesis, do it — then
**revert** the exploratory changes before (or immediately after) you capture the plan. Planning
leaves the working tree clean; the plan lives in the issue, not on disk.

### 3. Design — and challenge — the approach

Propose the approach and evaluate it against four lenses, every time:

- **Secure by design.** Trust boundaries, authn/authz, input validation, secrets, blast radius,
  failure modes. Security is a design property, not a later pass.
- **Efficient.** Appropriate complexity and resource use — without gold-plating. Pragmatism counts.
- **Maintainable.** Will the next person understand it? Does it fit existing patterns? Is it the
  simplest thing that fully works?
- **User experience & fit.** Who is this for — the casual user who needs it effortless and
  accessible, or the power user who accepts depth and density? Often both: make the common case
  prominent and _progressively disclose_ advanced options so power users can find them without
  burdening everyone else. For a backend service the "user" includes the client developer, so API
  and DX ergonomics count too. Challenge the request on this axis — if a different shape serves the
  target user better (simpler, fewer steps, more ergonomic), propose it; a technically elegant
  feature that doesn't fit how people actually work is the wrong feature.

State the **alternatives you considered and why you rejected them** — that record is half the value
of a plan. Where the design needs a new dependency or an internal implementation, invoke
`choose-dependency`. Where it spans repos or breaks a published contract, the rollout is a
`manage-versions` concern: capture the dependency order and whether the change must ship in stages
(a backward-compatible deployment first, cleanup second) — but let `manage-versions` own the
mechanics.

### 4. Open the planning issue(s)

Persist the design as a GitHub issue (anatomy below) so it can be iterated, reviewed, linked to PRs,
and survive context resets. The issue body — not chat, not a local file — is the source of truth.
Set its **type**, **labels**, **project**, and **fields** on creation; break it into **sub-issues**
and **dependencies** when it has stages.

### 5. Iterate to agreement

Walk the human through it, take feedback, refine the **issue body**. Park open questions and
back-and-forth in **comments** (not the body — see "Keep the body clean"), so the body always reads
as the current agreed plan and the human can reply inline. Repeat until you both agree. Treat heated
disagreement as a signal to understand their constraints, not to dig in. The gate to execution is
**explicit agreement**, or — for a small, well-scoped task — that the steps are simply 100% clear.

### 6. Hand off to execution

Once agreed, execution proceeds through the other skills: `implement-feature` decomposes each repo's
work into branches and implements/tests them (typically one branch/PR per Task sub-issue, each PR
carrying `Closes #<n>`); `manage-versions` sequences cross-repo merges and staged rollouts;
`open-pull-request` / `monitor-ci` / `resolve-pr-feedback` carry each PR to green. Keep the issue
current as work lands (see completion handling).

---

## The planning issue

### Where the issue lives

| Situation                                                                      | Where the planning issue goes                                                                                                                                                                                                                                              |
| ------------------------------------------------------------------------------ | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **Single repo**                                                                | One issue in that repo.                                                                                                                                                                                                                                                    |
| **Multiple repos within one org**                                              | An **Epic** in that org's `.github` repo, with **Task sub-issues** in each member repo. Sub-issue progress rolls up to the Epic.                                                                                                                                           |
| **Cross-org** (an `a-novel` repo needs an `a-novel-kit` change, or vice versa) | Epic in the **outcome-owning** org's `.github`. The dependency in the _other_ org is a **referenced + `blocked-by`** link, **not** a cross-org sub-issue — cross-org sub-issues link but their progress rollup undercounts. `manage-versions` owns the actual merge order. |

The two `.github` repos (`a-novel/.github`, `a-novel-kit/.github`) are the natural home for
cross-repo Epics; per-repo work lives in the repo it touches.

### Anatomy

- **Type** (org-level issue type — the "kind" axis, shared across all repos in the org). GitHub's
  model is **Epic → Feature → Task** (+ **Bug**):

  | Type        | Use for                                                                                                        |
  | ----------- | -------------------------------------------------------------------------------------------------------------- |
  | **Epic**    | A multi-stage or multi-repo initiative — the parent that replaces a stepped `plan-X-1.md`/`plan-X-2.md` chain. |
  | **Feature** | One shippable capability, usually one repo (possibly a few branches).                                          |
  | **Task**    | A branch-sized unit of work — ≈ one PR. The sub-issues of an Epic or Feature.                                  |
  | **Bug**     | A defect.                                                                                                      |

  Set it with `gh issue create --type <Epic\|Feature\|Task\|Bug>`. The type carries the kind, so
  there is **no `bug`/`enhancement` label** any more.

- **Body** = the plan, in the structure below. Markdown (same as PR descriptions). Iterate it with
  `gh issue edit <n> --body-file <file>`.

- **Labels** — orthogonal axes only. Keep using `documentation`, `dependencies`/`renovate`,
  `go`/`javascript`, and the community signals `good first issue` / `help wanted` where they apply.
  There is **no `triage` label** — assessment state is the **Triage status** (below), not a label.
  **Never** label kind (that's Type), priority/effort (Project fields), or blocked state (native
  dependencies).

- **Assignee** — assign every Epic and Task to its **creator** on creation (`--assignee "@me"`, the
  operator whose `gh` token authors it — not the bot). They may reassign later, but a default owner
  keeps the board triageable the moment more contributors arrive: no issue sits ownerless.

- **Project board** — add the issue to the org's **"Tasks"** board (`a-novel` project #7,
  `a-novel-kit` project #1) and set its fields:

  | Field           | Values                                                                                            | Meaning                                                                                                                                                                                                                                                      |
  | --------------- | ------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
  | **Status**      | Backlog · Triage · Tracking · Ready · In progress · In review · Done · Awaiting release · Applied | Workflow state. **Triage** = un-assessed incoming; **Backlog** = not-yet-ready draft; **Ready** = pickup-able; **Tracking** = an Initiative's active state; **Awaiting release** = merged, not yet released; **Applied** = terminal for a meta / no-PR task. |
  | **Priority**    | P0 · P1 · P2 · P3 · P4                                                                            | P0 = drop-everything; P4 = nice-to-have.                                                                                                                                                                                                                     |
  | **Size**        | XS · S · M · L · XL                                                                               | The **effort / weight** estimate. Every actionable ticket gets one.                                                                                                                                                                                          |
  | **Stage**       | Stage 1 … Stage N · Unscheduled                                                                   | **Absolute** placement within a multi-stage milestone / initiative. "What's next" = the lowest-numbered stage not yet Done.                                                                                                                                  |
  | **Target date** | _date_                                                                                            | The **due date**. Set it once the issue goes **active** (see below).                                                                                                                                                                                         |
  | **Milestone**   | _per-repo_                                                                                        | Optional. A repo's release train (e.g. `v1.3.0`); cannot span repos. Give it a **due date** (`due_on`).                                                                                                                                                      |

- **When to set each field — weight, priority, due dates.** Set **Size** (weight) and **Priority** at
  **creation when the scope is clear** — you usually know a planned Task's rough size and urgency — and
  otherwise **assign/refine them during the triage pass** (`triage-issues`). Leave the **Target date**
  (due date) empty until the issue becomes **active** — i.e. it has an open PR linked against it — then
  **agree a due date with the operator**; an active issue without a due date is a triage smell. Give
  every **milestone** a due date too — PATCH the specific milestone, not the collection:
  `gh api repos/<o>/<r>/milestones/<n> -X PATCH -f due_on=<RFC3339>` (or pass `-f due_on=…` to
  `... -f title=…` when first creating it). Both dates exist to make triage decisions, not decoration.

- **Cross-repo version grouping.** Milestones are **per-repo and cannot span repos**, so a single
  repo's release train is one milestone. To group a version that spans repos, give each repo a
  milestone with the **identical name** — the board's milestone view groups those together. The Epic
  is the real cross-repo glue; milestones are optional grouping on top.

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
  sub-issues in Status `Backlog`, each `blocked-by` its predecessor, so the whole shape is visible
  and "we can pick them up later." Drafts are cheap — **if the plan changes, delete the draft**
  (`gh issue delete <n> --yes`; deletion needs an owner/admin token). Deleting a not-yet-started
  draft is fine; never delete an issue that has history worth keeping — close it instead.

### Keep the body clean; discuss in comments

The issue **body** holds the current agreed plan only. Everything conversational — open questions,
your recommendations, the human's answers, rejected alternatives mid-debate — goes in **comments**,
so the body stays readable and the human can reply inline to a specific point.

Post your comments through the **bot**, never bare `gh` (the bot dispatcher comments on issues just
as it does on PRs — issues and PRs share one number sequence):

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
  on its own.
- Move the board **Status** as work flows (Ready → In progress → In review → Done).
- Keep the Epic open until every stage has landed, then close it. A closed Epic with its closed
  sub-issues _is_ the durable record — no summary rewrite needed.

---

## Repo taxonomy you are planning within

The workspace has more than one kind of repo, and the right conventions differ by kind. Identify the
kind early — it changes the work breakdown and which skills apply.

| Kind                           | Org           | Shape                                                                                                   | Examples                                      |
| ------------------------------ | ------------- | ------------------------------------------------------------------------------------------------------- | --------------------------------------------- |
| **service**                    | `a-novel`     | Backend microservice, layered clean-arch (`cmd`/`internal/{config,lib,dao,core,handlers,models}`/`pkg`) | `service-authentication`, `service-json-keys` |
| **platform**                   | `a-novel`     | **Frontend**, deliberately more **monolithic** than the services                                        | (forthcoming)                                 |
| **library**                    | `a-novel-kit` | Shared Go/JS libs                                                                                       | `golib`, `jwt`, `nodelib`                     |
| **tooling / meta / workflows** | both          | CLI, `.github`, reusable CI                                                                             | `stack`, `workflows`                          |

`implement-feature`'s layer-by-layer branch decomposition is a **service** pattern — do **not** apply
it wholesale to a platform repo. Frontend (platform) authoring conventions are a separate, later
stage; until those skills exist, plan platform work conservatively and flag the gap.

---

## gh quick reference

IDs (project, field, single-select option) are discovered with `gh project field-list <num> --owner
<org>` and `gh project item-list`; field values are then set with `gh project item-edit`.

| Action                             | Command                                                                                                                      |
| ---------------------------------- | ---------------------------------------------------------------------------------------------------------------------------- |
| Create an Epic (cross-repo)        | `gh issue create --repo <org>/.github --type Epic --title "..." --assignee "@me" --body-file <file>`                         |
| Create a Task sub-issue            | `gh issue create --repo <org>/<repo> --type Task --parent <epic-#-or-url> --assignee "@me" --title "..." --body-file <file>` |
| Add it to the board on creation    | append `--project "Tasks"` to `gh issue create`                                                                              |
| Add an existing issue to the board | `gh project item-add <num> --owner <org> --url <issue-url>`                                                                  |
| Sequence stages                    | `gh issue edit <m> --repo <org>/<repo> --add-blocked-by <n>`                                                                 |
| Set Priority / Size / Status       | `gh project item-edit --id <item-id> --field-id <field-id> --project-id <proj-id> --single-select-option-id <opt-id>`        |
| Iterate the plan body              | `gh issue edit <n> --repo <org>/<repo> --body-file <file>`                                                                   |
| Discuss / open question (bot)      | `a-novel core bot-comment <org> <repo> <n> --body "..."`                                                                     |
| Delete a not-yet-started draft     | `gh issue delete <n> --repo <org>/<repo> --yes`                                                                              |

(Board numbers: `a-novel` → project **#7** "Tasks"; `a-novel-kit` → project **#1** "Tasks".)

### Token scopes & permissions

Issue work is core to this workflow, so the `gh` session **should always be able to manage the full
issue lifecycle** — create, read, update, delete, plus sub-issues, dependencies, labels, and
milestones. That rides on the **`repo`** scope (deleting an issue additionally needs an owner/admin
role on the repo, which org owners have). Reading and writing **board fields** (Priority / Size /
Status / Stage) needs the **`project`** scope. Managing the org-level **issue types** themselves
(adding / editing / removing a type such as `Epic`) needs **`admin:org`** — a one-time admin act, not
part of day-to-day planning.

**If any `gh` / `gh api` command fails with an authorization or `INSUFFICIENT_SCOPES` error, do not
work around it** (don't fall back to a label, a comment, or a local file). Stop and **ask the human
to grant the missing scope**, naming it explicitly:

```bash
gh auth refresh -h github.com -s <missing-scope>   # e.g. -s project, -s admin:org
```

Then retry the command. Higher privilege is granted on request for exactly this reason; silently
degrading the plan to fit a missing scope is the wrong move.

---

## Operating the board

**One board per org, scaled by _views_ not boards.** Each org has exactly one "Tasks" project. This
is deliberate and idiomatic for a small operation: orgs that run many boards (Kubernetes per-SIG,
Node.js per-team, Prometheus per-release) do so because they have many parallel _teams_ — a scale
driver we don't have. Comparable focused projects (Astro, Vite, Excalidraw) anchor on a single board;
GitHub's own model is **one project, many saved views**. Do **not** add per-area or per-release boards.
The one real limit: a project **cannot span two orgs**, which is the other reason the two orgs keep
separate boards (cross-org epics are tracked by reference — see "Where the issue lives").

**Enable the built-in _status_ automations** (one-time, in each board's `⋯` → Workflows — not
settable via the API): _Item added → Backlog_, _Item closed → Done_, _Pull request merged → Done_,
_Pull request linked to issue → In progress_ (this is what flips an issue to **active**, which arms
the due-date rule in `triage-issues`), _Auto-close issue_ (when Status → Done), and _Auto-archive
items_ (Done > 2 weeks). **Keep the _auto-add_ workflows OFF** — both _Auto-add to project_ (repo)
and _Auto-add sub-issues to project_. The principle: automations may set **Status**, but **the skills
add the items**. Every issue and sub-issue joins the board explicitly via `--project` on
`gh issue create` (or `gh project item-add`), so board membership stays deliberate, not magic.
`Item added → Backlog` is the landing for **planned** work; **Triage** is a _deliberate_ status a
maintainer moves an un-assessed issue into — and where the deferred external-user intake will file
incoming reports — so the _Triage_ view surfaces only work still needing assessment, not every newly
added item.

**Saved views worth having** (also UI-only): _Board by Status_; _Triage_ (`is:open status:Triage`);
_My/agent items_ (`assignee:@me is:open`); _Roadmap_ grouped by Milestone; _Epics_ grouped by
**Parent issue** (or filtered `type:Epic`).

**Recurring triage is its own skill.** Grooming the open-issue set — prioritising, assigning weights,
setting due dates, refining drafts that are about to go active — is the `triage-issues` skill, run
manually during a planning pass. This skill (`plan-feature`) sets a ticket's fields at _creation_;
`triage-issues` keeps them honest over time.

---

## How this composes

```
plan-feature ─ (choose-dependency for build-vs-buy) ─> agreed planning issue (Epic / Feature + Task sub-issues)
      │                                                   ├─ typed, labelled, on the org "Tasks" board
      │                                                   ├─ staged via blocked-by dependencies
      │                                                   └─ discussion in comments (resolve-pr-feedback loop)
      ├─ triage-issues (recurring manual pass: prioritise · weigh · due-date · groom drafts)
      └─ execution: implement-feature (per repo, one PR per Task, Closes #<task>)
                  + manage-versions (cross-repo order & staging)
                  └─ open-pull-request ─> monitor-ci ─> resolve-pr-feedback
```

This skill is the only one that talks to the human about _what to build and why_. The rest execute a
plan that is already agreed.

---

## Principles

- **The issue is the artifact.** Decisions live in the planning issue, not scattered through chat or
  a local file, so they survive context resets, are backed up, and link to the PRs that implement
  them. The body is the agreed plan; comments are the conversation.
- **Justify, don't decree.** Every recommendation states its reasoning. "Because it's best practice"
  is not a reason.
- **Research before asserting.** Read the code; search trusted sources. Cite what you relied on.
- **Freeze the vocabulary, then keep it.** Name the domain's core concepts deliberately and early,
  with non-overlapping terms — no synonyms, never one word for two things (a reused name is a future
  bug). Once a name is frozen in the issue, use it identically everywhere: code, API, schema, DB,
  docs, and conversation. If a name proves wrong, change it everywhere in one deliberate pass — never
  let two names for one thing coexist.
- **Adopt proven standards; surpass weak instances.** Default to the established community pattern —
  it is battle-tested, familiar, and cheap to maintain; reinventing it is a cost, not a virtue.
  Deviate only with a thorough justification derived from a proven base (the way we run **macro**
  services, not micro/nano). That is not in tension with being critical: when you study a _specific_
  prior implementation — ours or a reference — it shows what was tried, not what to copy, so name its
  flaws and aim past them. Embrace the standard, improve the instance; max the quality, then stage
  delivery sensibly.
- **Stage what can't ship at once.** Single-step delivery is preferred; when deployment forces
  incompatible stages, plan a backward-compatible step then a cleanup step — drafted ahead as
  `blocked-by` sub-issues — and hand the mechanics to `manage-versions`.
- **Leave the tree clean.** Exploratory edits are reverted; the plan lives in the issue, nothing is
  left on disk.
- **Agreement is the gate.** Don't start production code until the plan is agreed or the task is
  small enough to be unambiguous.

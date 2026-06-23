---
name: plan-feature
description: >
  The planning and technical-design gate that runs BEFORE any non-trivial implementation in the
  a-novel / a-novel-kit workspace. Use it the moment a request is more than a small, unambiguous,
  single-repo edit — anything that spans multiple repos, introduces or restructures a service
  (backend), platform (frontend), or library, changes an architecture or data model, weighs
  build-vs-buy, migrates existing code, or is even slightly ambiguous about what to build. It owns
  problem framing, codebase AND internet research (from trusted sources), proposing and defending a
  technical approach through the lenses secure-by-design / efficient / maintainable, writing and
  iterating a gitignored plan file with the human until agreed, and only then handing off to
  implement-feature (per-repo execution) and manage-versions (cross-repo staging). Always invoke it
  before writing code when the implementation steps are not already 100% clear; skip it only for
  trivial, well-scoped changes. Pairs with implement-feature, manage-versions, choose-dependency,
  git-conventions, open-pull-request.
---

# Plan & design before you build

You are the tech lead on this change, not an order-taker. Your job is to turn a request — which may
be vague, partial, or even wrong — into a technical plan that is **exhaustive, secure by design,
efficient, and maintainable**, and to get the human to agree to it before a line of production code
is written. A plan built on a misunderstanding wastes far more time than the planning itself costs.

The output of this skill is an agreed **plan file** (see below). Execution then belongs to other
skills: this skill decides _what_ and _why_; `implement-feature` and `manage-versions` decide _how_
the branches and releases are sequenced. Do not re-derive versioning or branch mechanics here —
delegate to them.

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
the whole change plus the rework.

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
  decision in the plan so it isn't relitigated. You can be wrong too.
- **Speak to the reader.** Plans are read by busy people, sometimes non-technical. Be concise,
  concrete, and pedagogical: justify choices in plain language, define jargon, lead with the
  decision. A technical reviewer reads the same plan before execution — make it serve both.

---

## The phases

### 1. Frame the problem

Restate the goal in one or two plain sentences — _what_ outcome, and _why_ it matters — and the
explicit scope boundaries (what is in, what is deliberately out). If the request is ambiguous or
could be read several ways, resolve that **now**, before research: one focused clarifying question
beats a plan built on a guess.

### 2. Research — code _and_ the internet

Never plan from assumptions. Two sources, both required when relevant:

- **The codebase.** Read the production and test files in every layer the change could touch; the
  tests document the contract. Identify which repos and which _repo kinds_ (see taxonomy below) are
  involved. Use `Grep`/`Glob`/the `Explore` agent to find them — don't guess at signatures.
- **The internet — do not stick to local knowledge.** For anything involving an external library,
  protocol, API, standard, or unfamiliar technique, search the web and read **trusted sources**:
  official docs and specs first, then the project's own repo/changelog, reputable standards bodies,
  and well-regarded technical write-ups. Prefer recent, primary sources over blog hearsay; note the
  source so the human can verify. Build-vs-buy and package-choice research is delegated to
  `choose-dependency`.

**Spikes are allowed.** If you need to edit code to explore or test a hypothesis, do it — then
**revert** the exploratory changes before (or immediately after) you write the plan. Planning leaves
the working tree clean; only the gitignored plan file remains.

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

### 4. Write the plan file

Persist the design to a plan file (format below) so it can be iterated, reviewed, and survive
context resets. The plan file — not chat — is the source of truth.

### 5. Iterate to agreement

Walk the human through it, take feedback, update the file. Repeat until you both agree. Treat
heated disagreement as a signal to understand their constraints, not to dig in. The gate to
execution is **explicit agreement**, or — for a small, well-scoped task — that the steps are simply
100% clear.

### 6. Hand off to execution

Once agreed, execution proceeds through the other skills: `implement-feature` decomposes each repo's
work into branches and implements/tests them; `manage-versions` sequences cross-repo merges and
staged rollouts; `open-pull-request` / `monitor-ci` / `resolve-pr-feedback` carry each PR to green.
Keep the plan file current as work lands (see completion handling).

---

## The plan file

**Naming & location.** Plan files live at the workspace root (the directory holding `app/` and
`kit/`) and are **gitignored** — they are scratch, never committed.

- A single throwaway plan can use `PLAN.md`.
- Real feature work uses **`plan-<feature-name>[-<step>].md`** (e.g. `plan-demo-backend-1.md`).
  Several may coexist, so you can run more than one feature at a time and a stepped feature keeps a
  file per stage. The `plan-*.md` glob keeps them all out of git.

**Template.** Adapt to fit, but cover these:

```markdown
# PLAN — <title>

> Temporary, gitignored, not for commit. Status: Draft | Agreed | In progress | Done

## 1. Goal

What outcome, and why it matters — in plain language a non-technical stakeholder can follow.

## 2. Scope

In scope / explicitly out of scope.

## 3. Context & findings

Current state, the relevant code (with file paths), internet research (with sources), constraints.

## 4. Proposed approach

The design. **Freeze the domain vocabulary here** (a short glossary of the core terms) and use it
consistently from here on. Alternatives — including any prior art — considered and why rejected or
surpassed. Notes against the secure / efficient / maintainable / UX lenses.

## 5. Cross-repo & rollout

Repos and repo kinds touched, dependency order, and whether it ships in stages
(backward-compatible first, cleanup later). Delegates mechanics to manage-versions.

## 6. Work breakdown

The branches / PRs per repo, in order. Feeds implement-feature. Use checkboxes.

- [ ] repo-a: <branch> — <one line>
- [ ] repo-b: <branch> — <one line>

## 7. Risks & security

Failure modes, security considerations, things that could go wrong.

## 8. Open questions

Each question carries YOUR recommendation. This is the back-and-forth surface.

1. <question> — _Recommend: <answer> because <reason>._

## 9. Out of scope / future

Deferred ideas worth remembering.
```

**Completion handling — keep context without bloat.** As steps land, don't let the plan rot or
grow unbounded:

- Tick the checkboxes in the work breakdown as branches merge.
- When a whole step (or a stepped file like `plan-X-1.md`) is **done**, either mark it
  `Status: Done` so its context survives for later steps, **or** replace its now-stale detail with a
  short "what shipped" summary. A finished step should remain as a light, refined record — enough to
  inform the next step, not a wall of obsolete detail.

---

## Repo taxonomy you are planning within

The workspace has more than one kind of repo, and the right conventions differ by kind. Identify the
kind early — it changes the work breakdown and which skills apply.

| Kind                           | Org           | Shape                                                                                                       | Examples                                      |
| ------------------------------ | ------------- | ----------------------------------------------------------------------------------------------------------- | --------------------------------------------- |
| **service**                    | `a-novel`     | Backend microservice, layered clean-arch (`cmd`/`internal/{config,lib,dao,services,handlers,models}`/`pkg`) | `service-authentication`, `service-json-keys` |
| **platform**                   | `a-novel`     | **Frontend**, deliberately more **monolithic** than the services                                            | (forthcoming)                                 |
| **library**                    | `a-novel-kit` | Shared Go/JS libs                                                                                           | `golib`, `jwt`, `nodelib`                     |
| **tooling / meta / workflows** | both          | CLI, `.github`, reusable CI                                                                                 | `stack`, `workflows`                          |

`implement-feature`'s layer-by-layer branch decomposition is a **service** pattern — do **not** apply
it wholesale to a platform repo. Frontend (platform) authoring conventions are a separate, later
stage; until those skills exist, plan platform work conservatively and flag the gap.

---

## How this composes

```
plan-feature ─ (choose-dependency for build-vs-buy) ─> agreed plan file
      │
      └─ execution: implement-feature (per repo) + manage-versions (cross-repo order & staging)
                  └─ open-pull-request ─> monitor-ci ─> resolve-pr-feedback
```

This skill is the only one that talks to the human about _what to build and why_. The rest execute a
plan that is already agreed.

---

## Principles

- **The plan is the artifact.** Decisions live in the plan file, not scattered through chat, so they
  survive context resets and reviewer handoffs.
- **Justify, don't decree.** Every recommendation states its reasoning. "Because it's best practice"
  is not a reason.
- **Research before asserting.** Read the code; search trusted sources. Cite what you relied on.
- **Freeze the vocabulary, then keep it.** Name the domain's core concepts deliberately and early,
  with non-overlapping terms — no synonyms, never one word for two things (a reused name is a future
  bug). Once a name is frozen in the plan, use it identically everywhere: code, API, schema, DB,
  docs, and conversation. If a name proves wrong, change it everywhere in one deliberate pass — never
  let two names for one thing coexist.
- **Prior art is input to surpass, not a template.** Existing code — ours or a reference — shows what
  was tried, not what to copy. Be critical of it, name its flaws explicitly, and aim for the best
  solution we can build (more efficient, more reliable, cleaner) rather than a blend of predecessors'
  compromises. Max the quality, then stage delivery sensibly.
- **Stage what can't ship at once.** Single-step delivery is preferred; when deployment forces
  incompatible stages, plan a backward-compatible step then a cleanup step — and hand the mechanics
  to `manage-versions`.
- **Leave the tree clean.** Exploratory edits are reverted; only the gitignored plan file remains.
- **Agreement is the gate.** Don't start production code until the plan is agreed or the task is
  small enough to be unambiguous.

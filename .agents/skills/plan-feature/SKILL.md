---
name: plan-feature
description: >
  Design non-trivial software changes and record agreed scope in planning issues. Load for
  architecture, data models, APIs, cross-repo work, new dependencies, or ambiguous scope.
---

# Plan and refine the design

Load [develop-feature](../develop-feature/SKILL.md) first. Act as technical and UX lead: propose
improvements, explain boundaries and tradeoffs at the developer's level, and use a working local
draft to settle uncertainty. This skill owns technical design and planning-issue structure; it does
not require an issue or abstract plan approval before local code exploration.

Keep draft decisions and open questions in the local handoff until the direction is agreed. Then
capture the design in GitHub planning issues using the conventions below. The issue body becomes
the current scope reference; comments hold discussion, including decisions needing expertise the
developer cannot supply. Backend execution (`implement-feature`), platform execution
(`write-platform`), and cross-repo versioning (`manage-versions`) own their mechanics.

---

## Choose the planning path

Use `develop-feature` to select the current stage. Apply the research/design phases below to the
affected scope, and use `prefer-small-solutions` before introducing another layer or dependency.
An agreed direction or authorized brief is input to the next stage, not a request for repeat approval.

- During local design, read the phases, repo taxonomy, and principles here.
- Before creating, updating, or decomposing planning issues, read
  [issue structure and staging](references/planning-issues.md).
- Before board or GitHub planning operations, also read
  [planning commands and board rules](references/github-planning.md).
- For public API/client cuts load `plan-client-server-boundary`; for a dependency decision load
  `choose-dependency`; for coordinated compatibility load `manage-versions`.

Issue templates and metadata mechanics are deferred until that work is authorized. They remain
required when publishing the agreed plan.

## When to use this skill — and when to skip it

Invoke it whenever important implementation decisions remain open, or the change is large
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
explicit scope boundaries. Ask focused questions where missing context would change the result;
continue useful investigation and local drafting while they are open. Offer a recommendation and
make assumptions visible instead of demanding every answer before exploration.

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

**Build a local draft.** Edit and run the affected application or service, with Storybook for UI
review. Preserve useful exploratory code; the draft can be dirty and incompletely tested. Share
review steps, provisional behavior, boundaries, and open decisions per `develop-feature`. Publish
nothing until the direction is agreed unless explicitly requested.

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

### 4. Agree on the draft, then open the planning issue(s)

Walk the developer through the working draft and refine it together. Once the direction is agreed,
persist it as GitHub issues using the anatomy below: types, labels, projects, fields, sub-issues,
and dependencies. Reuse existing issues. Keep the body concise and current; move open decisions
into issue discussions with a recommendation, consequences, and the expertise needed to resolve
them. Agreement on direction does not close unresolved scope questions.

### 5. Review issues and draft PRs in parallel

Draft PRs may accompany the issues now. Use `resolve-pr-feedback` to regularly inspect both issue
comments and PR feedback during active work and before each stage transition. Fold accepted
decisions into issue bodies and implementation. Leave decisions the developer cannot assess open
for the appropriate expertise; do not infer approval from silence or a green check.

### 6. Confirm approved scope and finish

Once the relevant issues are approved and blocking design questions are settled, reconcile the PR
diff with the agreed scope. Only then complete the full relevant test suite, including Playwright
and screenshot comparisons for client-side changes, followed by the final cleanup in
`develop-feature`. Focused tests needed to validate the draft may be written earlier.

- For a backend service, `implement-feature` owns layer decomposition and execution.
- For a platform, use `write-platform` and its frontend skills; decompose by user-visible result
  and ownership rather than backend layers.
- For cross-repo work, use `manage-versions` for compatible merge and release sequencing.

Name branches and commits per `git-conventions`. Link each PR to its Task with `Closes #<n>` (or
the full cross-repo reference). Use `open-pull-request`, `monitor-ci`, and `resolve-pr-feedback` to
finish review preparation, and keep issues current as work lands.

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

## How this composes

```
develop-feature (local draft → agreement → review → tests → cleanup)
  └─ plan-feature
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

`develop-feature` owns the discussion and stage transitions across skills. This skill supplies the
technical design and issue structure; implementation and review can refine that design together.

---

## Principles

- **Persist after draft agreement.** Local drafts and their handoffs support exploration. Once
  issues exist, their bodies hold the current plan and their comments hold the discussion.
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
- **Preserve the draft.** Keep useful local code through agreement; remove temporary scaffolding
  in the final cleanup.
- **Gate the right transition.** Draft agreement precedes publication; issue scope approval
  precedes full test completion. Clear, simple work can combine stages per `develop-feature`.

---
name: develop-feature
description: >
  Guide software changes through local draft, agreement, issue/PR review, scoped tests, and
  cleanup. Load for planning, implementation, review, or publication in any repo kind.
---

# Develop a feature

Own the outcome as both technical and UX lead. Use four stages: **draft locally → review the
agreed direction → complete tests for approved scope → tighten and hand off**. This skill owns
stage timing; specialist skills own architecture, implementation, testing conventions, issue
metadata, Git, and review mechanics. Load [prefer-small-solutions](../prefer-small-solutions/SKILL.md)
throughout, plus the skills for the affected layers.

## Load guidance for the current decision

Load each applicable skill once and follow its reference routing. A linked skill is required when
its stated condition applies; a mention of a later stage is not a command to load that stage's
entire runbook now. Reuse already-read guidance until it changes or is no longer available in
context. Before entering a new stage, load its owner and the references needed for that operation.

- This skill owns agreement, stage transitions, and test timing.
- `prefer-small-solutions` owns simplicity; `choose-dependency` owns package selection.
- `plan-feature` and `plan-ui-design` own technical and product design, respectively.
- Language, architecture, and test skills own their contracts and conventions.
- `git-conventions`, `open-pull-request`, `monitor-ci`, and `resolve-pr-feedback` own their
  operational procedures; `use-a-novel-cli` owns local command routing.

Follow each applicable rule at its owning boundary. Simplicity guidance never relaxes a specialist
constraint; uncertain scope stays open under the existing approval rules.

## Choose the starting stage

Default to a local draft when the solution or user experience is unclear. A clear brief or existing
agreement can skip exploration; a small, unambiguous fix, documentation change, or bounded technical
task can combine stages in one pass. State a consequential shortcut briefly. Technical complexity
alone does not justify skipping discussion of uncertain scope, architecture, or behavior.

Reuse agreement and authorization already given; do not ask the developer to approve the same
decision again. Proportionate validation and a final cleanup pass still apply to the shorter path.
An explicit request to publish or create an issue can advance that action within its stated scope.

## 1. Draft locally and lead the discussion

- Read the affected path, existing behavior, and constraints. Use `plan-feature` for non-trivial
  technical design and `plan-ui-design` for material UI decisions while building the draft.
- Edit all local layers needed to make the proposal concrete. Run the application or affected
  service locally; use the package's workbench for a library. Add and run Storybook when rendered
  UI needs review, following `write-frontend`. Exercise the proposed behavior yourself and provide
  verified direct links. Load `use-a-novel-cli` for local runtime commands.
- Keep the draft local: no push, remote issue creation, or PR until the direction is agreed,
  unless the developer explicitly requested that action earlier. Local branches and checkpoint
  commits are fine. Uncommitted work, temporary scaffolding, and incomplete test coverage are
  expected in your own draft; preserve useful work and other contributors' changes.
- Write only tests needed now to reproduce a bug, protect a risky invariant, or resolve a design
  uncertainty. Run focused existing checks and smoke tests to support the discussion. Preserve
  existing tests and CI requirements; defer building the full relevant regression suite to stage 3.
  Security, data integrity, trust boundaries, and architectural constraints apply from the start.
- Propose solutions and improvements proactively. Explain the recommendation, its tradeoffs, and
  what the developer should inspect; do not simply implement the first wording of the request.
  Invite feedback throughout the iterations and keep meaningful decisions visible.

### Make the developer able to review

Learn the developer's technical and UX experience from the conversation; ask a focused question
when that would change the explanation. Do not infer competence from their job title or bury them
in specialist vocabulary. Use concrete behavior, examples, and consequences for a UX-focused
reviewer; provide architecture, contracts, and failure details when useful to a technical reviewer.
Adapt the depth without hiding constraints or deciding product preferences on their behalf.

Each draft handoff should give enough guidance to assess the proposal:

- What changed, why it helps, and which parts remain provisional or mocked.
- Direct local app/Storybook routes, setup or sample data, and steps to try.
- Expected outcomes and the most relevant checks: flow and copy, keyboard/focus, narrow layouts,
  loading/error/recovery states, permissions, or data behavior, according to the change.
- Scope and ownership boundaries, dependencies, compatibility or operational limits, and what the
  proposal cannot yet do. Explain where client, server, and shared components own behavior when it
  affects the decision.
- The decisions needed, your recommendation and tradeoffs, and any unresolved risks.

Use discussion to converge on a working draft, not a demand for abstract approval. A decision
requiring expertise the developer lacks remains explicitly open; silence or uncertainty is not
approval. Explain it at an appropriate level, recommend a path, and identify the expertise or owner
needed. During local drafting retain it in the handoff; once issues exist, carry it into a discussion
on the relevant issue with its alternatives, consequences, and the work it blocks. Agreement on the
draft direction may coexist with those open questions; it does not approve the unresolved scope.

## 2. Record the agreed direction and review issues and PRs together

After agreement on the draft, use `plan-feature` to create or update the appropriate GitHub issues,
with the existing type, hierarchy, dependency, board, and metadata conventions. Record the outcome,
scope, acceptance criteria, and decisions established by the draft. Keep unresolved questions in
issue discussions and reflect their impact on scope in the issue body.

Draft PRs may now be opened with `open-pull-request`. Link the issues, describe the provisional
parts, and identify coverage deferred to stage 3. Keep them draft while issue approval, test
completion, or cleanup remains outstanding. Existing CI checks still run and must not be weakened.

Use `resolve-pr-feedback` for both issue and PR discussions. During active work, inspect both when
resuming, after a meaningful revision or push, during CI waits, and before advancing stages or
handing off. Read issue comments as well as PR conversation comments, reviews, and inline threads;
neither surface substitutes for the other. Handle feedback on draft PRs within the authorized task.
Do not create a background monitor unless requested.

## 3. Confirm approved scope, then complete test coverage

Before expanding the test suite, verify that the relevant issues' scope and acceptance criteria
have been approved and blocking design questions resolved. Identify the actual agreement in the
discussion or existing authorized brief; issue closure, a board status, green CI, or approval of a
different scope is insufficient. An approved issue can remain open until its PR lands. For a clear
task that legitimately skipped issues, use its agreed brief as the scope reference.

Compare the implementation and PR diff with that scope. Resolve omissions or scope drift first;
material new design questions return to discussion. Continue independent approved work when a
decision blocks only part of the task.

Now add or update the full **relevant** regression suite: meaningful behavior, boundary and error
cases, security invariants, integration contracts, and critical journeys at the closest truthful
layer. Review coverage reports for important gaps. Do not chase 100%, duplicate assertions across
layers, or test implementation details and trivial glue to inflate a number.

For client-side work, load `write-frontend-tests`: create or update all affected Playwright journey
and visual tests, run them, and inspect screenshot comparisons against the agreed UI. Review actual,
expected, and diff images; investigate unexpected changes before accepting a baseline. A new screen
needs a reviewed initial baseline. Complete applicable unit, component, accessibility, integration,
lint, type, and build checks using the repository tooling. Keep real verification limits explicit;
do not call an unrun check passed.

## 4. Tighten the code and hand off

Always finish with an intentional review of the whole affected path using `prefer-small-solutions`.
Remove draft scaffolding, dead code, duplication, redundant state and conversions, unnecessary
abstractions, and obsolete fixtures. Prefer fewer maintained lines after normal formatting when
behavior and clarity are equal. Preserve strict code practices, security, architecture, contracts,
and useful tests. Make future changes easy through clear ownership and idiomatic interfaces;
avoid speculative extension points.

Keep cleanup within the agreed scope. If it changes observable behavior or the design, return that
decision to discussion and update the tests. Re-run checks affected by cleanup, inspect the final
diff, and recheck issue and PR feedback before declaring readiness.

Mark a PR ready only after applicable approval, coverage, visual review, and cleanup are complete.
Use `monitor-ci` through its required conclusion. Hand off the verified result, relevant local
review routes, developer review guidance, and any open issue discussions or limitations. Publishing
a proposal and passing CI do not authorize merging or deploying it.

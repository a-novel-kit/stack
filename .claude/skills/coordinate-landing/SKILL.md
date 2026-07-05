---
name: coordinate-landing
description: >
  The vocabulary, invariants, and operator runbook for the cross-repo **landing saga** — how a
  multi-repo Epic lands atomically, recovers from a partial landing, rolls back, and releases across
  the a-novel / a-novel-kit governance automation (the `a-novel-kit/workflows` actions: merge-gate,
  epic-freeze / partial-landing detector, epic-rollback, release-train, plus the reconcile sweep and
  the AGENT_KILL_SWITCH halt). Load this skill whenever a change spans several repos under one Epic and
  you need to reason about how it lands or ships together — driving or rehearsing a release train,
  interpreting a held or frozen PR, recovering a partial landing, rolling an Epic back, or halting the
  automation during an incident. It owns the shared saga vocabulary (freeze it here, use it everywhere)
  and the **Epic Atomicity Rule**; it defers the git/version mechanics of a staged rollout
  (expand→contract, publish-before-rollout, `go.mod` pins) to `manage-versions`, and the per-repo
  release mechanics to `prepare-release` / the release workflow. Pairs with `plan-feature` (which
  creates the Epic), `implement-feature` (per-repo branches), `manage-versions` (cross-repo staging +
  the landing-failed runbook), and `resolve-pr-feedback` (the held/frozen-PR conversation).
---

# The landing saga

A single feature often spans several repos — a `golib` change plus the two services that consume it,
or a proto change and every client. Under the a-novel model those repos are **independently versioned
and independently merged**, so "land them together" is not free: it is a **saga** — a coordinated
sequence with compensating actions when a step fails. This skill is the map of that saga: what the
pieces are called, the one invariant they all serve, and the operator procedures when something needs
a human.

The saga is enforced by the `a-novel-kit/workflows` governance actions, driven by two triggers: the
per-PR / per-merge-group events, and a **reconcile sweep** that runs every ~15 minutes as a
level-triggered, self-healing floor. Nothing here is a bespoke distributed-transaction engine — it is
a small set of GitHub-native mechanisms (a required check, the merge queue, check-runs, a dispatch
action) composed to make a multi-repo landing behave like one.

---

## The Epic Atomicity Rule (INV-1)

> **All of an Epic's member PRs land together, or none of them do.**

An Epic is a planning issue; its member PRs each carry the `epic:<N>` label. INV-1 is the invariant
the whole saga exists to protect: a consumer must never merge while the dependency it needs is still
unmerged (or vice-versa), because that leaves a repo un-buildable. Every mechanism below is either an
_enforcer_ of INV-1 (merge-gate, the merge queue) or a _compensator_ for a violation of it
(epic-freeze recovers, epic-rollback undoes).

INV-1 is what makes the release train safe to run in **any order**: once an Epic has landed
atomically, its repos are mutually consistent, so releasing them is order-independent. Cross-**Epic**
ordering — Epic B depends on Epic A's release — is _not_ INV-1's job; that is `manage-versions`.

---

## Vocabulary — freeze it here, use it everywhere

Use these terms identically in code, PRs, issues, and conversation. No synonyms; never one word for
two things.

| Term                                       | Meaning                                                                                                                                                                                                                                                            |
| ------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| **Epic**                                   | The planning issue grouping a multi-repo change; its number is `N`.                                                                                                                                                                                                |
| **`epic:<N>` membership**                  | The label binding a PR to Epic N. Author-role-gated (only a maintainer can add it), so membership is trusted.                                                                                                                                                      |
| **Atomic landing**                         | All member PRs merging together — INV-1 satisfied.                                                                                                                                                                                                                 |
| **merge-gate**                             | The required status check that **holds** an `epic:<N>` PR until the whole member set is ready + approved, then lets the merge queue land them together. A standalone (unlabelled) PR fast-paths to pass. On engagement of the halt it posts `failure` on every PR. |
| **merge queue**                            | GitHub's native queue. The merge-gate re-evaluates over each frozen `gh-readonly-queue/...` head so the set greens and commits together.                                                                                                                           |
| **Partial landing**                        | An Epic where some members merged but others did not — an INV-1 violation (e.g. a member left the queue and did not re-enter).                                                                                                                                     |
| **epic-freeze / partial-landing detector** | The action + sweep pass that detects a partial landing and **freezes** every surviving sibling (posts `failure` on their heads + dequeues live groups) so no further member lands.                                                                                 |
| **Grace window**                           | The 45-minute interval (3× the 15-min sweep) a stray sibling has to re-enter the queue before the freeze trips — absorbs normal queue churn.                                                                                                                       |
| **Roll-forward**                           | Re-enqueuing a _landable_ stray within grace (enable auto-merge). Recovery **forward**, not a rollback — the preferred repair.                                                                                                                                     |
| **epic-rollback**                          | The human-triggered, admin-gated, VCS-layer compensator: reconstruct the merged `epic:<N>` ledger, `git revert` each squash newest-first, group the reverts under a **fresh** rollback-Epic, and land them in reverse through the unchanged merge-gate.            |
| **Release train**                          | One admin dispatch that releases every repo an Epic landed in — derive each repo's bump, drive its `release.yaml`, record the tag.                                                                                                                                 |
| **Receipt**                                | The tag a repo's release cut, recorded on the Epic. The release train's output **and** its idempotent-resume ledger.                                                                                                                                               |
| **AGENT_KILL_SWITCH**                      | The org-wide fail-safe emergency halt (an org variable).                                                                                                                                                                                                           |
| **`automation:paused`**                    | The per-Epic pause label on the Epic issue — holds THAT Epic's automation, best-effort.                                                                                                                                                                            |
| **Blast-cap**                              | The per-Epic distinct-repo cap that trips a loud alert (freeze) or a hard abort (rollback) when an Epic's fan-out is suspiciously wide.                                                                                                                            |
| **Saga**                                   | The whole coordinated land → detect → recover → release → archive lifecycle.                                                                                                                                                                                       |
| **Coordinator**                            | The set of governance actions that drive the saga (the reconcile sweep + the per-event workflows).                                                                                                                                                                 |

---

## The saga lifecycle

```
author → LAND (merge-gate + queue, atomic) → DETECT (reconcile sweep)
                                                 ├─ whole?  → RELEASE (release train) → ARCHIVE
                                                 └─ partial → RECOVER (freeze + roll-forward within grace)
                                                                └─ unrecoverable → ROLL BACK (human-approved)
```

1. **Author.** A maintainer labels each member PR `epic:<N>`.
2. **Land.** The merge-gate holds each member until the whole set is ready + approved, then the merge
   queue lands them together — INV-1 satisfied atomically. A standalone PR is unaffected.
3. **Detect.** The reconcile sweep (every ~15 min, level-triggered) re-derives each open Epic's state
   from live GitHub truth — it never trusts a stored flag, so it self-heals after any missed webhook.
4. **Recover.** On a detected partial landing it **freezes** the surviving siblings and **rolls
   forward** any landable stray within the grace window. Recovery is forward-first; a frozen sibling
   holds (its required check goes red) until the Epic is whole again or a human intervenes.
5. **Release.** Once whole, the **release train** releases the Epic's repos from one admin dispatch and
   records a tag receipt per repo.
6. **Archive.** Each repo's `release.yaml` clears its awaiting-release board items after it ships.

---

## Operator runbook

Every entry is admin-gated at the point of action; the destructive ones (rollback) additionally force
a human approval. All the write actions honor `AGENT_KILL_SWITCH` and `automation:paused`.

### Halt everything (incident brake)

Set the **`AGENT_KILL_SWITCH`** org variable (in the affected org) to any value that is **not** an
off-token — e.g. `on`. Effect, org-wide and immediate on the next event/sweep:

- **merge-gate posts `failure` on every PR** — nothing merges (the halt rests on the gate holding
  _every_ PR, so it can't fast-path a standalone).
- Every board writer, auto-merge arm, freeze poster, and rollback **no-ops or refuses**.

**Lift** by setting the value back to an off-token — canonically **`off`** (a created org variable
cannot be empty, so `off` is the resting value; unset behaves the same but isn't discoverable). The
switch is **fail-safe**: a fat-fingered or garbage value halts, not runs. It is a cooperative in-action
flag, not a security boundary — if the incident is a compromised App, revoke the installation / rotate
`AGENT_BOT_PRIVATE_KEY` instead.

### Pause one Epic

Add the **`automation:paused`** label to the Epic issue. Holds THAT Epic's automation (merge-gate holds
its members; the detector and rollback skip it) while other Epics keep moving. **Best-effort** — a
label-read blip fails open (the pause is skipped that pass); it is an operator convenience, not the
hard halt. Remove the label to resume.

### Recover a partial landing

Usually **automatic**: the sweep freezes the surviving siblings and rolls a landable stray forward
within grace. Intervene only when:

- **A stray can't land** (merge conflict, failing checks). The freeze holds the whole surviving set
  (their required check is red) — fix the stray and let it re-enter the queue, or escalate to a
  rollback if the landed subset is genuinely broken.
- **The freeze is very wide** (blast-cap tripwire fired: a loud red sweep). A freeze spanning more
  distinct repos than the cap is almost certainly a mis-scoped Epic — the freeze still posts (fail
  toward freezing), but investigate the Epic's membership before doing anything else.

### Roll back an Epic (INV-1 genuinely violated)

Dispatch **`epic-rollback`** for Epic N. It is admin-only, `dry_run`-default, and typed-confirm
(`revert-epic-<N>`). Always **dry-run first** — it prints the reconstructed ledger + the planned
per-repo reverts and does no writes. Then run live:

- It reconstructs the merged `epic:<N>` ledger from GitHub (REST-authoritative squash SHAs), git-reverts
  each squash **newest-first**, opens one revert PR per repo, and groups them under a **fresh**
  rollback-Epic `epic:<M>` through the unchanged merge-gate.
- The App authored the reverts, and **GitHub 422s a self-approval**, so the wave **PARKS pending a
  human approval** — this is forced four-eyes on a destructive op, not a choice. Review + approve each
  revert PR; the gate then greens and the wave lands in reverse, atomically.
- A revert conflict opens a **HELD draft** placeholder that holds the whole wave (no partial rollback);
  finish it by hand. The blast-cap **aborts loud** above the cap (a rollback that wide is opt-in — fail
  toward NOT reverting).

A rollback does not un-merge history — each revert is a new forward commit, so a mistaken rollback is
itself revertible.

### Release an Epic (the release train)

Dispatch **`release-train`** for Epic N (admin-gated, `dry_run`-default). Rehearse first (the dry run
dispatches each repo's `release.yaml` with `dry_run=true` and cuts nothing), then run live:

- It reconstructs the Epic's landed repos, derives each repo's semver **bump** from its
  conventional-commit range (fix→patch, feat→minor, `!`/`BREAKING CHANGE`→major), and drives each
  repo's own `release.yaml` — any order (INV-1 makes them independent).
- It records a **receipt** (the cut tag) per repo on the Epic. **Idempotent resume**: a re-dispatch
  skips every repo already shipped for this Epic (derived from _live tags_, never the receipt), so a
  partial train re-cuts only the unshipped.
- A repo parked at the protected `release` environment approval gate is **pending**, not failed —
  approve the run, then re-dispatch to record the receipt. A **botched cut** (tag pushed, Release
  missing) is flagged loudly for manual repair, never silently skipped.

---

## Version coordination — the two rules `manage-versions` owns

The saga lands and releases a set of repos; keeping them _version_-compatible across that release is
`manage-versions`' domain. Two rules from there govern how a saga is shaped:

- **Publish-before-rollout.** When one repo's change is needed by another, the **dependency PR merges
  and releases FIRST**, and the consumer re-pins to the released tag before _its_ PR merges. Within a
  single Epic the merge-gate lands the set together, but the _release_ order across repos still honors
  this: release the dependency, re-pin, then release the consumer (the release train derives bumps
  per repo; cross-repo re-pins are `manage-versions`').
- **Expand→contract** (staged breaking change). A breaking change never ships in one step: **expand**
  — add the new path alongside the old, non-breaking, and release; migrate every consumer; **contract**
  — remove the old path in a later release. Drafted ahead as `blocked-by` sub-issues. This is how a
  cross-repo breaking change stays landable atomically at every step: no single merge breaks a
  consumer, so INV-1 holds throughout.

See `manage-versions` for the mechanics (exact `go.mod` pins, pseudo-version development against an
unreleased dep, the tag-push release, Renovate re-bumps) and its **landing-failed runbook** for the
version-recovery decision tree.

---

## How this composes

```
plan-feature (creates the Epic + Task sub-issues)
   └─ implement-feature (per-repo branches, one PR per Task, each labelled epic:<N>)
        └─ THE SAGA (this skill): merge-gate lands the set atomically (INV-1)
             ├─ partial landing → epic-freeze recovers, or epic-rollback undoes (human-approved)
             ├─ manage-versions: publish-before-rollout · expand→contract · landing-failed runbook
             └─ release-train releases the Epic's repos → archive
```

- **Enforcer skills:** none of the saga is bespoke — it is the merge queue + required checks +
  check-runs + a dispatch action. Read the `a-novel-kit/workflows` actions for the ground truth.
- **`resolve-pr-feedback`** for the conversation on a **held or frozen** PR — explain _why_ it is held
  (waiting on its Epic set / frozen by a partial landing), not just _that_ it is.
- **`manage-versions`** for anything version-shaped in the rollout.

---

## Principles

- **INV-1 is the north star.** Every mechanism enforces "land together or not at all," or compensates
  for a violation. When in doubt about a saga decision, ask which side of INV-1 it serves.
- **Recover forward before you roll back.** A frozen partial landing that a roll-forward can finish is
  cheaper and safer than a revert wave. Rollback is the last resort, and it is human-approved.
- **The sweep is the floor, level-triggered.** State is re-derived from live GitHub truth every pass,
  never trusted from a stored flag — so a missed webhook self-heals. Don't add a stateful shortcut.
- **Halt is fail-safe and cooperative.** The kill-switch fails toward halting; it is an in-action flag,
  not a security control. Revoke the App for a real compromise.
- **Freeze constant, use it consistently.** The vocabulary above is the contract; a reused name is a
  future bug.

---
name: resolve-pr-feedback
description: >
  Inspect PRs and linked issues, evaluate review feedback, reply, resolve settled threads, and
  request re-review. Load before reading a new review or working on issue discussions.
---

# Resolve PR Feedback

Load [develop-feature](../develop-feature/SKILL.md) to determine whether this work is in issue/draft
review or final verification. During active development, survey linked issues and PRs together when
resuming, after meaningful revisions or pushes, during CI waits, and before stage transitions or
handoff. Read issue bodies/comments as well as all PR feedback surfaces below. Keep decisions needing
expertise the developer lacks open on the relevant issue, with a recommendation and their impact on
scope; do not mistake an unanswered discussion for approval.

Read this skill before acting on a review, even when a proposed fix looks obvious. A status check
and an instruction to address feedback both start with the survey; only authorized feedback work
continues through classification, fixes, replies, and resolution. Settled threads require both a
reply and resolution, using the identity and verification rules in the closing-the-loop reference.

It **also** governs discussion under an **issue** — chiefly the planning issues `plan-feature`
produces, where the human and the agent converse in comments while the body holds the agreed plan.
Phases 1–5 are written for PRs. When the work is an issue, start at
[Issue discussions](references/issues.md#issue-discussions-planning--triage), which maps the same posture onto issues:
flat comments, no review threads, no resolution state.

---

## Choose the review path

- PR status or review request: read [the survey procedure](references/survey.md) first.
  Status-only work ends after reporting evidence; it does not authorize edits or replies.
- Addressing PR feedback: survey, classify, and fix using the phases here. Before replying,
  resolving, or requesting re-review, read [closing the loop](references/close-loop.md).
- Issue discussion: read [issue discussions](references/issues.md); issue comments have no PR
  thread-resolution state. `plan-feature` owns changes to the planning issue body.

Reply and resolve remain one operation for settled PR threads. Use the authorized bot-comment
path, preserve unresolved decisions, and never infer permission to communicate from a status query.

## Guiding principle

A pull request — or a planning issue — is a **conversation**, not a checklist. Reviewers and
collaborators — human or bot — can be right, wrong, unclear, or working from a partial picture of the
change, so apply judgment. The failure modes are symmetric: silently overriding a valid concern
erodes trust; blindly applying an incorrect suggestion ships a regression. The remedy for both is the
same — speak on the thread so the reviewer sees your reasoning and can push back.

Two rules anchor the loop; the rest of this skill is their mechanics:

1. **Clear-cut → reply _and_ resolve, in the same action.** A thread you have decisively answered —
   accepted (and pushed the fix) or declined (with a defensible reason) — is settled. Reply, then
   resolve it right then; the resolve is not a later step. A "fixed in `<sha>`" reply that leaves the
   thread **open** is the defect this rule exists to stop — it forces the reviewer to re-read work
   that is already done and to close threads you should have closed. An open thread must mean a real
   decision is still pending, never "answered but not yet resolved." The reviewer can re-open with new
   information.
2. **Genuinely unclear or worth discussing → reply with a specific question, leave open.**
   Silence is the worst option; acting on partial understanding is the second-worst.

If you partially accepted, took a different direction, or bundled the fix with adjacent changes, the
**reply explains the deviation** before resolution. Quiet, after-the-fact re-interpretation erodes
trust. Larger deviations where the reviewer might prefer the original suggestion fall under rule 2 —
leave open with an explicit "OK with this approach?" question.

**Reply style: rationale-dense, zero filler.** Thread replies may be more technical than a PR body,
but the same economy applies — lead with the reason, cite the evidence (a SHA, a doc, a measured
fact), and stop. Never restate what the reviewer or the diff already shows.

---

## Phase 2: Classify each unresolved thread

For every unresolved thread, fit it into one of four buckets. Read the full thread (including prior
replies), read the code it points at, and reason about each thread independently — never in bulk.

| Bucket                    | When to use                                                                                                                                                                                                      |
| ------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **Accept**                | The comment is correct and actionable; a straightforward change implements its intent.                                                                                                                           |
| **Accept-with-deviation** | The core concern is valid but the specific suggestion is wrong, partial, or better served by a different approach.                                                                                               |
| **Decline**               | The comment misunderstands the change, conflicts with a repo guideline (CLAUDE.md, a skill file, a documented decision), would reintroduce a security or correctness regression, or is out of scope for this PR. |
| **Unsure**                | You cannot confidently place the comment into one of the above.                                                                                                                                                  |

Signals that push toward **decline** specifically:

- Accepting would violate a rule in `.agents/skills/*/SKILL.md` — the skill file is the authoritative
  source, not the comment.
- The comment asks to re-expose something deliberately hidden for security (e.g., error strings on an
  unauthenticated endpoint). That is a decline, not a conversation.
- The comment asks for feature work outside this PR's layer or scope. The answer is usually "decline
  for this PR, file a follow-up."

Signals that push toward **unsure**:

- The comment assumes context you do not see in the PR (an incident, a prior decision, code in
  another repo).
- Two readings of the comment lead to different fixes, and the reviewer did not pick one.
- The comment is terse ("this won't work") with no specifics.

**Bots vs humans.** Copilot and similar bots do not re-engage on thread replies. Still classify their
comments with the same rigor — bots miss context routinely, and blanket acceptance is how insecure or
incorrect changes land. Weight your reply toward the **human** reviewer who will read the thread
later.

A bot repeating the same claim across several comments is **not** independent evidence of
correctness; it is one opinion with a megaphone. Verify the underlying fact once — official docs, a
spec, or an empirical test (often one `gh api` call away) — and cite that source in your reply.
Treating repetition as confirmation is how a confidently wrong bot lands an error in the codebase.

Bots are especially prone to confident errors about **external specs** — API endpoint paths, header
names, status code semantics, protocol details. When a comment asserts a factual claim about a
third-party system, check that system's authoritative source before arguing from plausibility.

---

## Phase 3: Act on each thread

Work the cheap replies first (decline, unsure) before the code changes (accept,
accept-with-deviation). That gets the conversation moving while you focus on the fixes.

### 3.1 Decline

Reply once, inline on the thread, with:

- A one-sentence reason.
- A pointer to the authoritative source when one exists (skill file, CLAUDE.md section,
  linked incident, standard).
- An invitation to push back if more context would change the assessment.

Resolve the thread. The reply is the closure; if the reviewer brings new information, they can
re-open and you re-enter Phase 2 on the same thread.

```bash
a-novel core bot-comment <org> <repo> <number> --reply-to <comment-id> \
  --body "$(cat <<'EOF'
<one-sentence reason>

Per <.agents/skills/...-SKILL.md section / CLAUDE.md anchor / linked source>.
Happy to revisit if I've missed context here.
EOF
)"
```

### 3.2 Unsure — start a discussion

Reply with a **specific question**, not a generic "what do you mean?". Quote the part you are unsure
about and lay out the interpretations you see. That respects the reviewer's time and anchors the next
round.

Wait for a reply before acting. Re-enter Phase 2 once the reviewer responds; multi-round exchanges on
a single thread are normal.

### 3.3 Accept-with-deviation

Apply the fix in the direction that actually makes sense (Phase 4). After pushing, reply
**explaining the deviation** before any resolution:

- "Took the core suggestion but scoped it to X instead of Y — Y would also touch the
  Z layer, which is out of scope for this branch."
- "Applied the spirit of the comment via <alternative> — the literal suggestion would
  not work because <reason>."

How far you deviated decides whether to resolve. Resolve a small, well-explained deviation — the
reply is the audit trail. A larger deviation where the reviewer might prefer the original suggestion
stays open with an explicit "OK with this approach?" question.

### 3.4 Accept

Apply the fix (Phase 4). After pushing, reply with a one-liner:

- `Fixed in <short-sha>.`
- Optional: one sentence on anything non-obvious about how you applied it.

Resolve the thread.

---

## Phase 4: Apply fixes and push

### 4.1 Commit per `git-conventions`

One logical unit per commit. Pick the commit type that matches the change itself, not the reviewer's
category:

- Reviewer asked for a test → `test(<scope>): ...`
- Reviewer asked for a doc or description clarification → `docs(<scope>): ...`
- Reviewer flagged a real bug → `fix(<scope>): ...`
- Reviewer asked for a rename or internal reshape → `refactor(<scope>): ...`

Cite the review in the commit body so the log is self-documenting:

```
Addresses <reviewer-login> review feedback on #<PR-number>.
```

**Never amend a pushed commit to address review.** The review is anchored to the old SHA; amending
rewrites shared history and strands the review thread's context. Always create new commits. (A hard
rule from `git-conventions`.)

### 4.2 Run the narrowest test target

After each logical change, before pushing, apply the current `develop-feature` stage. Use focused
checks during issue/draft review; complete the full relevant coverage only after scope approval.
For final verification, use the affected suites:

- Go changes (internal or `pkg/go`) → `a-novel test --type=go -y`
- `pkg/js` changes → `a-novel test --type=pnpm -y`

Full mapping in the `use-a-novel-cli` skill (auto-loaded). Never push a red tree.

### 4.3 Push

```bash
git push
```

If the fix required a rebase, use `git push --force-with-lease`. Never plain `--force`, never
force-push to `master`.

---

## Common pitfalls

- **Silent resolution without a reply.** Always pair a resolve with a reply naming the SHA (5.1).
- **Blanket acceptance of bot comments**, or **treating repeated bot claims as confirmation.**
  Classify every comment, and verify the underlying fact once against an authoritative source
  (Phase 2).
- **Accepting a reviewer's spec claim without checking the spec.** Verify an asserted API shape,
  endpoint path, header name, or protocol detail against upstream docs — or one empirical call —
  before editing. Plausibility is not evidence.
- **Replying with the wrong mode.** A top-level comment does not thread with an inline review
  comment. To reply on a review thread, pass `--reply-to <comment-id>` to `a-novel core bot-comment`;
  a bare comment (no `--reply-to`) posts at the top level.
- **Commenting as yourself.** Every PR/issue/review comment goes through `a-novel core
bot-comment` so it attributes to `<app-slug>[bot]`. Bare `gh pr comment` / `gh api …
comments` posts as your user account — only reads use plain `gh`.
- **Leaving clear-cut threads open.** Resolve a thread you have decisively answered; reserve the open
  state for genuinely-pending decisions (rule 1).
- **Amending or force-pushing to address review.** Review comments are anchored to the SHA that was
  reviewed. Rewriting strands them. New commits, every time.
- **Re-requesting review too early.** Wait for all pushes + green CI + posted declines.
- **Acting while unsure.** A specific question is the only correct first move. A best-guess fix
  explained afterwards on the thread wastes a round.
- **Mixing types in one commit to bundle a batch of review fixes.** Each review-driven commit is
  still subject to `git-conventions` — a `test` and a `docs` fix are two commits, even from the same
  review.

---

## Hand-offs

- **From `open-pull-request`** — once a PR is open and reviewers start commenting, the push-and-open
  flow hands off here to assess CI, review threads, and reviewer status, then work the feedback.
- **With `plan-feature`** — `plan-feature` owns the planning-issue **body** (the agreed plan);
  this skill owns the **comment loop** around it (posting open questions, answering the human's
  replies, folding decisions back into the body). See [Issue discussions](references/issues.md#issue-discussions-planning--triage).
- **To `monitor-ci`** — for failing checks that need flake-vs-real classification or a retry loop.
  When CI agrees with a reviewer (same root cause), fold the fix into the thread response rather than
  pushing twice.
- **To `git-conventions`** — every review-driven commit. No exceptions.
- **To the layer-specific skills** — `write-go`, `write-go-service` (or `write-go-kit` for a-novel-kit repos), `write-go-tests`, `write-openapi`,
  `write-js-package`, etc. Phase 4 writes code; those skills govern _how_.
- **Close with the session recap table** (memory `session-recap-table`) — after working a PR's
  feedback, end the turn with a table linking every PR/issue still outstanding this session (this
  PR's remaining threads, its CI, anything else in flight), so the operator can jump to each.

---

## Quick reference

| Situation                          | Command                                                                                                             |
| ---------------------------------- | ------------------------------------------------------------------------------------------------------------------- |
| PR envelope                        | `gh pr view <n> --json number,title,state,isDraft,mergeable,reviewDecision,baseRefName,headRefName,reviews,commits` |
| Inline review comments             | `gh api repos/<o>/<r>/pulls/<n>/comments`                                                                           |
| Top-level PR comments              | `gh api repos/<o>/<r>/issues/<n>/comments`                                                                          |
| Review envelopes                   | `gh api repos/<o>/<r>/pulls/<n>/reviews`                                                                            |
| Thread resolution state (node IDs) | GraphQL `reviewThreads` query (Phase 1.3)                                                                           |
| CI status                          | `gh pr checks <n>`                                                                                                  |
| Reply on a review thread (bot)     | `a-novel core bot-comment <o> <r> <n> --reply-to <cid> --body "..."`                                                |
| Resolve a thread                   | GraphQL `resolveReviewThread` mutation (Phase 5.2)                                                                  |
| Start a new inline thread          | not a bot action — post a top-level bot comment naming `file:line` (see "Starting your own thread")                 |
| Comment on a PR or issue (bot)     | `a-novel core bot-comment <o> <r> <n> --body "..."`                                                                 |
| Re-request review                  | `gh api .../pulls/<n>/requested_reviewers -X POST -F 'reviewers[]=<login>'`                                         |
| **Issue** envelope                 | `gh issue view <n> --repo <o>/<r> --json number,state,title,labels,assignees,body,comments`                         |
| **Issue** comment stream           | `gh api repos/<o>/<r>/issues/<n>/comments`                                                                          |
| Comment on an issue (bot)          | `a-novel core bot-comment <o> <r> <n> --body "..."` (flat — no `--reply-to`)                                        |
| Edit the plan body (operator)      | `gh issue edit <n> --repo <o>/<r> --body-file <file>`                                                               |
| Close an issue                     | `gh issue close <n> --repo <o>/<r> --reason "not planned"` (or `completed`)                                         |

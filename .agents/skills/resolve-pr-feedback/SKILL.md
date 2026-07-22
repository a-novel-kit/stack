---
name: resolve-pr-feedback
description: >
  Survey a pull request's state (CI, review threads, reviewer status) and work through reviewer
  feedback on any repo in the a-novel / a-novel-kit orgs. Use it when checking on an open PR, reading
  Copilot or human review comments, replying on or resolving a thread, re-requesting review, or
  answering comments under an issue. Pairs with plan-feature, which owns the issue body.
---

# Resolve PR Feedback

This skill governs how Claude surveys a pull request's state and works through reviewer feedback. It
runs as a passive read ("check PR 532") and as an active workflow ("address the comments on PR 532").
Both modes share Phase 1; only the resolve workflow continues through Phases 2–5.

It **also** governs discussion under an **issue** — chiefly the planning issues `plan-feature`
produces, where the human and the agent converse in comments while the body holds the agreed plan.
Phases 1–5 are written for PRs. When the work is an issue, start at
[Issue discussions](#issue-discussions-planning--triage), which maps the same posture onto issues:
flat comments, no review threads, no resolution state.

---

## Guiding principle

A pull request — or a planning issue — is a **conversation**, not a checklist. Reviewers and
collaborators — human or bot — can be right, wrong, unclear, or working from a partial picture of the
change, so apply judgment. The failure modes are symmetric: silently overriding a valid concern
erodes trust; blindly applying an incorrect suggestion ships a regression. The remedy for both is the
same — speak on the thread so the reviewer sees your reasoning and can push back.

Two rules anchor the loop; the rest of this skill is their mechanics:

1. **Clear-cut → resolve.** A thread you have decisively answered — accepted (and pushed the fix) or
   declined (with a defensible reason) — is settled. Resolve it. An open thread signals a real
   decision pending, not a pending audit trail. The reviewer can re-open with new information.
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

## Phase 1: Survey PR state

Callable on its own. When the user asks only to "check", "look at", or "monitor" a PR, run this
phase, report back, and stop. Do not act without an explicit go-ahead.

### 1.1 Read the PR envelope

```bash
gh pr view <number> --json \
  number,state,isDraft,mergeable,reviewDecision,baseRefName,headRefName,title,commits,reviews
```

Fields that matter:

- **state**: OPEN / CLOSED / MERGED. Never act on non-OPEN PRs without confirmation — reopening a
  closed discussion is a different kind of decision.
- **isDraft**: draft PRs rarely need the full review-cycle. If the reviewer left comments anyway,
  confirm with the user whether they want them addressed now.
- **reviewDecision**: APPROVED / CHANGES_REQUESTED / REVIEW_REQUIRED. Shapes Phase 5.
- **baseRefName** / **headRefName**: land fixes as new commits on `headRefName`. Force-push with
  `--force-with-lease` only if a rebase was required.

### 1.2 Read review comments

GitHub splits review feedback across three endpoints, and a comment in one does not show up in the
others. Read all three when surveying.

**Inline review comments** (anchored to `file:line`):

```bash
gh api repos/<owner>/<repo>/pulls/<number>/comments
```

Each record has `id`, `path`, `line`, `body`, `user.login`, `in_reply_to_id`, `commit_id`. The `id`
here is the REST comment ID — the GraphQL thread node ID used for resolution comes from 1.3.

**Top-level PR comments** (the "Conversation" tab, not anchored to code):

```bash
gh api repos/<owner>/<repo>/issues/<number>/comments
```

**Review envelopes** (APPROVED / CHANGES_REQUESTED / COMMENTED wrappers that group
inline comments):

```bash
gh api repos/<owner>/<repo>/pulls/<number>/reviews
```

A single review envelope can contain zero or many inline comments and a top-level body.

### 1.3 Read thread resolution state

The REST API does not expose whether a review thread is resolved. Use GraphQL:

```bash
gh api graphql -f query='
query($owner:String!, $repo:String!, $number:Int!, $threadCursor:String) {
  repository(owner:$owner, name:$repo) {
    pullRequest(number:$number) {
      reviewThreads(first:100, after:$threadCursor) {
        pageInfo { hasNextPage endCursor }
        nodes {
          id
          isResolved
          isOutdated
          comments(first:50) {
            pageInfo { hasNextPage endCursor }
            nodes { databaseId author{login} path line body url }
          }
        }
      }
    }
  }
}' -F owner=<owner> -F repo=<repo> -F number=<number>
```

The `id` returned here is the **thread node ID**, distinct from the REST `comment.id`. Phase 5.2
needs it to resolve the thread. Save it.

`reviewThreads(first:100)` and `comments(first:50)` cover most PRs, but a long-lived or high-traffic
one can exceed either limit. The authoritative truncation signal is `pageInfo.hasNextPage`;
pagination is **two-level** because GraphQL cursors are scoped to the connection instance that
produced them:

1. **Outer — threads.** If `reviewThreads.pageInfo.hasNextPage` is `true`, re-issue the query above
   with `-F threadCursor=<endCursor>` and loop until it is `false`.
2. **Inner — comments on a specific thread.** Each thread exposes its own `comments.pageInfo`. If a
   thread reports `comments.pageInfo.hasNextPage == true`, that thread's `endCursor` is meaningful
   **only for that thread** and cannot be reused across threads. Paginate per-thread via a
   `node(id:)` follow-up, using the `thread.id` saved above:

   ```bash
   gh api graphql -f query='
   query($threadId:ID!, $cursor:String) {
     node(id:$threadId) {
       ... on PullRequestReviewThread {
         comments(first:50, after:$cursor) {
           pageInfo { hasNextPage endCursor }
           nodes { databaseId author{login} path line body url }
         }
       }
     }
   }' -F threadId=<thread-node-id> -F cursor=<endCursor>
   ```

(A result count of exactly 100 or 50 can coincide with the page size, so it is a weaker heuristic
than `hasNextPage` — treat it as a hint to check, not a signal on its own.) Missing a thread or a
comment at survey time silently drops feedback during classification, the worst failure mode here.

`isOutdated: true` means the comment anchored to code that has since changed; the reviewer's concern
may already be addressed by a later push. Confirm before closing.

### 1.4 Read CI state

```bash
gh pr checks <number>
```

CI failures are feedback too. When a CI failure overlaps with a reviewer's concern (same lint rule,
same missing test, same typo), fold the fix into the thread response so the reviewer sees it
addressed in one place. Summarize the failing checks in your status report, and hand isolated CI
failures — or anything needing flake-vs-real classification — to `monitor-ci`.

### 1.5 Report the survey

When invoked as a standalone check, report in this shape:

- **Summary line**: state, review decision, CI status, mergeability.
- **Unresolved threads**: one line per thread — `path:line — reviewer — excerpt` — plus
  the thread node ID so the user can act on it later.
- **Failing CI checks**: name + link.
- **New commits since last review**: short-SHA + subject.

Stop here — classifying and replying wait for the user's go-ahead.

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

After each logical change, before pushing:

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

## Phase 5: Close the loop

### 5.1 Reply on every addressed thread

Even threads you resolve get a one-line reply. The reply is the audit trail — the resolve button
alone leaves reviewers guessing which commit addressed which comment. For declines and deviations,
the reply is the whole point; the resolution (if any) follows from it.

Post the reply **as the bot** with `a-novel core bot-comment --reply-to` — never bare `gh`, which
attributes the note to your user account. The `<comment-id>` is the REST review-comment id from the
Phase 1.2 inline listing, not the GraphQL thread node id. Top-level comments do **not** thread with
inline review comments, so a thread reply must pass `--reply-to`:

```bash
a-novel core bot-comment <org> <repo> <number> --reply-to <comment-id> \
  --body "Fixed in <short-sha>."
```

The command triggers the dispatcher workflow and blocks until it finishes; on a non-zero exit, read
the surfaced run log and retry.

### 5.2 Resolve settled threads

A thread is settled when you've decisively answered it — clean accept (3.4), small deviation (3.3),
or defensible decline (3.1). All three get resolved. Only large deviations and unsure threads (3.2)
stay open, because both need the reviewer's next move.

Resolving a thread is **not** a comment, so it always runs as you (operator user token, plain `gh`);
the bot can only post comments. Resolve with the thread node id from Phase 1.3:

```bash
gh api graphql -f query='
mutation($id:ID!) {
  resolveReviewThread(input:{threadId:$id}) {
    thread { id isResolved }
  }
}' -F id=<thread-node-id>
```

The `thread-node-id` comes from the Phase 1.3 GraphQL response, not the REST comment ID.

### 5.3 Re-request review

Only after:

- Every accepted fix has been pushed.
- CI is green — hand off to `monitor-ci` while it runs.
- Any decline replies have been posted so the reviewer has context when they look again.

Then:

```bash
gh api repos/<owner>/<repo>/pulls/<number>/requested_reviewers \
  -X POST -F 'reviewers[]=<reviewer-login>'
```

Note the `reviewers[]=...` syntax: `gh api` sends `-f` and `-F` values as scalar strings (`-F` infers
types only on literal `true`/`false`/`null`/ints), so neither `-f reviewers='["alice"]'` nor
`-F reviewers='["alice"]'` produces a JSON array — both send a string. The documented way to build an
array is repeated `key[]=value` entries, one per element; the GitHub API then receives an actual
`reviewers: [...]` payload.

Re-requesting mid-exchange, while declines are unresolved, or with failing CI burns reviewer
attention and signals carelessness. Don't.

### 5.4 Give the workspace back

Approval is where a scratch stack's life ends. Once the reviewer has approved and no thread is
awaiting a change from you, prune the stack this work was done in:

```bash
a-novel core stacks prune <name>
```

This is the trigger `git-conventions` › Workspace Hygiene names, and it lands here because this skill
is where approval arrives — a stack pruned at push time gets rebuilt by the first review comment.

Only prune a stack you allocated. Work done in the default stack leaves nothing to reclaim, and
`prune` refuses that one anyway.

---

## Starting your own thread

Claude may initiate a thread when:

- Applying a fix surfaces an adjacent concern that deserves discussion — either on the
  same line, or at the top level for cross-cutting issues.
- A decision taken in the PR is non-obvious and the commit message alone won't reach
  future readers.
- An assumption needs reviewer confirmation before another round.

Every comment you post goes through the bot (`a-novel core bot-comment`), never bare `gh`.

**Top-level comment** (general discussion — or a concern that points at specific code,
naming the `file:line` in the body):

```bash
a-novel core bot-comment <org> <repo> <number> --body "..."
```

**Reply on an existing thread** (continuing a review conversation):

```bash
a-novel core bot-comment <org> <repo> <number> --reply-to <comment-id> --body "..."
```

Starting a _brand-new_ inline thread anchored to a code line is not a bot capability — the dispatcher
posts top-level comments and thread replies only. To raise line-specific code as the bot, post a
top-level comment that names the `file:line`; anchored-thread creation is a human reviewer's.

---

## Issue discussions (planning & triage)

Everything above is written for pull requests, but the same posture — **a conversation, not a
checklist** — governs **issues**, above all the planning issues `plan-feature` produces. Use this
section when reading and responding to comments under an issue: answering the human's questions on a
plan, posting your own open questions, or triaging an incoming report.

**What carries over unchanged:** the survey-then-act shape; the accept / accept-with-deviation /
decline / unsure classification (Phase 2); rationale-dense, zero-filler replies; the bots-vs-humans
skepticism; and the hard rule that **every comment goes through the bot** —
`a-novel core bot-comment <org> <repo> <issue-number> --body` — because issues and PRs share one
number sequence and one dispatcher. Reads still use plain `gh`.

**What's different — issues are simpler than PRs:**

- **No inline threads, no resolution state, no re-request-review.** Issue comments are a single flat
  top-level stream: no `--reply-to` (that targets PR inline review threads), no `resolveReviewThread`
  mutation, no reviewer to re-request. None of Phase 1.3 (thread node IDs), Phase 5.2 (resolve), or
  Phase 5.3 (re-request) applies.
- **Survey with the issue endpoints:**

  ```bash
  gh issue view <n> --repo <org>/<repo> \
    --json number,state,title,labels,assignees,body,comments
  gh api repos/<org>/<repo>/issues/<n>/comments   # the full comment stream
  ```

- **The body belongs to the plan; the comments are the discussion.** Keep the back-and-forth in
  comments so the body stays the clean, current plan (see `plan-feature`). When you and the human
  settle a question, fold the decision into the **body** with
  `gh issue edit <n> --repo <org>/<repo> --body-file <file>` — that edit runs as the **operator**
  token (the bot can comment but cannot edit a body) — then optionally drop a one-line bot comment
  noting it's resolved.
- **Closing, not resolving.** An issue stays **open** while work is pending; it closes when its PR
  merges (`Closes #<n>`) or when you and the human agree it's done or won't be done
  (`gh issue close <n> --repo <org>/<repo>`, passing `--reason completed` or `--reason "not planned"`
  — gh's spaced, quoted values, per `gh issue close --help`). Drop the `triage`
  label once assessed, and advance the board **Status** as the work moves.

**The planning loop, concretely.** Post each open question as its own bot comment, carrying your
recommendation (the `plan-feature` posture — propose, don't just ask). Wait for the human's reply.
Classify it with the Phase 2 buckets exactly as you would a review comment, then act: fold accepted
decisions into the body, keep discussing the unsure ones, and push back (once, with a reason) where
you disagree. The body converges on the agreed plan; the comment stream records how you got there.

**An answered comment is permanent history.** Post the follow-up as a new comment and leave the
answered one in place, so the human's reply keeps the context it was written against. Replacing a
comment in place is right only while it is still **unanswered** — a list of open questions a design
reshape has made obsolete, where leaving the stale list would mislead. Once even one item has an
answer, the whole comment stays: deleting it strands the reply, which goes on referencing headings
that exist nowhere. Permission to replace a comment is granted against its unanswered state and does
not carry forward past the first answer, so check for a reply before any
`gh api -X DELETE .../issues/comments/<id>`.

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
  replies, folding decisions back into the body). See [Issue discussions](#issue-discussions-planning--triage).
- **To `monitor-ci`** — for failing checks that need flake-vs-real classification or a retry loop.
  When CI agrees with a reviewer (same root cause), fold the fix into the thread response rather than
  pushing twice.
- **To `git-conventions`** — every review-driven commit. No exceptions.
- **To the layer-specific skills** — `write-go`, `write-go-service` (or `write-go-kit` for a-novel-kit repos), `write-go-tests`, `write-openapi`,
  `write-js-package`, etc. Phase 4 writes code; those skills govern _how_.

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

---
name: resolve-pr-feedback
description: >
  Survey a pull request's combined state (CI + review threads + reviewer status) and work through
  reviewer feedback for Agora backend services. Use this skill whenever checking on an open PR
  ("what's the state of PR X", "look at the CI and comments on 123", "monitor PR 532"), reading
  Copilot or human review comments, deciding whether to accept a suggestion, replying on a thread,
  resolving a thread after a fix, starting your own thread to flag a concern, or re-requesting
  review after changes. Always invoke it when the user mentions PR review comments, a reviewer's
  name, a failing check on an open PR, or asks anything that involves reading + responding to
  feedback on a pull request — even if the word "resolve" is not used. Pairs with git-conventions
  (for the format of review-driven fix commits).
---

# Resolve PR Feedback

This skill governs how Claude surveys a pull request's state and works through reviewer
feedback. It is called both as a passive read ("check PR 532") and as an active workflow
("address the comments on PR 532"). Both modes share Phase 1; only the resolve workflow
continues through Phases 2–5.

---

## Guiding principle

A pull request is a **conversation**, not a checklist. Reviewers — human or bot — can be
right, wrong, unclear, or working from a partial picture of the change. Apply judgment,
not obedience. The failure modes are symmetric: silently overriding a valid concern
erodes trust; blindly applying an incorrect suggestion ships a regression. In both cases
the remedy is the same — speak on the thread so the reviewer sees your reasoning and
can push back.

Two rules anchor the loop. Everything else in this skill is the mechanics behind them:

1. **Clear-cut → resolve.** A thread you have decisively answered — accepted (and pushed
   the fix) or declined (with a defensible reason) — is settled. Resolve it. Open
   threads should signal a real decision pending, not a pending audit trail. The
   reviewer can re-open if they bring new information.
2. **Genuinely unclear or worth discussing → reply with a specific question, leave open.**
   Silence is the worst option; acting on partial understanding is the second-worst.

If you partially accepted, took a different direction, or bundled the fix with adjacent
changes, the **reply explains the deviation** before resolution. Quiet, after-the-fact
re-interpretation is what erodes trust. Larger deviations where the reviewer might
prefer the original suggestion fall under rule 2 — leave open with an explicit "OK with
this approach?" question.

---

## Phase 1: Survey PR state

Callable on its own. When the user asks only to "check", "look at", or "monitor" a PR,
run this phase, report back, and stop. Do not proceed to action without explicit
go-ahead.

### 1.1 Read the PR envelope

```bash
gh pr view <number> --json \
  number,state,isDraft,mergeable,reviewDecision,baseRefName,headRefName,title,commits,reviews
```

Fields that matter:

- **state**: OPEN / CLOSED / MERGED. Never act on non-OPEN PRs without confirmation —
  reopening a closed discussion is a different kind of decision.
- **isDraft**: draft PRs rarely need the full review-cycle. If the reviewer left
  comments anyway, confirm with the user whether they want them addressed now.
- **reviewDecision**: APPROVED / CHANGES_REQUESTED / REVIEW_REQUIRED. Shapes what
  Phase 5 looks like.
- **baseRefName** / **headRefName**: you almost always land fixes as new commits on
  `headRefName`. Force-push with `--force-with-lease` only if a rebase was required.

### 1.2 Read review comments

GitHub splits review feedback across three endpoints. You need all three to see the
full picture — a comment in one does not show up in the others.

**Inline review comments** (anchored to `file:line`):

```bash
gh api repos/<owner>/<repo>/pulls/<number>/comments
```

Each record has `id`, `path`, `line`, `body`, `user.login`, `in_reply_to_id`, `commit_id`.
The `id` here is the REST comment ID — not the GraphQL thread node ID used for
resolution (see 1.3).

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
Read all three endpoints when surveying.

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

The `id` returned here is the **thread node ID** — distinct from the REST `comment.id`.
You need it for Phase 5.2 to resolve the thread. Save it.

`reviewThreads(first:100)` and `comments(first:50)` cover the vast majority of PRs, but
long-lived or high-traffic PRs can exceed either limit. The authoritative truncation
signal is `pageInfo.hasNextPage`; pagination is **two-level** because GraphQL cursors
are scoped to the specific connection instance that produced them:

1. **Outer — threads.** If `reviewThreads.pageInfo.hasNextPage` is `true`, re-issue the
   query above with `-F threadCursor=<endCursor>` and loop until it is `false`.
2. **Inner — comments on a specific thread.** Each thread exposes its own
   `comments.pageInfo`. If a thread reports `comments.pageInfo.hasNextPage == true`,
   that thread's `endCursor` is meaningful **only for that thread** — it cannot be
   reused across threads. Paginate per-thread via a `node(id:)` follow-up, using the
   `thread.id` saved above:

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

(An exact-100 or exact-50 result count can coincidentally match the page size, so it is
a weaker heuristic than `hasNextPage` — treat it as a hint to check, not a signal on its
own.) Missing a thread or a comment at survey time means silently missing feedback
during classification, which is the worst failure mode for this phase.

`isOutdated: true` means the comment anchored to code that has since changed; the
reviewer's concern may already be addressed by a later push. Confirm before closing.

### 1.4 Read CI state

```bash
gh pr checks <number>
```

CI failures are feedback too. When a CI failure overlaps with a reviewer's concern (same
lint rule, same missing test, same typo), fold the fix into the thread response so the
reviewer can see it addressed in one place. For isolated CI failures — or anything that
needs flake-vs-real classification — hand off to `monitor-ci`. Summarize the failing
checks in your status report and use `monitor-ci` to investigate and drive next steps.

### 1.5 Report the survey

When invoked as a standalone check, report in this shape:

- **Summary line**: state, review decision, CI status, mergeability.
- **Unresolved threads**: one line per thread — `path:line — reviewer — excerpt` — plus
  the thread node ID so the user can act on it later.
- **Failing CI checks**: name + link.
- **New commits since last review**: short-SHA + subject.

Stop here. Do not begin classifying or replying without the user's go-ahead.

---

## Phase 2: Classify each unresolved thread

For every unresolved thread, fit it into one of four buckets. Read the full thread
(including prior replies), read the code it points at, and reason about each thread
independently — do not classify in bulk.

| Bucket                    | When to use                                                                                                                                                                                                      |
| ------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **Accept**                | The comment is correct and actionable; a straightforward change implements its intent.                                                                                                                           |
| **Accept-with-deviation** | The core concern is valid but the specific suggestion is wrong, partial, or better served by a different approach.                                                                                               |
| **Decline**               | The comment misunderstands the change, conflicts with a repo guideline (CLAUDE.md, a skill file, a documented decision), would reintroduce a security or correctness regression, or is out of scope for this PR. |
| **Unsure**                | You cannot confidently place the comment into one of the above.                                                                                                                                                  |

Signals that push toward **decline** specifically:

- Accepting would violate a rule in `.claude/skills/*/SKILL.md` — the skill file is the
  authoritative source, not the comment.
- The comment asks to re-expose something that was deliberately hidden for security
  (e.g., error strings on an unauthenticated endpoint). Treat that as a decline, not a
  conversation.
- The comment asks for feature work outside this PR's layer or scope. The right answer
  is usually "decline for this PR, file a follow-up."

Signals that push toward **unsure**:

- The comment assumes context you do not see in the PR (e.g., references an incident,
  a prior decision, or code in another repo).
- Two readings of the comment would lead to different fixes, and the reviewer did not
  pick one.
- The comment is terse ("this won't work") with no specifics.

**Bots vs humans.** Copilot and similar bots do not re-engage on thread replies. Still
classify their comments with the same rigor — bots miss context routinely, and blanket
acceptance is how insecure or incorrect changes land. Weight your reply toward the
**human** reviewer who will read the thread later.

A bot repeating the same claim across several comments is **not** independent evidence
of correctness; it is one opinion with a megaphone. Verify the underlying fact once —
official docs, a spec, or an empirical test (the latter is often one `gh api` call
away) — and cite that source in your reply. Treating repetition as confirmation is how
a confidently wrong bot propagates an error from the review into the codebase.

Bots are especially prone to confident errors about **external specs** — API endpoint
paths, header names, status code semantics, protocol details. When a comment asserts a
factual claim about a third-party system, the correct first move is to check that
system's authoritative source, not to argue from plausibility.

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

Resolve the thread. The reply is the closure; if the reviewer has new information that
changes the assessment, they can re-open and you re-enter Phase 2 on the same thread.
A defensible decline does not need to wait for the reviewer to close — leaving it open
creates audit-trail noise.

```bash
gh api repos/<owner>/<repo>/pulls/<number>/comments/<comment-id>/replies \
  -f body="$(cat <<'EOF'
<one-sentence reason>

Per <.claude/skills/...-SKILL.md section / CLAUDE.md anchor / linked source>.
Happy to revisit if I've missed context here.
EOF
)"
```

### 3.2 Unsure — start a discussion

Reply with a **specific question**, not a generic "what do you mean?". Quote the part
you are unsure about and lay out the interpretations you see. This respects the
reviewer's time and anchors the next round.

Wait for a reply before acting. Re-enter Phase 2 once the reviewer responds; multi-round
exchanges on a single thread are normal.

### 3.3 Accept-with-deviation

Apply the fix in the direction that actually makes sense (Phase 4). After pushing,
reply **explaining the deviation** before any resolution:

- "Took the core suggestion but scoped it to X instead of Y — Y would also touch the
  Z layer, which is out of scope for this branch."
- "Applied the spirit of the comment via <alternative> — the literal suggestion would
  not work because <reason>."

Whether to resolve depends on how far you deviated. Small, well-explained deviations
should be resolved — the reply is the audit trail. Larger deviations where the reviewer
might prefer the original suggestion stay open with an explicit "OK with this approach?"
question. **When the question is genuinely open, leave it open.**

### 3.4 Accept

Apply the fix (Phase 4). After pushing, reply with a one-liner:

- `Fixed in <short-sha>.`
- Optional: one sentence on anything non-obvious about how you applied it.

Resolve the thread.

---

## Phase 4: Apply fixes and push

### 4.1 Commit per `git-conventions`

One logical unit per commit. Pick the commit type that matches the change itself, not
the reviewer's category:

- Reviewer asked for a test → `test(<scope>): ...`
- Reviewer asked for a doc or description clarification → `docs(<scope>): ...`
- Reviewer flagged a real bug → `fix(<scope>): ...`
- Reviewer asked for a rename or internal reshape → `refactor(<scope>): ...`

Cite the review in the commit body so the log is self-documenting:

```
Addresses <reviewer-login> review feedback on #<PR-number>.
```

**Never amend a pushed commit to address review.** The review is anchored to the old
SHA; amending rewrites shared history and strands the review thread's context. Always
create new commits. (This is a hard rule from `git-conventions`.)

### 4.2 Run the narrowest test target

After each logical change, before pushing:

- Go changes (internal or `pkg/go`) → `a-novel test --type=go -y`
- `pkg/js` changes → `a-novel test --type=pnpm -y`

Full mapping in the `use-a-novel-cli` skill (auto-loaded). Never push a red tree.

### 4.3 Push

```bash
git push
```

If the fix required a rebase, use `git push --force-with-lease`. Never plain `--force`,
never force-push to `master`.

---

## Phase 5: Close the loop

### 5.1 Reply on every addressed thread

Even threads you resolve get a one-line reply. The reply is the audit trail — the
resolve button alone leaves reviewers guessing which commit addressed which comment.
For declines and deviations, the reply is the whole point; the resolution (if any)
follows from it.

Use the inline reply endpoint — top-level PR comments do **not** thread with inline
review comments:

```bash
gh api repos/<owner>/<repo>/pulls/<number>/comments/<comment-id>/replies \
  -f body="Fixed in <short-sha>."
```

### 5.2 Resolve settled threads

A thread is settled when you've decisively answered it — clean accept (3.4), small
deviation (3.3), or defensible decline (3.1). All three get resolved. Only large
deviations and unsure threads (3.2) stay open, because both genuinely need the
reviewer's next move.

If the helper bot's GitHub App lacks `resolveReviewThread` permission (returns
`FORBIDDEN`), fall back to the operator's user token. Bots are for posting; resolution
sometimes needs the broader admin scope.

```bash
gh api graphql -f query='
mutation($id:ID!) {
  resolveReviewThread(input:{threadId:$id}) {
    thread { id isResolved }
  }
}' -F id=<thread-node-id>
```

The `thread-node-id` comes from the Phase 1.3 GraphQL response — not the REST comment
ID.

### 5.3 Re-request review

Only after:

- Every accepted fix has been pushed.
- CI is green (hand off to `monitor-ci` while it runs if that skill is available on
  master — pending, tracked in #533 — otherwise watch `gh pr checks <n>` manually).
- Any decline replies have been posted so the reviewer has context when they look again.

Then:

```bash
gh api repos/<owner>/<repo>/pulls/<number>/requested_reviewers \
  -X POST -F 'reviewers[]=<reviewer-login>'
```

Note the `reviewers[]=...` syntax: `gh api` sends `-f` and `-F` values as scalar strings
by default (or, for `-F`, does type inference only on literal `true`/`false`/`null`/ints).
Neither `-f reviewers='["alice"]'` nor `-F reviewers='["alice"]'` produces a JSON array —
both send a string. The documented way to build an array is repeated `key[]=value`
entries, one per element; the GitHub API then receives an actual `reviewers: [...]`
payload.

Re-requesting mid-exchange, while declines are unresolved, or with failing CI burns
reviewer attention and signals carelessness. Don't.

---

## Starting your own thread

The skill is not just defensive. Claude may initiate a thread when:

- Applying a fix surfaces an adjacent concern that deserves discussion — either on the
  same line, or at the top level for cross-cutting issues.
- A decision taken in the PR is non-obvious and the commit message alone won't reach
  future readers.
- An assumption needs reviewer confirmation before another round.

**Inline thread** (anchored to a line on the current head commit):

```bash
gh api repos/<owner>/<repo>/pulls/<number>/comments \
  -f body="..." \
  -f commit_id="<head-sha>" \
  -f path="<file>" \
  -F line=<N> \
  -f side="RIGHT"
```

**Top-level comment** (general discussion, not anchored to code):

```bash
gh pr comment <number> --body "..."
```

Rule of thumb: inline for anything that points at specific code; top-level for PR-wide
context (e.g., a summary of how a batch of fixes was applied, or a question about the
overall direction).

---

## Common pitfalls

- **Silent resolution without a reply.** Reviewers cannot tell which commit addressed
  the thread — they have to hunt. Always pair a resolve with a reply linking to the SHA.
- **Blanket acceptance of bot comments.** Copilot can be wrong. Classify every comment;
  the failure mode of over-trust is insecure or incorrect code landing in main.
- **Treating repeated bot claims as confirmation.** Three identical comments from one
  bot are one opinion amplified, not three independent signals. Verify the underlying
  fact once against an authoritative source before accepting or declining.
- **Accepting a reviewer's spec claim without checking the spec.** When a comment
  asserts a specific API shape, endpoint path, header name, or protocol detail, verify
  it against the upstream docs — or make a single empirical call — before editing.
  Plausibility is not evidence.
- **Using the wrong endpoint for replies.** Top-level PR comments (`gh pr comment`) do
  not thread with inline review comments. A reply to an inline comment must go through
  `POST /pulls/:n/comments/:id/replies`.
- **Leaving clear-cut threads open.** A thread you have decisively answered — accept
  and fixed, or decline with a defended reason — does not need to wait for the reviewer
  to close. Leaving it open creates audit-trail noise. Resolve it; the reviewer can
  re-open if they have new information. Reserve the open state for genuinely-pending
  decisions.
- **Amending or force-pushing to address review.** Review comments are anchored to the
  SHA that was reviewed. Rewriting strands them. New commits, every time.
- **Re-requesting review too early.** Wait for all pushes + green CI + posted declines.
- **Acting while unsure.** If the comment is ambiguous, the only correct first move is
  a specific question. Do not apply a best-guess fix and then explain on the thread —
  that wastes a round.
- **Mixing types in one commit to bundle a batch of review fixes.** Each review-driven
  commit is still subject to `git-conventions` — a `test` and a `docs` fix are two
  commits, even when they came from the same review.

---

## Hand-offs

- **From `open-pull-request`** — once a PR is open and reviewers start commenting, the
  push-and-open flow hands off here to assess CI, review threads, and reviewer status,
  then work through the feedback.
- **To `monitor-ci`** — for failing checks that need flake-vs-real classification or a
  retry loop. When CI agrees with a reviewer (same root cause), fold the fix into the
  thread response rather than pushing twice.
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
| Reply on an inline thread          | `gh api repos/<o>/<r>/pulls/<n>/comments/<cid>/replies -f body="..."`                                               |
| Resolve a thread                   | GraphQL `resolveReviewThread` mutation (Phase 5.2)                                                                  |
| Start a new inline thread          | `POST .../pulls/<n>/comments` with `commit_id`, `path`, `line`, `side`                                              |
| Top-level PR comment               | `gh pr comment <n> --body "..."`                                                                                    |
| Re-request review                  | `gh api .../pulls/<n>/requested_reviewers -X POST -F 'reviewers[]=<login>'`                                         |

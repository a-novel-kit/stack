# Issue discussions

Read this reference when routed here by [resolve-pr-feedback](../SKILL.md). Its rules apply to the selected work.

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

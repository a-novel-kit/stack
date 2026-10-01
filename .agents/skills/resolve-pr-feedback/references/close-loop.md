# Reply, resolve, and re-request review

Read this reference when routed here by [resolve-pr-feedback](../SKILL.md). Its rules apply to the selected work.

Contents:

- [Phase 5: Close the loop](#phase-5-close-the-loop)
- [5.1 Reply on every addressed thread](#51-reply-on-every-addressed-thread)
- [5.2 Resolve settled threads](#52-resolve-settled-threads)
- [5.3 Re-request review](#53-re-request-review)
- [5.4 Give the workspace back](#54-give-the-workspace-back)
- [Starting your own thread](#starting-your-own-thread)

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

Answering several threads at once? Use `--batch`, which posts them in one run — a JSON array of
`{number, body, reply_to?}` on disk or on stdin:

```bash
echo '[{"number":21,"reply_to":3654568470,"body":"Removed."},
       {"number":21,"reply_to":3654577321,"body":"Dropped the helper."}]' \
  | a-novel core bot-comment <org> <repo> --batch -
```

The command triggers the dispatcher workflow and blocks until it finishes; on a non-zero exit, read
the surfaced run log and retry.

> **`gh api …/pulls/<n>/comments/<id>/replies` is the trap.** It is the obvious REST call, it works,
> and it posts as the **human**. There is no capability gap driving you to it — `bot-comment`
> supports both `--reply-to` and `--batch` against exactly that endpoint. Reaching for `gh` here is
> a habit, not a workaround, and the damage is silent: the reply reads correctly and is signed by
> the wrong person. (Verified 2026-07-27, after doing precisely this on eight threads of
> `service-genai#21`.)
>
> The narrow thing the bot genuinely **cannot** do is create a _review_ carrying **new**
> line-anchored comments — that needs `POST /pulls/{n}/reviews`, which the dispatcher does not
> implement. Replying into an **existing** thread is fully supported. Do not let the real carve-out
> excuse the reply path.

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

- Every accepted fix has been pushed, and linked issue discussions have been checked for scope changes.
- The request is appropriate to the current stage; completed-work review requires the testing and
  cleanup gates in `develop-feature`.
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

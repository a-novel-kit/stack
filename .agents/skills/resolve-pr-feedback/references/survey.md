# PR survey commands

Read this reference when routed here by [resolve-pr-feedback](../SKILL.md). Its rules apply to the selected work.

Contents:

- [Phase 1: Survey PR state](#phase-1-survey-pr-state)
- [1.1 Read the PR envelope](#11-read-the-pr-envelope)
- [1.2 Read review comments](#12-read-review-comments)
- [1.3 Read thread resolution state](#13-read-thread-resolution-state)
- [1.4 Read CI state](#14-read-ci-state)
- [1.5 Report the survey](#15-report-the-survey)

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
- **isDraft**: draft PRs and issues receive review in parallel. Address feedback within the already
  authorized development task; draft status does not require fresh permission. A read-only survey
  request remains read-only.
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

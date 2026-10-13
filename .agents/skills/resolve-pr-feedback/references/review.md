# Review a pull request

Read this reference when routed here by [resolve-pr-feedback](../SKILL.md). Its rules apply to the selected work.

A request to review a pull request is a request to publish the review on it. Post it as soon as the
review is complete, without asking first, and give the developer its link. A status check ("check",
"look at") stays read-only under [the survey procedure](survey.md).

## Prepare the findings

1. Survey the PR with [the survey procedure](survey.md): envelope, reviews, threads, CI, and the
   linked issues with their comments. When an open thread already raises a finding, reply there
   under [closing the loop](close-loop.md) instead of raising it again.
2. Load the skills that govern the changed files. Review the paths the diff affects outside its
   hunks too: callers, reusable workflows and the repositories that consume the change.
3. Verify each finding with the code, a live call or a test run before posting it.

## Post one atomic review

Post every finding in one review, with your operator `gh` token. The bot cannot create a review,
and a top-level comment that links `file:line` is not one: it has no thread to reply in or resolve.

- `commit_id`: the head SHA, read again just before posting. When it moved, review the new commits
  first.
- `comments`: one finding per comment, anchored where the fix belongs, stating the fix. `line` and,
  for a range, `start_line` number the lines of the new file, with `side` and `start_side` set to
  `RIGHT`. An anchor outside the diff hunks makes GitHub reject the whole review, so list the hunks
  first (`git diff <base>...<head> -- <path> | grep '^@@'`).
- `body`: the verdict, the findings with no line in the diff (a file the PR does not touch, a
  consequence in another repository) as permalinks at the head SHA, and what you verified.
- `event`: `REQUEST_CHANGES` when a finding blocks the merge, otherwise `COMMENT`. GitHub refuses
  `REQUEST_CHANGES` on a pull request the operator authored, so use `COMMENT` there and state the
  verdict in the body. Never `APPROVE`: approval stays the developer's decision.

Build the payload with a script or `jq` rather than escaping Markdown by hand, then post it:

```bash
gh api repos/<owner>/<repo>/pulls/<number>/reviews --method POST --input review.json
```

```json
{
  "commit_id": "<head-sha>",
  "event": "REQUEST_CHANGES",
  "body": "**Changes requested.** <verdict, findings outside the diff, what was verified>",
  "comments": [
    { "path": "<file>", "line": 77, "side": "RIGHT", "body": "**Blocking:** <finding and fix>" },
    {
      "path": "<file>",
      "start_line": 49,
      "start_side": "RIGHT",
      "line": 52,
      "side": "RIGHT",
      "body": "<finding and fix>"
    }
  ]
}
```

## After posting

Check that every inline comment landed on its line: `gh api repos/<owner>/<repo>/pulls/<number>/comments`
returns `line` and `start_line` for each. Review new pushes in a new review, and continue existing
threads under [closing the loop](close-loop.md). When a review went out in another form, post it
correctly, verify it landed, then delete the superseded comment so reviewers read one review.

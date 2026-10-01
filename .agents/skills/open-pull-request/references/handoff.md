# Session recap and approval handoff

Read this reference when routed here by [open-pull-request](../SKILL.md). Its rules apply to the selected work.

## Phase 8: Close With a Session Recap Table and Approval Command (mandatory)

**Whenever you finish a stretch of code or issue work, end your reply with a recap table** so the
operator can jump straight to whatever needs their attention. This is not optional and not limited
to what changed since the last prompt: **list every item from this whole session that still needs
attention** — PRs awaiting review or merge, issues to act on, branches pushed, CI still running —
even ones you reported turns ago, until they are actually resolved.

Each row's identifier is an **inline markdown link** to the PR or issue, so the target is one click
away. Every PR row also carries its approval handoff in a dedicated **Admin-only approval (after
review)** column; use an em dash for non-PR rows. A minimal shape:

```markdown
| Item                                    | State           | Needs               | Admin-only approval (after review)                                                                                                                                   |
| --------------------------------------- | --------------- | ------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| [#321](https://github.com/…/321)        | Ready, CI green | Your review → merge | `gh workflow run approve-pr.yaml --repo a-novel/service-authentication --ref master --field pull_request=https://github.com/a-novel/service-authentication/pull/321` |
| [.github#432](https://github.com/…/432) | Task, blocked   | Decide ownership    | —                                                                                                                                                                    |
```

Drop the table only when the turn touched no code or issues at all (a pure question). If nothing
is outstanding, say so in one line instead of an empty table. This rule is session-global — its
authoritative statement lives in memory (`session-recap-table`) so it fires even on turns where
this skill never loads (e.g. issue-only work under `triage-issues`).

For rendered UI, the same final report must also include the freshly verified direct local Storybook
link required by `write-frontend`, adjacent to the recap table or in the relevant PR row. Never put
that local-only link in the PR body.

For every open PR in the recap, put a copy-pasteable command **inside that PR's table row** that lets
a repository admin record their approval through the repo's `approve-pr` workflow after reviewing
the PR:

```bash
gh workflow run approve-pr.yaml --repo <org>/<repo> --ref <default-branch> --field pull_request=<PR-URL>
```

Resolve every placeholder before presenting the command: use the PR's exact repository and URL, and
the repository's actual default branch. Label it **admin-only** and say it is for use after review.
Never place the command only in prose beside or after the table: condensed task summaries may retain
the recap table while omitting surrounding prose.
This is a handoff command, not authorization to dispatch the workflow; never run it unless the user
explicitly asks. The workflow itself fails closed when the dispatching user is not a repository
admin.

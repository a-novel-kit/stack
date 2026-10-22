# Audit-log alerts

Read this reference when routed here by [write-github-actions](../SKILL.md): before changing the
audit-log alerts feed or an ignore rule, or when an alert needs an answer.

Contents:

- [What alerts and where](#what-alerts-and-where)
- [Window and deduplication](#window-and-deduplication)
- [Ignore rules](#ignore-rules)
- [Reading an alert](#reading-an-alert)
- [When the feed stops](#when-the-feed-stops)

## What alerts and where

An audit-log entry that can hand someone access, weaken a protection or push past a rule reaches the
private `security-audit` Discord channel within half an hour. The watched actions are a fixed list in
`collect.mjs`, by family:

- pushes past a rule, and changes to rulesets, branch protections and environments;
- members, collaborators, teams and roles;
- keys, hooks, Apps, OAuth Apps, tokens and self-hosted runners;
- the org's security settings: two-factor, SAML, IP allow lists and push protection;
- repositories created, deleted, transferred or made public, and a workflow disabled.

Git events, workflow runs and package versions stay out: they carry no ref or actor, or grant nothing.

| Piece                                        | Where                                                                           |
| -------------------------------------------- | ------------------------------------------------------------------------------- |
| Engine: `notify-audit-run.yaml`              | `a-novel-kit/workflows`, reading through `generic-actions/collect-audit-alerts` |
| Caller and ignore rules: `notify-audit.yaml` | Each org's `.github` repo, `workflow_dispatch` only                             |
| Clock: every 30 minutes, at :11 and :41      | `github_dispatch_schedules` in a-novel/infra's `bootstrap/github-dispatch.tf`   |
| Channel: the `SECURITY_WEBHOOK` secret       | A repository secret of each `.github` repo; one webhook serves both orgs        |
| Ping: the `TOKEN_EXPIRY_ADMIN` secret        | The admin who triages token expiry                                              |

A run with alerts posts once, in red, titled `<N> audit-log alerts in <org>` and linking the org's
audit log. Alerts sharing an action, an actor and a target, such as one change in nine
repositories, read as one line. The Agent App reads the log with an installation token holding
organization **Administration: read** and nothing else. The default token reads the runs.

A public repository's run summary is public, so the step summary holds counts only: entries read,
alerting and fresh, and what each ignore rule hid. The alerts themselves reach only Discord. A
`hook.create` entry, for one, holds the webhook URL in clear.

## Window and deduplication

GitHub indexes some audit-log entries late. Each run therefore reads from one hour before the
caller's previous successful run, the **overlap**, to the present. A first run reads the last hour.
An entry indexed more than an hour late falls before every window and never alerts.

The overlap rereads alerts the previous run posted. Each run uploads the `_document_id` of every
alert in its window as its `notify-audit-seen` artifact, kept one day, and the next run skips
those. A lost artifact warns in the run, and its alerts may post again.

The caller's concurrency group runs one poll at a time. A post Discord refuses fails the run, so the
next run reads from the last success and posts the alerts again: a duplicate, never a loss.

## Ignore rules

A routine shape gets an ignore rule in the `ignore:` JSON list of the org's `.github`
`notify-audit.yaml`. A shape both orgs produce goes in both callers, each with its own org's bots:
`anovelbot-*` logins and App ids in `a-novel`, `anovelkitbot-*` in `a-novel-kit`.

```json
{
  "reason": "Renovate tries a direct merge, which the merge queue refuses",
  "actor": "^anovelbot-dependencies\\[bot\\]$",
  "action": "^protected_branch\\.rejected_ref_update$",
  "branch": "^refs/heads/master$"
}
```

- A rule needs a `reason` and at least one pattern. The engine refuses the whole list otherwise, and
  the run fails before reading the log.
- Each other key names a top-level field of the audit-log entry, and its value is a regular
  expression the field must match. A list field matches as its sorted values joined by commas, and a
  missing field as the empty string, so `"overridden_codes": "^$"` requires no override.
- An entry is hidden only when every pattern of a rule matches. Anchor each pattern with `^` and
  `$`, and pin the actor, the token type (`programmatic_access_type`) and the exact override codes, so
  a rule hides one shape and nothing near it.
- `user_agent` and `referrer` come from the client. Use them only to narrow a rule that already pins
  the actor and the token type.
- Read the fields from a real entry with the `audit` function of the
  [compromise runbook](../../../../docs/sysadmins/respond-to-compromise.md#investigate), filtered to
  the action.
- The run's step summary counts what each rule hid. Delete a rule that keeps matching nothing.

`a-novel repo update` writes the managed governance files straight to `master` through the admin's
GitHub CLI token, past the merge queue, the checks and the review. A rule hides that shape: kushuh,
an OAuth token, a GitHub CLI user agent and the three override codes. A token lifted from the
admin's machine can therefore write `master` unseen, while everything else it touches still alerts.
Two more rules hide the admin CLI's environment and team grants, and `repo update`'s ruleset
reconciles. An admin action outside those shapes, such as a member added in the browser, alerts.

## Reading an alert

An alert reads as two lines. The first names the action, the actor and the targets, such as the
branch and the override codes. The second names the repositories, the earliest time, the token
type and a short label for the user agent:

| Label                                          | Means                                                           |
| ---------------------------------------------- | --------------------------------------------------------------- |
| `Chrome on Linux`, `Safari on iOS`             | A person in a browser                                           |
| `GitHub CLI v2.102.0`                          | The GitHub CLI, the admin's tool for `repo update` and runbooks |
| `Go-http-client/2.0`, `curl/8.5.0`, `octokit…` | A script, through a known library or tool                       |
| `terraform-provider-github/6.6.0`              | Any other client, by its first product token                    |
| `unrecognized agent`                           | An agent with no product token at its start                     |

The label copies at most 60 product-token characters from the agent, so no client-written text
renders as a link. The audit log, which the post links, keeps the raw value, and the ignore rules
match that raw value.

An alert nobody expected is a possible compromise. Follow
[respond to a compromise](../../../../docs/sysadmins/respond-to-compromise.md): freeze, cut the
account off, then investigate with the account's login.

## When the feed stops

`notify-audit` cannot report its own silence. A broken dispatch, a revoked App key or a disabled
workflow all stop it without a post. The hourly `notify-stuck` run in each `.github` repo watches
it: its `watch-audit` job reads the last successful `notify-audit.yaml` run with the default token
alone, and posts once to the security channel when that success turns 2 hours old, four missed runs.
The check runs only when the caller passes `security_webhook`.

Every timed workflow shares the org's dispatch token, so a lapsed token stops the watch too.
`token-expiry` warns days before the token expires.

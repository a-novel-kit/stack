---
name: check-access
description: >
  Know what the operator's GitHub roles, tokens and accounts allow, check before an operation a base
  contributor may lack, and state what is missing instead of trying. Load before approvals, merges
  past a gate, releases, privileged labels, repository or org settings, secrets, Google Cloud,
  Bitwarden or Discord, and when a call fails with 403, 404 or a missing secret.
---

# Check access

An agent acts with the developer's own `gh` token, secrets and accounts, so it holds exactly their
access. Most members are developers, and many operations in these skills need more. Before such an
operation, check that the developer holds what it needs. When they do not, tell them what is missing
and who holds it, then stop. The gates are deliberate: never retry, borrow another credential, fork,
switch tools or ask for a broader token scope to get past one.

## Roles

Each role is a GitHub team per repository class: `service`, `platform`, `infra`, `library`,
`workflows` or `meta`. A member joins the area teams they work in.

| Role       | Team                                    | Adds                                                                                      |
| ---------- | --------------------------------------- | ----------------------------------------------------------------------------------------- |
| Developer  | `developers-<class>`                    | Write: push branches, open pull requests, label, dispatch workflows, re-run jobs          |
| Ops        | `developers-infra` and a Google account | Production: `gcloud` reads, OpenTofu state, IAP SSH, secret versions                      |
| Maintainer | `maintainers-<class>`                   | Maintain: approvals that `review-gate` counts                                             |
| Owner      | `owners-<class>`                        | Admin: ruleset bypass, `approve-pr` on their own pull requests, the `release` environment |
| Core       | `core`, owner of both organizations     | Admin everywhere, org settings and teams, Bitwarden, `a-novel repo update`, Discord admin |

A role covers its class's repositories only; elsewhere the member reads. Pull requests are for
collaborators, so a developer opens them only where they can write, and a fork is refused. The stack
repository is library class.

Write lets anyone approve and label, but `review-gate` counts an approval, or a privileged label
such as `allow-dependency-change`, only from a maintainer, an admin or the [Agent] App. An agent never
applies a privileged label: it records a reviewer's decision.

Core hands some access out one by one, whatever the role:

- Tokens in `a-novel secrets`: `BWS_ACCESS_TOKEN` (core only), `DISCORD_READER_TOKEN`,
  `DISCORD_WRITER_TOKEN`, and the service secrets a repository's `.a-novel/secrets.yaml` declares.
- Write on an organization's project board, granted per project.
- A planned downtime, which core schedules.

Each developer creates their own `read:packages` token for GitHub Packages, as the developer guide
shows.

## Check before acting

Run a check at the first operation that needs it, and reuse the answer for the session.

| Access               | Check                                                                                             | Holds when                            |
| -------------------- | ------------------------------------------------------------------------------------------------- | ------------------------------------- |
| Teams                | `gh api user/teams --jq '.[] \| .organization.login + "/" + .slug'`                               | the team is listed                    |
| Role on a repository | `gh api repos/<owner>/<repo> --jq .permissions`                                                   | `push`, `maintain` or `admin` is true |
| Org owner            | `gh api user/memberships/orgs/<org> --jq .role`                                                   | `admin`                               |
| Token scopes         | `gh auth status`                                                                                  | the scope is listed                   |
| Local secret         | `a-novel secrets ls`                                                                              | the id is listed                      |
| Packages token       | `grep -q '//npm.pkg.github.com/:_authToken' ~/.npmrc`                                             | it succeeds                           |
| Google Cloud         | `gcloud auth list --filter=status:ACTIVE --format='value(account)'`                               | an `@agorastoryverse.com` account     |
| Project board        | `gh api graphql -f query='{organization(login:"<org>"){projectV2(number:<n>){viewerCanUpdate}}}'` | `true`                                |

A missing tool, such as `gcloud`, means the access is missing too. A scope is not a role:
`gh auth refresh -s admin:org` helps an org owner only, and a 403 or a 404 from a missing role is no
scope problem.

## When access is missing

Tell the developer which role or grant the step needs, that they lack it, and who holds it. Hand the
step over as a request to that holder, never as a command the developer cannot run: "needs a
maintainer's review", "core cuts the release". When the developer asks for such a command, answer
with what is missing instead. Carry on with the parts of the task that do not depend on the step.

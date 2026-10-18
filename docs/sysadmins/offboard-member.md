# Offboard a member

A leaving member hands their work over first, then loses each access they held. Plan the hand-off with them before their last day. Removing members takes owner rights on both GitHub organizations.

```bash
LOGIN=octocat
```

## Hand their work over

### List their open work

```bash
FILTERS=(--owner a-novel --owner a-novel-kit --state open)
gh search issues --assignee "$LOGIN" "${FILTERS[@]}"
gh search prs --author "$LOGIN" "${FILTERS[@]}"
gh search prs --review-requested "$LOGIN" "${FILTERS[@]}"
```

### Give each item a new owner

Agree with them who takes each item. Move each issue and review request to its new owner:

```bash
NEW_LOGIN=octocat2
ISSUE=https://github.com/a-novel/infra/issues/1
PR=https://github.com/a-novel/infra/pull/2
gh issue edit "$ISSUE" --remove-assignee "$LOGIN" --add-assignee "$NEW_LOGIN"
gh pr edit "$PR" --remove-reviewer "$LOGIN" --add-reviewer "$NEW_LOGIN"
```

The new owner of a pull request pushes to its branch, or closes it with a note that links its issue.

### Cover their roles

List the teams they belong to, with each team's member count:

```bash
for ORG in a-novel a-novel-kit; do
  gh api graphql -f org="$ORG" -f login="$LOGIN" -f query='
    query($org: String!, $login: String!) {
      organization(login: $org) {
        teams(first: 100, userLogins: [$login]) {
          nodes { slug members { totalCount } }
        }
      }
    }' --jq '.data.organization.teams.nodes[] | "\(.slug) \(.members.totalCount)"'
done
```

A `maintainers-` or `core` team at 1 would lose its last member. [Promote](./promote-member) someone into it first. Give each Discord repository role they hold to another member too, so CI alerts and review requests still reach someone.

## Remove their access

Run these on their last day, in this order.

### Ops: suspend their Google account

Suspending blocks their sign-in to Google, Google Cloud included. In the [Google Admin console](https://admin.google.com), open **Directory → Users**, then their account, then **Suspend user**.

### Remove them from GitHub

```bash
gh auth refresh -h github.com -s admin:org
for ORG in a-novel a-novel-kit; do
  gh api -X DELETE "orgs/$ORG/memberships/$LOGIN"
done
gh auth refresh -h github.com --remove-scopes admin:org
```

This drops every team and cancels a pending invitation. Their commits and reviews stay. Each organization now answers `Not Found`:

```bash
gh api "orgs/a-novel/members/$LOGIN"
gh api "orgs/a-novel-kit/members/$LOGIN"
```

### Remove their Discord roles

Untick every role they hold: `developers` or `ops`, their repository roles and the admin role. They keep the community channels, as any visitor does.

### Unlink their accounts

Print their GitHub ID:

```bash
gh api "users/$LOGIN" --jq .id
```

In the [Bitwarden web app](https://vault.bitwarden.eu), open **Secrets Manager → Projects → github-discord-map** and delete the secret with that name.

### Core: close their Bitwarden access

1. Under **Secrets Manager → Machine accounts**, open the account behind `repo update` and revoke their access token.
2. Remove them under **Admin Console → Members**.
3. Rotate every secret of the `a-novel` and `a-novel-kit` projects, since they could read them all. Replace each value with a new one from its source, then [apply the settings](./repositories#apply) with `--all`, which copies the new values to every repository. Delete each old App key or token only after that run.

### Ops: remove their production access

Find every line of [a-novel/infra](https://github.com/a-novel/infra) that names their address, from an up-to-date checkout. IAM lists and alert recipients both show up:

```bash
EMAIL=octocat@agorastoryverse.com
git -C ~/git-projects/a-novel/app/infra grep -n "$EMAIL" -- '*.tfvars'
```

In one pull request, delete their `user:` entries from the IAM lists and give each other line, such as an alert recipient, to another member. Then rotate the production secrets they could read, with the [infra secrets runbook](https://github.com/a-novel/infra/blob/master/docs/runbooks/secrets.md).

### Ops: delete their Google account

Wait for the infra merge to apply, so no binding names a deleted account. Then open their account in **Directory → Users** and choose **Delete user**. Google offers to transfer their Drive and Calendar data: give it to the member who took over their work.

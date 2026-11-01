# Add a member

A member joins as a developer or as ops. Developers work in the repositories. Ops also run production, so they get a Google account and cloud access on top.

Enroll a newcomer once the team agrees and they have [sent you their accounts](/dependencies/discord#link-your-accounts). Inviting members takes owner rights on both GitHub organizations.

## Set the member

Use the GitHub username they sent. Their GitHub numeric ID keys the account link, since it survives a username change.

```bash
LOGIN=octocat
GITHUB_ID=$(gh api "users/$LOGIN" --jq .id)
echo "$GITHUB_ID"
```

## Link their accounts

The `github-discord-map` project in Bitwarden Secrets Manager holds one secret per member, which CI reads to mention them on Discord. This step needs the [Bitwarden web app](https://vault.bitwarden.eu).

Open **Secrets Manager → Projects → github-discord-map**, then **New → Secret**. Set **Name** to their GitHub ID and **Value** to the Discord user ID they sent.

## Give the Discord role

In Discord, right-click their name in the member list, open **Roles** and tick `developers` or `ops`. The role opens the private working channels.

## Invite them on GitHub

Each repository class has an area team, `developers-<class>`, with push access to every repository of that class. Adding someone to a team invites them to its organization. List an organization's area teams:

```bash
gh api orgs/a-novel/teams --jq '.[].slug | select(startswith("developers-"))'
```

Add them to the area teams they work in, in both organizations. Ops also join `developers-infra`.

```bash
gh auth refresh -h github.com -s admin:org
for TEAM in developers-service developers-platform; do
  gh api -X PUT "orgs/a-novel/teams/$TEAM/memberships/$LOGIN" --jq .state
done
for TEAM in developers-library; do
  gh api -X PUT "orgs/a-novel-kit/teams/$TEAM/memberships/$LOGIN" --jq .state
done
gh auth refresh -h github.com --remove-scopes admin:org
```

Each team prints `pending` until they [accept the invitations](/dependencies/github#accept-your-invitations). Both lines print `active` once they have:

```bash
gh api "orgs/a-novel/memberships/$LOGIN" --jq .state
gh api "orgs/a-novel-kit/memberships/$LOGIN" --jq .state
```

## Ops: open their Google account

Ops sign in to Google Cloud with an `agorastoryverse.com` account. The ops organizational unit allows only passwordless sign-in with a security key. This step needs the [Google Admin console](https://admin.google.com).

1. Open **Directory → Users → Add new user**. Enter their name and address, and pick the ops organizational unit.
2. Hand them the temporary password through a private channel.
3. At first sign-in, they register their security key and its backup as passkeys, under **Google Account → Security → Passkeys and security keys**.

## Ops: grant production access

Production access is code in [a-novel/infra](https://github.com/a-novel/infra). Add `user:<their address>` to two lists in a pull request:

- `operator_principals` in `bootstrap/terraform.tfvars`, for the management plane and recovery.
- `database_operator_principals` in `environments/production/foundation/terraform.tfvars`, for the services' namespaces on the cluster and the workload project's logs.

The merge applies both. They then set up the [Google Cloud CLI](./gcloud) and [OpenTofu](./opentofu).

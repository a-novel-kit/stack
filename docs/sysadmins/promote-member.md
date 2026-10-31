# Promote a member

A member moves up to repository admin, to look after some repositories, or to core, to run both organizations. Promoting takes owner rights on both GitHub organizations.

```bash
LOGIN=octocat
```

## Repository admin

Repository admins approve the pull requests of their repositories and answer their alerts.

### Join the maintainers team

Each repository class has a `maintainers-<class>` team, with the Maintain role on every repository of that class. Maintain bypasses no ruleset. Their approval is what `review-gate` waits for.

```bash
ORG=a-novel
TEAM=maintainers-service
gh auth refresh -h github.com -s admin:org
gh api -X PUT "orgs/$ORG/teams/$TEAM/memberships/$LOGIN" --jq .state
gh auth refresh -h github.com --remove-scopes admin:org
```

It prints `active`.

### Give the repository roles

Discord has one role per repository, named `<org>/<repo>`. CI pings it when the repository's build fails or a pull request waits for a reviewer. Right-click their name in the member list, open **Roles** and tick the role of each repository they look after.

## Core

Core holds Admin on every repository of both organizations, ruleset bypass included. It approves releases, keeps the credentials and runs production.

### Check their setup

Core meets the [sysadmin security requirements](./#security-requirements). Every key these commands print starts with `sk-`, the mark of a security key:

```bash
gh api "users/$LOGIN/keys" --jq '.[].key'
gh api "users/$LOGIN/ssh_signing_keys" --jq '.[].key'
```

Core needs production access too. Give them the ops steps of [Add a member](./add-member#ops-open-their-google-account) first if they lack it.

### Join core

```bash
gh auth refresh -h github.com -s admin:org
for ORG in a-novel a-novel-kit; do
  gh api -X PUT "orgs/$ORG/teams/core/memberships/$LOGIN" --jq .state
done
gh auth refresh -h github.com --remove-scopes admin:org
```

Both lines print `active`.

### Make them a Discord admin

Give them the server's admin role, the one with the **Administrator** permission.

### Open Bitwarden to them

This step needs the [Bitwarden web app](https://vault.bitwarden.eu).

1. Invite them under **Admin Console → Members**, with Secrets Manager access.
2. Under **Secrets Manager → Projects**, open `a-novel`, `a-novel-kit` and `github-discord-map` in turn and give them write access on the **People** tab.

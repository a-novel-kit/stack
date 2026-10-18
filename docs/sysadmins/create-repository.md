# Create a repository

A new repository takes its settings from its class, joins the workspace and gets its own Discord role. Creating one takes owner rights on its organization.

## Set the repository

The name sets the class: `service-*` for a service, `platform-*` for a platform, and a library for most other names. A [class override](https://github.com/a-novel-kit/stack/tree/master/cli/internal/repocfg/templates#classesclassyaml-and-reposorg_repoyaml) in the stack sets any other class.

```bash
ORG=a-novel
NAME=service-example
```

## Create it

Services and platforms start from their template. Use `platform-template` for a platform, and leave `--template` out for anything else.

```bash
gh auth refresh -h github.com -s admin:org
a-novel repo create "$ORG" "$NAME" --template service-template
gh auth refresh -h github.com --remove-scopes admin:org
```

A repository without a template starts without CI or Renovate. The `write-github-actions` skill describes both, so an agent can add them.

## Install the release App

A repository that releases needs its organization's release App, `AnovelBot[Release]` or `AnovelKitBot[Release]`. Its rulesets fail with HTTP 422 until then. In the organization's **Settings → GitHub Apps**, configure the App and add the repository under **Repository access**.

## Add it to the workspace

Add `<org>/<name>` to the stack's `workspace-repos.yaml` in a pull request. Once it merges, pull the stack and clone the new repository:

```bash
git -C ~/git-projects/a-novel pull --ff-only
a-novel core sync
```

Then [apply its settings](./repositories#apply) from its new checkout, under `app/` or `kit/`.

## Create its Discord role

CI mentions a repository through its own role.

1. In Discord, open **Server Settings → Roles → Create Role**. Name it exactly `<org>/<name>` and save.
2. With [Developer Mode](/dependencies/discord#link-your-accounts) on, right-click the role and choose **Copy Role ID**.

Add the role to the organization's `DISCORD_REPO_ROLES` variable, which maps each repository to its role:

```bash
ROLE_ID=123456789012345678
gh auth refresh -h github.com -s admin:org
ROLES=$(gh api "orgs/$ORG/actions/variables/DISCORD_REPO_ROLES" \
  --jq ".value | fromjson | .[\"$ORG/$NAME\"] = \"$ROLE_ID\" | tojson")
gh variable set DISCORD_REPO_ROLES --org "$ORG" --visibility all --body "$ROLES"
gh variable get DISCORD_REPO_ROLES --org "$ORG"
gh auth refresh -h github.com --remove-scopes admin:org
```

`gh variable get` prints the map, the new repository included. Give its role to the repository's admins, as in [Promote a member](./promote-member#give-the-repository-roles).

## Tag it

Topics help outsiders find the repository. Start with `agora-storyverse`, then add its language, kind and domain:

```bash
gh repo edit "$ORG/$NAME" --add-topic agora-storyverse,go,golang,microservice
```

# Create a repository

A new repository takes its settings from its class, joins the workspace and gets its own Fluxer role and channels. Creating one is a pull request to a-novel/infra. The release App, the Fluxer role and the channels take owner rights on its organization and on the Fluxer community.

## Set the repository

Later steps read its organization and name:

```bash
ORG=a-novel
NAME=service-example
```

## Declare it

Add the repository to `repositories` in a-novel/infra's `environments/production/github/terraform.tfvars`, in a pull request. Its class sets its settings: `service`, `platform`, `library`, `workflows`, `meta` or `infra`. The description names its role in one sentence, never the contents that will change: "The public profile of Agora users.", not the fields a profile holds today. Topics help outsiders find it: start with `agora-storyverse`, then add its language, kind and domain.

```hcl
"a-novel/service-example" = {
  class       = "service"
  description = "Names the service's role, in one sentence."
  topics      = ["agora-storyverse", "go", "golang", "microservice"]
}
```

The merge's deploy creates the repository, with its settings, team access and labels. A service or a platform starts from its template repository, `service-template` or `platform-template`. Any other class starts empty, without CI, Renovate or the governance workflows. The `write-github-actions` skill describes all three, so an agent can add them.

Both organizations keep no default repository labels, so the deploy creates every label itself, `dependencies` included, before Renovate's first pull request. GitHub copies an organization's default labels into each new repository, where the root fails to create its own labels of the same names, with `already_exists`. When an apply fails that way, defaults came back: delete them under the organization's **Settings → Repository → Repository defaults → Repository labels**, import every name the failed apply lists in a second pull request, as [a-novel/infra#1379](https://github.com/a-novel/infra/pull/1379) does, and drop the import once it applied.

## Install the release App

A repository that releases needs its organization's release App, `AnovelBot[Release]` or `AnovelKitBot[Release]`. Do it as soon as the deploy has created the repository. Its rulesets name the App, and GitHub refuses them with HTTP 422 until it is installed. A repository made from a template holds `main.yaml` from the start, so the next apply already creates them, and the GitHub root fails until the install.

In the organization's **Settings → GitHub Apps**, configure the App and add the repository under **Repository access**.

The command version needs a personal access token: GitHub refuses `gh`'s own sign-in on this endpoint. Create a classic one with the `repo` scope in the browser, under **Settings → Developer settings → Personal access tokens → Tokens (classic)**, paste it at the prompt, and delete it afterwards:

```bash
INSTALLATION=$(gh api "orgs/$ORG/installations" \
  --jq '.installations[] | select(.app_slug | endswith("-release")) | .id')
REPO_ID=$(gh api "repos/$ORG/$NAME" --jq .id)
read -rs PAT
GH_TOKEN=$PAT gh api -X PUT \
  "user/installations/$INSTALLATION/repositories/$REPO_ID"
unset PAT
```

## Add it to the workspace

Add `<org>/<name>` to the stack's `workspace-repos.yaml` in a pull request. Once it merges, pull the stack and clone the new repository:

```bash
git -C ~/git-projects/a-novel pull --ff-only
a-novel core sync
```

The repository has no ruleset until its default branch holds `.github/workflows/main.yaml`, so its first workflows merge without required checks. Within 10 minutes of that merge, the GitHub root's sweep gives it its rulesets and environments, and copies the governance keys its workflows name. Until then, every governance check fails on a missing key.

## Give it the release key

The release workflow signs in as the release App with a key that only the repository's `release` environment holds. Once the sweep created that environment, open the App's settings in the organization, generate a key under **Private keys**, and set it:

```bash
KEY_FILE=~/Downloads/anovelkitbot-release.2026-10-22.private-key.pem
gh secret set RELEASE_BOT_PRIVATE_KEY --env release --repo "$ORG/$NAME" < "$KEY_FILE"
rm "$KEY_FILE"
```

The App keeps its other keys, so every other repository keeps releasing.

## Create its Fluxer role

CI mentions a repository through its own role.

1. In Fluxer, open **Community settings → Roles → Create role**. Name it exactly `<org>/<name>` and save.
2. Right-click the role and choose **Copy role ID**.

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

## Create its Fluxer channels

A repository posts its pull requests and its issues to channels of its own. Until it has them, both go to the organization's `github-reviews` and `github-issues`.

1. In Fluxer, create `<name>-reviews` in the organization's **Pull Requests** category and `<name>-issues` in its **Issues** category, so each takes the category's permissions.
2. On each channel, open **Edit channel → Webhooks → Create webhook**. Expand the new webhook and choose **Copy webhook URL**, which starts with `https://api.fluxer.app/webhooks/`.
3. Set each webhook as a secret of the repository. A repository secret wins over the organization's of the same name, and changing a channel later sets that one secret again. Each command prompts for its webhook:

   ```bash
   gh secret set REVIEWS_WEBHOOK --repo "$ORG/$NAME"
   gh secret set ISSUES_WEBHOOK --repo "$ORG/$NAME"
   ```

The next pull request event posts to the new reviews channel. Issues reach their channel through the repository's `notify-post.yaml`, which the `.github` repository's issues feed dispatches.

## Watch it in Grafana

A service or a platform joins Grafana's Errors, Health and Performance dashboards on its first deploy. Its alert thresholds and its tab on the Services dashboard come in an infra pull request, as infra's [contributing guide](https://github.com/a-novel/infra/blob/master/CONTRIBUTING.md#rules) describes.

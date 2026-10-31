# Create a repository

A new repository takes its settings from its class, joins the workspace and gets its own Discord role. Creating one is a pull request to a-novel/infra. The release App and the Discord role take owner rights on its organization.

## Set the repository

Later steps read its organization and name:

```bash
ORG=a-novel
NAME=service-example
```

## Declare it

Add the repository to `repositories` in a-novel/infra's `environments/production/github/terraform.tfvars`, in a pull request. Its class sets its settings: `service`, `platform`, `library`, `workflows`, `meta` or `infra`. Topics help outsiders find it: start with `agora-storyverse`, then add its language, kind and domain.

```hcl
"a-novel/service-example" = {
  class       = "service"
  description = "Says what the service does, in one sentence."
  topics      = ["agora-storyverse", "go", "golang", "microservice"]
}
```

The merge's deploy creates the repository, with its settings, team access and labels. A service or a platform starts from its template repository, `service-template` or `platform-template`. Any other class starts empty, without CI, Renovate or the governance workflows. The `write-github-actions` skill describes all three, so an agent can add them.

## Install the release App

A repository that releases needs its organization's release App, `AnovelBot[Release]` or `AnovelKitBot[Release]`. In the organization's **Settings → GitHub Apps**, configure the App and add the repository under **Repository access**. Do it before a workflow naming the `release` environment reaches the default branch: the rulesets that follow name the App, and GitHub refuses them with HTTP 422 until it is installed.

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

## Connect it to Sentry

A service reports its faults, traces and logs to a Sentry project of its own. A platform takes step 1 and skips the others: its server traces and its visitors' consented browser telemetry need no token. Every other repository skips this section.

1. An infra pull request adds the repository to `projects` in `environments/production/sentry/terraform.tfvars`. Its deploy creates the Sentry project, named after the repository, with spike protection. Never create a project by hand: the Sentry root owns them all.
2. In the organization's **Settings → GitHub Apps**, configure **Sentry** and add the repository under **Repository access**. Sentry reads each release's commits through it.
3. Copy the Sentry organization token `release-ci` from [Bitwarden](./bitwarden), and paste it at the prompt:

   ```bash
   gh secret set SENTRY_AUTH_TOKEN --env release --repo "$ORG/$NAME"
   ```

The service's DSN, OTLP header and project ID need no copying: once step 1 has deployed, the service root reads them from the Sentry root, as the `write-infra` skill describes. A DSN is not a secret, so none of them goes to Secret Manager.

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

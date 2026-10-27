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

`repo create` configures the repository before it holds any workflow, so it creates no environment. Until the settings are applied again, every governance check fails on a missing key. Apply them as soon as a checkout holds the first `main.yaml` and `release.yaml`, even on its pull request branch. Discovery reads the working tree, so one run creates the `governance` and `release` environments and requires the new jobs.

## Give it the release key

The release workflow signs in as the release App with a key that only the repository's `release` environment holds. Once the settings created that environment, open the App's settings in the organization, generate a key under **Private keys**, and set it:

```bash
KEY_FILE=~/Downloads/anovelkitbot-release.2026-10-22.private-key.pem
gh secret set RELEASE_BOT_PRIVATE_KEY --env release --repo "$ORG/$NAME" < "$KEY_FILE"
rm "$KEY_FILE"
```

The App keeps its other keys, so every other repository keeps releasing.

## Connect it to Sentry

A service reports its faults, traces and logs to a Sentry project of its own. A platform sends its server traces only, so it takes step 1 and skips the others: it runs no Sentry SDK, and browser telemetry waits for counsel's ruling on consent ([a-novel/.github#399](https://github.com/a-novel/.github/issues/399)). Every other repository skips this section.

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

## Tag it

Topics help outsiders find the repository. Start with `agora-storyverse`, then add its language, kind and domain:

```bash
gh repo edit "$ORG/$NAME" --add-topic agora-storyverse,go,golang,microservice
```

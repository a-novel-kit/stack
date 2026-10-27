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

A service reports its faults, traces and logs to a Sentry project of its own. A platform skips this section until counsel rules on browser telemetry ([a-novel/.github#399](https://github.com/a-novel/.github/issues/399)), and so does every other repository.

1. An infra pull request adds the repository to `projects` in `environments/production/sentry/terraform.tfvars`. Its deploy creates the Sentry project, named after the repository, with spike protection. Never create a project by hand: the Sentry root owns them all.
2. In the organization's **Settings → GitHub Apps**, configure **Sentry** and add the repository under **Repository access**. Sentry reads each release's commits through it.
3. Copy the Sentry organization token `release-ci` from [Bitwarden](./bitwarden), and paste it at the prompt:

   ```bash
   gh secret set SENTRY_AUTH_TOKEN --env release --repo "$ORG/$NAME"
   ```

4. An infra pull request creates the service's DSN and OTLP header containers, as the `write-infra` skill describes. Once it deploys, sign in to `gcloud` first: a piped `gcloud secrets versions add` cannot prompt you to sign in again. Copy the DSN from the project's **Settings → Client Keys (DSN)**, and paste it at the prompt:

   ```bash
   SECRET="production-${NAME#service-}-sentry-dsn"
   read -rs DSN
   printf '%s' "$DSN" | gcloud secrets versions add "$SECRET" \
     --project=a-novel-management-prod --data-file=-
   unset DSN
   ```

   Then paste the DSN's public key, the part between `https://` and `@`. The command writes the header around it:

   ```bash
   SECRET="production-${NAME#service-}-sentry-otlp-headers"
   read -rs KEY
   printf 'x-sentry-auth=sentry sentry_key=%s' "$KEY" | gcloud secrets versions add "$SECRET" \
     --project=a-novel-management-prod --data-file=-
   unset KEY
   ```

   Each prints `Created version [1]`, the version infra pins. Add both before the pull request that pins them merges: a deploy that pins a missing version fails, and Cloud Run keeps the old revision.

5. The service root's `sentry_project_id` input takes the project's ID, the number at the end of its DSN.

[a-novel/infra#1154](https://github.com/a-novel/infra/issues/1154) moves steps 4 and 5 into code.

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

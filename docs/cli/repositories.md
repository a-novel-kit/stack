# Configure repositories

::: info Repository administrators
Writing settings takes admin rights on the repositories and the `admin:org` scope on your `gh` token. Anyone can preview them.
:::

Every repository's GitHub settings are code: its features and merge rules, security options, Pages, rulesets and required checks, plus the governance workflows, labels and code owners it carries. The stack derives them from templates in [`cli/internal/repocfg/templates`](https://github.com/a-novel-kit/stack/tree/master/cli/internal/repocfg/templates), from three inputs: the preset of the repository's class, or its own override; its organization's profile; and the checks its workflows declare.

`a-novel repo update` reconciles a repository to that derived state: it resets every setting the templates name and deletes every ruleset they do not produce, so a change made by hand in GitHub does not last. To change a setting, change the templates through a pull request, then apply.

## Preview

Work from a checkout of the repository on its default branch. Set its directory, then preview the API calls an update would make:

```bash
REPO_DIR=~/git-projects/a-novel/app/service-json-keys
cd "$REPO_DIR"
a-novel repo update --dry-run
```

## Apply

Besides the repository's own settings, `repo update` chooses which repositories can read the organization's scoped secrets, and that takes the `admin:org` scope on your `gh` token. Grant it for the run, then give it back:

```bash
gh auth refresh -h github.com -s admin:org
a-novel repo update
gh auth refresh -h github.com --remove-scopes admin:org
```

Without the scope, the secret grants fail with HTTP 403 while everything else applies; run the update again once you have it.

The command shows the plan and waits for your confirmation before writing anything; only a person runs it. To reconcile the whole workspace, run it from anywhere with `--all`:

```bash
a-novel repo update --all --dry-run
a-novel repo update --all
```

`--all` covers the stack and every repository under `app/` and `kit/`. It skips, untouched, a checkout that is off its default branch or holds uncommitted changes, because its working tree would not reflect the merged configuration. One confirmation covers the batch, and `--exclude` leaves named repositories out.

The CLI reads the templates from your stack checkout, so a merged template change applies without a rebuild. A change that adds a new field needs `a-novel install` first, since older binaries refuse fields they do not know.

## Create a repository

`repo create` creates the repository, from an organization template when one fits, and applies its class configuration in one step:

```bash
ORG=a-novel
NAME=service-example
CLASS=service
a-novel repo create "$ORG" "$NAME" --class "$CLASS" --template service-template
```

The class is one of `service`, `platform`, `library`, `infra`, `workflows` or `meta`. Repositories are public unless you pass `--private`. Add the new repository to the stack's `workspace-repos.yaml` so `core sync` clones it.

## Store the Bitwarden token

`repo update` provisions the organizations' privileged keys from Bitwarden Secrets Manager, with the access token from the [Bitwarden](/dependencies/bitwarden#configure) page. Store the token in the CLI's encrypted store; it is read with no echo:

```bash
a-novel secrets set BWS_ACCESS_TOKEN
```

# Configure repositories

Repository settings are code: [templates](https://github.com/a-novel-kit/stack/tree/master/cli/internal/repocfg/templates) in the stack define them for every repository. `a-novel repo update` applies them and reverts manual changes, so change settings through the templates. Anyone can preview; applying takes admin rights.

## Preview

From the repository's default branch:

```bash
REPO_DIR=~/git-projects/a-novel/app/service-json-keys
cd "$REPO_DIR"
a-novel repo update --dry-run
```

## Apply

Granting organization secrets needs the `admin:org` scope; add it for the run only:

```bash
gh auth refresh -h github.com -s admin:org
a-novel repo update
gh auth refresh -h github.com --remove-scopes admin:org
```

Without the scope, those grants fail with HTTP 403 and the rest applies.

`--all` covers the whole workspace, and skips checkouts off their default branch or with uncommitted changes:

```bash
a-novel repo update --all --dry-run
a-novel repo update --all
```

Template changes apply without a rebuild, except a new field, which needs `a-novel install` first.

## Store the Bitwarden token

Store the [Bitwarden](./bitwarden#configure) access token once; `repo update` reads it from there:

```bash
a-novel secrets set BWS_ACCESS_TOKEN
```

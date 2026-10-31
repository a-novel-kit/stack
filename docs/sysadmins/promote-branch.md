# Promote a branch

Promoting a branch makes a long-lived branch, such as a rebuild, the repository's `master`. The old `master` lives on under another name. A promotion takes admin rights on the repository.

## Set the branches

The new line is the branch you promote, and `NEW_LINE` holds its name. The old line is the current `master`, renamed to `OLD_LINE`.

```bash
ORG=a-novel
NAME=service-example
NEW_LINE=v1
OLD_LINE=v0
```

## Rename the old line

```bash
gh api -X POST "repos/$ORG/$NAME/branches/master/rename" \
  -f new_name="$OLD_LINE" --jq .name
```

The default branch moves to the old line. So do the pull requests open on `master`.

## Create the new master

GitHub now redirects `master` to the old line. Renaming the new line to `master` fails with HTTP 422 "New branch already exists". Create `master` at the new line's head instead:

```bash
SHA=$(gh api "repos/$ORG/$NAME/git/ref/heads/$NEW_LINE" --jq .object.sha)
gh api -X POST "repos/$ORG/$NAME/git/refs" \
  -f ref=refs/heads/master -f sha="$SHA" --jq .ref
```

## Make `master` the default

```bash
gh api -X PATCH "repos/$ORG/$NAME" -f default_branch=master \
  --jq .default_branch
```

Rulesets that target the default branch follow it to `master`. The old line keeps only the rules for every branch, such as the commit-message check. It freezes once `master` releases a later major: the GitHub root then [locks](./repositories#follow-the-code) every `v<N>` branch below the major in `master`'s root `package.json`.

## Delete the new line

A merge into the new line after you created `master` exists only on the new line. Check that both refs print the same commit, and that no pull request is open on the new line:

```bash
gh api "repos/$ORG/$NAME/git/ref/heads/master" --jq .object.sha
gh api "repos/$ORG/$NAME/git/ref/heads/$NEW_LINE" --jq .object.sha
gh pr list --repo "$ORG/$NAME" --base "$NEW_LINE"
```

Point each pull request listed at `master` with `gh pr edit <number> --repo "$ORG/$NAME" --base master`. Then delete the new line:

```bash
gh api -X DELETE "repos/$ORG/$NAME/git/refs/heads/$NEW_LINE"
```

## Reset your checkout

The new `master` shares no history with the old one. `a-novel core sync` skips your checkout as diverged. Point your local `master` at the new one:

```bash
REPO_DIR=~/git-projects/a-novel/app/service-example
cd "$REPO_DIR"
git fetch --prune origin
git switch -C master origin/master
```

Ask every contributor with a checkout of the repository to run the same commands.

## Wait for the required checks

The `master` ruleset still requires the jobs in the old line's `main.yaml`, until the GitHub root [reads the new one](./repositories#follow-the-code) within 10 minutes. Until then, a pull request blocks on each job the new line dropped.

## Run Renovate

Renovate reads its configuration from the default branch. A `baseBranchPatterns` entry that pointed it at the new line stays on the old line and no longer applies. Dispatch Renovate so it opens its pull requests on the new `master` now:

```bash
gh workflow run renovate.yaml --repo "$ORG/$NAME"
```

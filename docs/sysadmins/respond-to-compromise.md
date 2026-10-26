# Respond to a compromise

A compromise puts an account, a key or a token in someone else's hands: a lost security key, a hijacked GitHub or Google account, a leaked bot key or token, or a machine that ran malware. Cut the attacker off first. Then find what they did, repair what they published and restore access. The steps take owner rights on both GitHub organizations, and the Google steps a Workspace admin.

Work from a clean machine. A machine that ran malware gave away every credential it held. Wipe it, and set it up again only once you [restore access](#restore-and-close).

## Choose the steps

| What happened                                                       | Steps                                                                                                                                                                                 |
| ------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| A security key is lost                                              | [Remove its SSH keys](#remove-the-ssh-and-signing-keys) and [its passkey](#lock-the-google-account), then [enrol a replacement](#re-enrol-the-security-keys)                          |
| A bot key or token leaked                                           | [Freeze](#freeze-production), [halt the bots](#halt-the-bots), rotate what leaked, then [investigate](#investigate) with the bot's login                                              |
| A Google account was used by someone else                           | [Freeze](#freeze-production), [lock the account](#lock-the-google-account), [rotate the production secrets](#rotate-the-production-secrets), then check [Google Cloud](#google-cloud) |
| A GitHub account was used by someone else, or a machine ran malware | Every step, in order                                                                                                                                                                  |

Set the account and the suspect window. `SINCE` is the first day the attacker could have held the account. When you cannot tell, take the day 180 days ago, the oldest GitHub still logs.

```bash
LOGIN=octocat
EMAIL=octocat@agorastoryverse.com
SINCE=2026-04-24
```

For a bot, quote its login: `LOGIN='anovelbot-release[bot]'`.

## Contain

Run these steps as fast as you can, in order.

### Freeze production

```bash
gh workflow disable deploy.yaml --repo a-novel/infra
```

Merges go on, but none reaches production until you [resume deploys](#lift-the-halt-and-resume-deploys). The infra runbook's [Freeze production](https://github.com/a-novel/infra/blob/master/docs/runbooks/deployments.md#freeze-production) lists what else waits.

### Cut the GitHub account off

For another member's account, [remove them from GitHub](./offboard-member#remove-them-from-github). Their access to both organizations ends at once. They then secure their own account with the steps below, and [come back](#bring-the-member-back) afterwards.

For your own account, open [GitHub's settings](https://github.com/settings/profile) in the browser:

1. Under **Sessions**, revoke every session but this one.
2. Under **Password and authentication**, change the password. Remove each passkey, security key or authenticator app you do not hold, then regenerate the recovery codes.
3. Under **Applications → Authorized OAuth Apps**, revoke **GitHub CLI**. That kills every `gh` token on every machine. Revoke any other App you do not recognize there and under **Authorized GitHub Apps**.
4. Under **Developer settings → Personal access tokens**, delete every token, fine-grained and classic. Note the ones to recreate, such as your [packages token](/dependencies/node#configure) and the infra [GitHub dispatch tokens](https://github.com/a-novel/infra/blob/master/docs/runbooks/secrets.md#github-dispatch-tokens).

Then [sign `gh` in](/dependencies/github#sign-in) again. If you can no longer sign in to GitHub, recover the account through [GitHub Support](https://support.github.com).

### Remove the SSH and signing keys

List the account's keys, with the scopes that manage them:

```bash
gh auth refresh -h github.com -s admin:public_key,admin:ssh_signing_key
FIELDS='.[] | [.id, .title, (.key | split(" ")[0])] | @tsv'
gh api user/keys --jq "$FIELDS"
gh api user/ssh_signing_keys --jq "$FIELDS"
```

Delete the keys of the lost security key or of the compromised machine. Delete every key you cannot match to a security key in your hands too. A sysadmin's key type always starts with `sk-`. Delete each key from the list that printed it:

```bash
KEY_ID=12345678
gh api -X DELETE "user/keys/$KEY_ID"              # authentication key
gh api -X DELETE "user/ssh_signing_keys/$KEY_ID"  # signing key
```

Drop the scopes once both lists hold only your keys:

```bash
gh auth refresh -h github.com --remove-scopes admin:public_key,admin:ssh_signing_key
```

### Halt the bots

A leaked bot key lets the attacker open pull requests and post checks as the bots. A machine that held the Bitwarden token leaked those keys. Halt the Agent automation until the keys are rotated: both organizations then hold every pull request.

```bash
gh auth refresh -h github.com -s admin:org
for ORG in a-novel a-novel-kit; do
  gh variable set AGENT_KILL_SWITCH --org "$ORG" --visibility all --body on
done
gh auth refresh -h github.com --remove-scopes admin:org
```

### Revoke the Bitwarden tokens

Each core member keeps a token for `repo update` on their machine. The token opens every secret of the `a-novel` and `a-novel-kit` Bitwarden projects, the Agent and Dependencies App keys included. In the [Bitwarden web app](https://vault.bitwarden.eu):

1. Under **Secrets Manager → Machine accounts**, open the account behind `repo update` and revoke the token of the compromised machine. When you cannot tell which token leaked, revoke them all, then [give each core member a new one](./promote-member#open-bitwarden-to-them).
2. For your own account, open **Settings → My account → Danger zone** and choose **Deauthorize sessions**. Then change your master password.

### Rotate the App keys

Each organization has three Apps whose private keys act on every repository:

| App          | Its key lives in                                       | Its key can                                              |
| ------------ | ------------------------------------------------------ | -------------------------------------------------------- |
| Agent        | Bitwarden, copied into each `governance` environment   | post the required checks, approve bot changes            |
| Dependencies | Bitwarden, copied into each `governance` environment   | open pull requests that need no approval                 |
| Release      | each releasing repository's `release` environment only | write `master`, release lines and tags past the rulesets |

Rotate each key that leaked. A leaked Bitwarden token leaked the Agent and Dependencies keys.

1. In the organization's **Settings → Developer settings → GitHub Apps**, choose **Edit** next to the App. Under **Private keys**, generate a new key, then delete the old one. Deleting the old key cuts the attacker off at once. The App fails until its new key lands.
2. For Agent and Dependencies, store the new key in the organization's Bitwarden project under its name, `AGENT_BOT_PRIVATE_KEY` or `DEPENDENCY_BOT_PRIVATE_KEY`. After a leaked Bitwarden token, replace every other secret of both projects as well, as [offboarding](./offboard-member#core-close-their-bitwarden-access) does. Then [apply the settings](./repositories#apply) with `--all`, which copies the new values into every repository.
3. For Release, set the new key in every repository of the organization that has a `release` environment. `gh` names each one it sets. Run it once per organization, with that organization's key:

   ```bash
   ORG=a-novel
   KEY_FILE=~/Downloads/anovelbot-release.2026-10-21.private-key.pem
   gh repo list "$ORG" --limit 200 --json name --jq '.[].name' |
     while read -r REPO; do
       gh api "repos/$ORG/$REPO/environments/release" --silent 2>/dev/null &&
         gh secret set RELEASE_BOT_PRIVATE_KEY --env release \
           --repo "$ORG/$REPO" < "$KEY_FILE"
     done
   ```

4. Delete the downloaded key file.

### Lock the Google account

In the [Google Admin console](https://admin.google.com), open **Directory → Users**, then the account.

For another member's account, choose **Suspend user**. Suspension blocks every sign-in, Google Cloud included.

For your own account, or one that stays active, open **Security**:

1. Under **Sign-in cookies**, choose **Reset**. The account is signed out everywhere.
2. Under **Passkeys & security keys**, remove each key its owner does not hold, the lost one included.
3. Under **Connected applications**, remove every application. That revokes the `gcloud` and OpenTofu sign-ins, which you [set up again](#set-up-the-credentials-again) later.

### Rotate the production secrets

An ops account reads every production secret. When the attacker held one, or a machine signed in to `gcloud` with one, rotate the secrets with the infra [secrets runbook](https://github.com/a-novel/infra/blob/master/docs/runbooks/secrets.md#rotate-a-secret), and revoke each old value where it was issued. Start with the secrets that the [access log](https://github.com/a-novel/infra/blob/master/docs/runbooks/secrets.md#audit-access) shows the account reading. The database password waits for a planned downtime.

### Rotate the Sentry keys

Sentry issues two kinds of keys:

| Key                                 | It lives in                                                              | It can                           |
| ----------------------------------- | ------------------------------------------------------------------------ | -------------------------------- |
| The organization token `release-ci` | Bitwarden, copied into each `release` environment as `SENTRY_AUTH_TOKEN` | record releases in every project |
| A project's DSN                     | the service's `production-<service>-sentry-dsn` secret                   | send events to that project      |

Rotate each key that leaked. Revoking the old key cuts the attacker off at once. Sentry then records no new release, or no event from the service, until the new key lands.

1. For the organization token, open **Settings → Developer Settings → Organization Tokens** in Sentry. Revoke `release-ci`, create a new token with the same name, and store it in Bitwarden in place of the old one. Then set it in every repository whose `release` environment holds the token, pasting it at the prompt. `gh` names each one it sets:

   ```bash
   read -rs TOKEN
   for ORG in a-novel a-novel-kit; do
     gh repo list "$ORG" --limit 200 --json name --jq '.[].name' |
       while read -r REPO; do
         gh secret list --env release --repo "$ORG/$REPO" --json name \
           --jq '.[].name' 2>/dev/null | grep -qx SENTRY_AUTH_TOKEN &&
           printf '%s' "$TOKEN" | gh secret set SENTRY_AUTH_TOKEN \
             --env release --repo "$ORG/$REPO"
       done
   done
   unset TOKEN
   ```

2. For a DSN, open the project's **Settings → Client Keys (DSN)** in Sentry. Choose **Generate New Key**, then revoke the old key. Rotate the service's `production-<service>-sentry-dsn` with the infra [secrets runbook](https://github.com/a-novel/infra/blob/master/docs/runbooks/secrets.md#rotate-a-secret): add the new DSN as a version, then pin it in the root's `secret_versions.sentry-dsn`.

## Investigate

Find everything the attacker did since `SINCE`. GitHub's audit log keeps 180 days of events, and Google Cloud keeps 400 days of admin activity. Older events are gone, so save every output in a folder of its own as you go:

```bash
mkdir -p ~/incident && cd ~/incident
```

The `audit` function below lists one action of the account across both organizations. Keep each query to one action and one account: both organizations log thousands of events a day, and the audit log answers 1,750 queries an hour.

```bash
gh auth refresh -h github.com -s read:audit_log
audit() {
  for ORG in a-novel a-novel-kit; do
    gh api --paginate -X GET "orgs/$ORG/audit-log" -F per_page=100 \
      -f phrase="actor:$LOGIN action:$1 created:>=$SINCE" --jq "$2"
  done
}
```

Some checks go through every repository instead. List them once:

```bash
for ORG in a-novel a-novel-kit; do
  gh repo list "$ORG" --limit 200 --json nameWithOwner \
    --jq '.[].nameWithOwner'
done > repos.txt
```

### Ruleset bypasses and tags

Every write that skips a ruleset leaves a `protected_branch.policy_override` event. Tags leave one too, since only the release App and admins may create a tag.

```bash
audit protected_branch.policy_override \
  '.[] | [(.created_at / 1000 | todate), .repo, .branch,
    (.overridden_codes | join(",")), .referrer // .after] | @tsv' |
  tee bypasses.tsv
```

Three shapes are routine:

| Ref                                   | Skipped rules                                                          | Last column    | Routine when                                                 |
| ------------------------------------- | ---------------------------------------------------------------------- | -------------- | ------------------------------------------------------------ |
| `refs/heads/master`                   | `merge_queue`                                                          | a pull request | an admin merged a pull request you know                      |
| `refs/heads/master`                   | `required_status_checks`, `merge_queue`, `review_policy_not_satisfied` | a commit       | the commit is a `ci: sync managed config` from `repo update` |
| `refs/tags/v*`, plus a version commit | none on the tag                                                        | a commit       | the release App made it during a release you know            |

Read a commit's message:

```bash
REPO=a-novel/service-json-keys
SHA="<commit from the last column>"
gh api "repos/$REPO/commits/$SHA" --jq .commit.message
```

Every other event is the attacker's until proven otherwise: a tag a person created, a write to another branch, a merge nobody remembers. Revert each such write in a pull request, and carry each tag to [Recover published artifacts](#recover-published-artifacts).

### Workflow runs and releases

Any workflow run can publish its repository's packages, a branch's run included. A push alone can therefore publish an npm version or move an image tag. The audit log records every publish as `github-actions[bot]`, so trace them through the runs the account started instead. The last line counts those runs by event, branch and workflow:

```bash
while read -r REPO; do
  gh run list --repo "$REPO" --user "$LOGIN" --created ">=$SINCE" \
    --limit 1000 --json createdAt,event,headBranch,workflowName,url \
    --jq '.[] | [.createdAt, .event, .headBranch, .workflowName, .url] | @tsv'
done < repos.txt | tee runs.tsv
cut -f2-4 runs.tsv | sort | uniq -c
```

A release is a `release.yaml` run that someone dispatched, then approved at the `release` environment. List the jobs the account approved:

```bash
audit workflows.approve_workflow_job \
  '.[] | [(.created_at / 1000 | todate), .repo, .workflow_run_id] | @tsv' |
  tee approvals.tsv
```

Then list every release of the window with its author:

```bash
while read -r REPO; do
  gh api --paginate "repos/$REPO/releases?per_page=100" \
    --jq '.[] | [.created_at, .author.login, .html_url] | @tsv'
done < repos.txt | awk -v since="$SINCE" '$1 >= since' | tee releases.tsv
```

Each branch must be one you know, and each dispatch, approval and release one you remember. A genuine release comes from the release App. Carry every other one to [Recover published artifacts](#recover-published-artifacts).

### Package versions

A run on an unknown branch may have published a version with a release's name. A version is genuine only with its release proof: an attestation from its tag's `release.yaml` run, and an immutable Release of that tag from the release App. Production checks both for every image it deploys. For an image, the first command succeeds and the second prints the release App's login and `true`:

```bash
REPO=a-novel/service-json-keys
TAG=v2.11.0
IMAGE="ghcr.io/a-novel/service-json-keys/grpc:$TAG"
gh attestation verify "oci://$IMAGE" --repo "$REPO" \
  --signer-workflow "$REPO/.github/workflows/release.yaml" \
  --source-ref "refs/tags/$TAG" --deny-self-hosted-runners
gh api "repos/$REPO/releases/tags/$TAG" --jq '"\(.author.login) \(.immutable)"'
```

An npm version takes the same attestation check on its tarball, downloaded with your [packages token](/dependencies/node#configure). A version published before October 2026 carries no attestation, so its Release alone vouches for it.

```bash
PACKAGE=@a-novel/service-json-keys-rest
TARBALL=$(npm pack "$PACKAGE@${TAG#v}" --silent \
  --registry=https://npm.pkg.github.com)
gh attestation verify "$TARBALL" --repo "$REPO" \
  --signer-workflow "$REPO/.github/workflows/release.yaml" \
  --source-ref "refs/tags/$TAG" --deny-self-hosted-runners
```

### Secrets, settings and access

```bash
for ACTION in environment repo org team repository_ruleset hook \
  integration_installation personal_access_token; do
  audit "$ACTION" \
    '.[] | [(.created_at / 1000 | todate), .action, .repo // .org] | @tsv'
done | tee changes.tsv
```

Each secret or variable change, environment or ruleset edit, new member, webhook, App or token grant needs an owner you know. [Apply the settings](./repositories#apply) with `--all` to undo unknown changes to the rulesets and environments, which the templates define. Undo every other change by hand.

Drop the scope once the GitHub queries are done:

```bash
gh auth refresh -h github.com --remove-scopes read:audit_log
```

### Google Cloud

List what the Google account did in both projects:

```bash
FILTER="protoPayload.authenticationInfo.principalEmail=\"$EMAIL\""
FILTER="$FILTER AND timestamp>=\"${SINCE}T00:00:00Z\""
for PROJECT in a-novel-management-prod a-novel-production-prod; do
  gcloud logging read "$FILTER" --project="$PROJECT" \
    --format='table(timestamp,protoPayload.methodName,protoPayload.resourceName)'
done | tee gcloud.txt
```

Data access entries, such as secret reads, stay 30 days. The Admin console's **Reporting → Audit and investigation → User log events** lists the account's sign-ins, with their IP addresses.

## Recover published artifacts

Production deploys only images with their release proof. Developers, CI and outside users install by version or tag, so repair each bad version you found. A bad release that reached production ships its fix [as a hotfix](https://github.com/a-novel/infra/blob/master/docs/runbooks/deployments.md#fix-a-bad-release).

### Go modules

The Go proxy keeps every version it has served, forever. A version the attacker tagged stays downloadable, so retract it: `go get` then skips it. A moved tag cannot change a version the proxy already served, since the checksum database pins its content.

1. In a pull request to the module's repository, add the version to its `go.mod`, with the reason:

   ```text
   retract v1.4.2 // Published from a compromised account.
   ```

2. Once it merges, release a patch. The retraction takes effect with the release that carries it, which waits for your approval at the `release` environment:

   ```bash
   REPO=a-novel-kit/golib
   gh workflow run release.yaml --repo "$REPO" -f release_type=patch
   ```

3. Once the release is out, check that the proxy reports the retraction. The command prints the reason:

   ```bash
   MODULE=github.com/a-novel-kit/golib
   go list -m -retracted -f '{{.Retracted}}' "$MODULE@v1.4.2"
   ```

### npm packages

GitHub Packages does not support `npm deprecate`, so delete the version. GitHub can restore it for 30 days. A public version with more than 5,000 downloads needs GitHub Support to delete it. `PACKAGE` is the name without its scope:

```bash
gh auth refresh -h github.com -s read:packages,delete:packages
ORG=a-novel
PACKAGE=service-json-keys-rest
VERSION=2.11.1
VERSION_ID=$(gh api --paginate "orgs/$ORG/packages/npm/$PACKAGE/versions" \
  --jq ".[] | select(.name == \"$VERSION\") | .id")
gh api -X DELETE "orgs/$ORG/packages/npm/$PACKAGE/versions/$VERSION_ID"
```

Then find the lockfiles that took it, and move each one off it in a pull request:

```bash
gh search code "$PACKAGE@$VERSION" --owner a-novel --owner a-novel-kit
```

### Container images

Delete each image version that lacks its release proof. Check that production does not pin it first, from an up-to-date checkout of [a-novel/infra](https://github.com/a-novel/infra): a match means the digest is genuine. Write the `/` of an image name as `%2F`:

```bash
ORG=a-novel
PACKAGE=service-json-keys%2Fgrpc
DIGEST="sha256:<digest of the bad version>"
git -C ~/git-projects/a-novel/app/infra grep -n "$DIGEST"
VERSION_ID=$(gh api --paginate "orgs/$ORG/packages/container/$PACKAGE/versions" \
  --jq ".[] | select(.name == \"$DIGEST\") | .id")
gh api -X DELETE "orgs/$ORG/packages/container/$PACKAGE/versions/$VERSION_ID"
```

Deleting a version drops its tags. Leave a moved tag missing: production and Renovate pin digests, and the next release gives tag readers a genuine image. Drop the scopes once every bad version is gone:

```bash
gh auth refresh -h github.com --remove-scopes read:packages,delete:packages
```

### Release notes

Warn the readers of each Release that shipped a bad version, and name the bad versions in the Release that fixes them. An immutable Release keeps its notes editable. This puts a note above the existing notes:

```bash
REPO=a-novel-kit/golib
TAG=v1.4.3
NOTE="Retracts v1.4.2, which a compromised account published."
gh release view "$TAG" --repo "$REPO" --json body --jq .body |
  { printf '%s\n\n' "$NOTE"; cat; } |
  gh release edit "$TAG" --repo "$REPO" --notes-file -
```

## Restore and close

### Re-enrol the security keys

Enrol a replacement for a lost security key as in [GitHub with a security key](./#github-with-a-security-key), and register it as a passkey under **Google Account → Security → Passkeys and security keys**. After a compromised machine, create new SSH keys on every security key you hold, the backup included: the old key files stayed on that machine.

### Set up the credentials again

On the clean machine, set up each credential the attacker could have taken:

- [Sign `gh` in](/dependencies/github#sign-in) and recreate your [packages token](/dependencies/node#configure).
- [Store your new Bitwarden token](./repositories#store-the-bitwarden-token).
- Give each ID that `a-novel secrets ls` lists a new value from its source, with `a-novel secrets set`.
- [Sign `gcloud` in](./gcloud#configure), both logins.
- Recreate the infra [GitHub dispatch tokens](https://github.com/a-novel/infra/blob/master/docs/runbooks/secrets.md#github-dispatch-tokens).

### Lift the halt and resume deploys

Resume only once every unknown write is reverted: the deploy applies everything merged during the freeze.

```bash
gh auth refresh -h github.com -s admin:org
for ORG in a-novel a-novel-kit; do
  gh variable set AGENT_KILL_SWITCH --org "$ORG" --visibility all --body off
done
gh auth refresh -h github.com --remove-scopes admin:org
gh workflow enable deploy.yaml --repo a-novel/infra
gh workflow run deploy.yaml --repo a-novel/infra
```

### Bring the member back

A member removed from GitHub returns once their own account passes the steps above. [Add them](./add-member) again, then [promote them](./promote-member) to their former roles.

### Write the incident notes

Record the incident in a draft security advisory, under the **Security** tab of the repository it touched most, or of the organization's `.github` repository. A draft stays private to admins. Write down:

- the suspect window, how the attacker got in and how you found out;
- what they reached, from the saved outputs;
- each step you took, with its time;
- what would have caught it sooner.

When a bad version reached anyone outside the team, publish the advisory with its affected versions once the fix ships.

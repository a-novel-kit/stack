# Repository config templates

These YAML files are the editable source of truth for the `a-novel repo create`
and `a-novel repo update` commands. They are embedded into the CLI binary, but
an on-disk copy here (or under `$REPO_CONFIG_DIR`) takes precedence, so edits
take effect without a rebuild. The files decode strictly, so a new field breaks
every older installed CLI once the stack checkout pulls it: its rollout runs
`a-novel install` right after the merge.

A repo's desired config is composed from three inputs:

1. a **class** preset (`classes/<class>.yaml`) — or a **repo override**
   (`repos/<org>_<repo>.yaml`) which fully replaces the class for one repo;
2. the **org** profile (`orgs/<org>.yaml`) — bot IDs + signing policy;
3. **discovered** checks/languages (`checks.yaml` + the repo's contents).

Those three are the _only_ inputs. A repo's config is **derived**, never
accumulated: what they do not produce, `repo update` removes — see
[Derived, not accumulated](#derived-not-accumulated).

CLI flags and the interactive form override any field.

## Org-level prerequisite: `AGENT_KILL_SWITCH`

The governance workflows read one org-level Actions variable that the operator
provisions **by hand** — repocfg threads it into every caller
(`kill_switch: ${{ vars.AGENT_KILL_SWITCH }}`) but never creates or writes it.

`AGENT_KILL_SWITCH` is the org-wide emergency halt for all [Agent] automation,
and it is **fail-safe**: a run is RUNNING only when the value (lowercased,
whitespace-stripped) is an explicit off-token — `off`, `false`, `no`,
`disabled`, `0`, or the variable **absent** (an unset `vars.*` resolves to the
empty string); **any other value halts**. A fat-fingered or garbage value
therefore halts, by design (a kill-switch must fail toward halting).

Create it in **both** orgs (`a-novel` and `a-novel-kit`) with the value **`off`**.
A GitHub Actions variable cannot hold an empty string, so `off` — not empty — is
the canonical resting token: equivalent to leaving the variable unset, but
discoverable in the org settings and flippable in place. To **engage** the halt,
set the value to `on` (or anything else the fail-safe does not treat as off); the
next event or reconcile sweep then holds every PR org-wide. **Lift** it by setting
the value back to `off`.

## Always-provisioned files

Every repo gets `.github/workflows/lock-closed.yaml` (`governance/lock-closed.yaml`),
which locks an issue's or pull request's conversation once it closes and unlocks
it on reopen. Open conversations stay unlocked, since a locked one refuses a
GitHub App's review.

Every repo also gets `.github/workflows/notify.yaml` (`governance/notify.yaml`),
which posts to the org's Discord server: a failed `main` or `release` run on the
default branch, pinging the repository's role, then the run that fixes it; each
published release with its notes; a person's pull request reviews, merge and
drop from the merge queue, pinging its reviewers or owner; and a bot's pull
request whose `main` fails. It reads org-level values the operator provisions by
hand: the `CI_WEBHOOK`, `RELEASES_WEBHOOK`, `REVIEWS_WEBHOOK` and
`DEPENDENCIES_WEBHOOK` secrets; `REVIEWS_WEBHOOKS`, a JSON map from
`<org>/<repo>` to that repository's own reviews channel, which wins over
`REVIEWS_WEBHOOK`; `BWS_READ_TOKEN` and `BWS_SERVER_URL`, which resolve a
GitHub account to the Discord user it pings; and the
`DISCORD_REPO_ROLES` variable mapping each `<org>/<repo>` to its role id. A
missing webhook skips the post; a person or repository missing from its map
posts without that ping. Reviews post from `notify-reviews.yaml`, which follows
`pr-relay.yaml`'s runs: a review runs a pull request's own copy of a workflow, so
the Agent key cannot be read on that event.

Every repo also gets `.github/workflows/auto-assign-author.yaml`
(`governance/auto-assign-author.yaml`), which assigns a person's pull request to
its author. A bot's pull request, Dependabot's or Renovate's, stays unassigned.

Independent of class, `repo update` commits a uniform `.github/CODEOWNERS`
(`governance/CODEOWNERS` — a single owner today) to every repo so review
requests route automatically, and removes any stray root `CODEOWNERS` so a repo
never carries two (GitHub honors `.github/` over the root). Dependency manifests
and lockfiles stay unowned, so a bot's version bump requests no review; the
`check-dependencies` check holds a person's manifest change instead.

It also reconciles a uniform **label set** (`governance/labels.yaml`) on every
repo: `ensure` labels are upserted (created, or recolored / re-described to
match), `retire` labels are deleted, and labels in neither list are left
untouched — apply never removes a label it was not told about. Kind
(Epic / Feature / Task / Bug / Initiative) is an issue **type** and Priority /
Size are **project fields**, so neither is a label here; the label set is the
cross-cutting signals that sort work within a board status.

## Contributor tiers (`teams.yaml`)

Each tier in `teams.yaml` is an org team with no repository access:
`developers` (push), `maintainers` (maintain) and `owners` (admin). Under each,
`repo update` keeps one area team per repository class, `<tier>-<class>`, and
grants it the tier's role on every repository of that class, so a new repository
is covered the moment `repo create` classifies it. A person joins the area teams
they work in; membership is managed in GitHub. Core holds Admin everywhere
through its `all_repo_admin` org role and is not a tier.

The reconcile owns only the tier teams. It creates a missing tier or area team,
grants a repository its three area teams, and removes a grant any other tier team
holds there. Teams outside the tiers are left untouched, as labels outside
`labels.yaml` are: core's access comes from its org role, which no repository
grant can remove.

Owners hold Admin, which the `admins` bypass entry covers, so they bypass the
rulesets exactly as core does. Maintainers hold Maintain, which bypasses nothing:
their approval is what `review-gate` waits for.

## Actions settings and scoped secrets

Every repo gets the same Actions settings: a workflow token is read-only unless
its job asks for more and never approves a pull request, and an outside
contributor's fork pull request runs no workflow until a maintainer approves it.
Every repo also turns on immutable releases, so a published release's tag can
never move to other code, whoever bypasses the `tags` ruleset.
Both orgs set the same values org-wide; the repo-level copy keeps a repo correct
if an org setting loosens.

The org's `scoped_secrets` have selected-repository visibility. Discovery reads
every `secrets.NAME` reference in the repo's workflows, and `repo update` grants
each scoped secret the repo names and revokes the others. A new repo reads
exactly what its CI uses, and a secret its workflows stop naming is revoked on
the next reconcile. Switching a secret to selected visibility needs its value,
so an admin does it once in the org settings, picking the repos that name it,
before listing it in `orgs/<org>.yaml`; a listed secret still visible to every
repo fails each grant and revoke with 409.

## The release environment

A repo whose workflows name the `release` environment releases through the
org's release App, whose key is a secret of that environment: a job reads it
only once the environment admits the job's ref. `repo update` opens the
environment to refs a ruleset guards and deletes any other pattern: the default
branch always, and `release/v*` lines and `v*` tags where `rulesets.tags`
limits who creates them. A workflow on any other ref never unlocks the key.
Every job there waits for an approval from `core` or the repo's `owners-<class>`
team, and the person who dispatched the run may give it, so no bot can release.
The release App joins the bypass lists of that repo only, and is the one bot
that writes its default branch, tags and release lines directly.

The release App is installed on selected repositories, and GitHub accepts it as
a bypass actor only where it is installed. A new releasing repo, one created
from service-template included, joins the App's installation in the org
settings before its first `repo update`; until then its rulesets fail with
HTTP 422.

## The governance environment

The privileged keys, listed under `governance.secrets` in `orgs/<org>.yaml`,
are secrets of each repo's `governance` environment, never org secrets. A repo
whose workflows name one gets the environment, opened to the refs the release
environment admits, and `repo update` copies into it exactly the privileged
secrets the repo names from the org's Bitwarden Secrets Manager project, where
each secret's key is its GitHub name. A secret is rewritten when Bitwarden's
revision is newer than GitHub's copy, and one no workflow names is deleted, so
rotating a key is updating it in Bitwarden and running `repo update --all`.

The CLI runs `bws` with the access token stored by
`a-novel secrets set BWS_ACCESS_TOKEN`. Without one the environment keeps its
secrets, and a named secret it lacks fails the run. A job that reads a key
names `environment: {name: governance, deployment: false}`; a job calling a
reusable workflow passes the secret by name and leaves the environment to the
called job, which receives the environment's value.

## `classes/<class>.yaml` and `repos/<org>_<repo>.yaml`

Same schema. A `repos/` file is for a one-off repo and **replaces** the
class entirely (it still names a base `class` for provenance). All fields
are required unless noted.

| Field                                                           | Type   | Meaning                                                                                                                                                                                                                                                                                                                 |
| --------------------------------------------------------------- | ------ | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `class`                                                         | string | Class ID (`service`, `platform`, `infra`, `library`, `workflows`, `meta`).                                                                                                                                                                                                                                              |
| `features.issues` / `.wiki` / `.projects` / `.discussions`      | bool   | Repo feature toggles.                                                                                                                                                                                                                                                                                                   |
| `features.pull_requests`                                        | string | Who may open pull requests: `all` or `collaborators_only`.                                                                                                                                                                                                                                                              |
| `merge.squash` / `.merge_commit` / `.rebase`                    | bool   | Allowed merge methods (squash-only org-wide).                                                                                                                                                                                                                                                                           |
| `merge.auto_merge`                                              | bool   | Allow auto-merge.                                                                                                                                                                                                                                                                                                       |
| `merge.delete_branch_on_merge`                                  | bool   | Auto-delete head branch on merge.                                                                                                                                                                                                                                                                                       |
| `merge.allow_update_branch`                                     | bool   | Offer "update branch" on out-of-date PRs.                                                                                                                                                                                                                                                                               |
| `merge.signoff_required`                                        | bool   | Require `Signed-off-by` on web commits.                                                                                                                                                                                                                                                                                 |
| `security.secret_scanning` / `.push_protection` / `.dependabot` | bool   | Secret scanning, push protection, and Dependabot security-update PRs.                                                                                                                                                                                                                                                   |
| `security.dependabot_alerts`                                    | bool   | Optional explicit state for Dependabot vulnerability alerts; omission leaves the live state alone.                                                                                                                                                                                                                      |
| `pages`                                                         | bool   | Reconcile Pages on (`workflow`) or off.                                                                                                                                                                                                                                                                                 |
| `clean_images`                                                  | bool   | Provision `governance/clean-images.yaml`, which deletes container images once their branch or pull request is gone (on close, and in a daily sweep that a-novel/infra's Cloud Scheduler dispatches), or remove it. Only classes whose images no deployment pins by digest set it: infra pins its tool images by digest. |
| `code_quality`                                                  | bool   | Turn GitHub Code Quality on and add the `code_quality` rule to the `master` ruleset; `false` keeps both off. Code Quality bills per active committer, and per AI credit on every default-branch push.                                                                                                                   |
| `rulesets.master` / `.require_approval` / `.tags`               | bool   | Apply those rulesets. `tags` locks tag (and release) creation to the release bot + admins, and adds `release-lines`, which governs `release/vX.Y` branches like the default branch. `master` also applies `commit-messages`, which checks the subject of every commit on every branch against Conventional Commits.     |
| `rulesets.admins_only`                                          | bool   | Drop the bots from every bypass list, leaving admins only. For a class whose merges deploy production and whose bots reach the default branch through pull requests, since a bot key is an org secret any repository's workflows can read.                                                                              |

The `infra` class is public-by-default and deployment-only. It keeps Pages,
wiki, discussions, release workflow callers, and the tag ruleset off, and only
admins bypass its rulesets. When a repo changes to this class, reconciliation
also deletes managed `release-train` and `hotfix` callers left by a
release-bearing class. Create it interactively
with `a-novel repo create a-novel infra --class infra` after releasing the CLI.

## `orgs/<org>.yaml`

| Field                                                    | Type     | Meaning                                                                                                             |
| -------------------------------------------------------- | -------- | ------------------------------------------------------------------------------------------------------------------- |
| `org`                                                    | string   | Org login (must match the filename).                                                                                |
| `signing_required`                                       | bool     | Require signed + signed-off commits.                                                                                |
| `bots.dependencies` / `.agent` / `.publish` / `.release` | int      | GitHub App IDs of the org bots; the CLI resolves a ruleset bypass entry like `publish` to the right ID for the org. |
| `scoped_secrets`                                         | []string | Org secrets with selected-repository visibility. Each repo can read exactly the ones its workflows name.            |
| `governance.secrets`                                     | []string | Privileged secrets, held only by each repo's `governance` environment.                                              |
| `governance.bws_project` / `.bws_server_url`             | string   | Bitwarden Secrets Manager project the CLI copies them from, keyed by GitHub name, and its server.                   |

## `rulesets/<name>.yaml`

Static ruleset structure. The CLI injects what it cannot know statically:
the `required_status_checks` list (from discovery) and concrete
`bypass_actors` (from the `bypass` list below). `update` replaces an existing
ruleset wholesale.

| Field                                      | Type     | Meaning                                                                                                                                                                                                                                                                                                                                                                 |
| ------------------------------------------ | -------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `name`                                     | string   | Ruleset name (reconciled by name).                                                                                                                                                                                                                                                                                                                                      |
| `target` / `enforcement`                   | string   | `branch` or `tag` / `active`.                                                                                                                                                                                                                                                                                                                                           |
| `conditions.ref_name.include` / `.exclude` | []string | Refs; `~DEFAULT_BRANCH` = the repo default, `~ALL` = all refs of the target.                                                                                                                                                                                                                                                                                            |
| `bypass`                                   | []string | Generic actors (see below).                                                                                                                                                                                                                                                                                                                                             |
| `rules.*`                                  | mixed    | Rule parameters. `creation`/`update`/`deletion` restrict that ref op to bypassers; `required_status_checks.checks` is injected; `code_quality` is dropped when the class sets `code_quality: false`; `merge_queue`, `pull_request` and `commit_message_pattern` pass through as written. Commit metadata rules such as `commit_message_pattern` need GitHub Enterprise. |

**Generic bypass actors** (`bypass:` entries):

- `admins` — org admins + the admin repo role, always. Org/repo-independent.
- `dependencies` / `release` — the org's bot apps, resolved per org from
  `orgs/<org>.yaml`. On the `master`, `tags` and `release-lines` rulesets the
  bypass mode is `always` — the release bot writes directly (the version-bump
  commit, the release tag, a new release line), no branch proxy; on the PR
  rulesets the mode is `exempt`. The agent and publish bots bypass nothing:
  every other bot write reaches a protected ref through a pull request.
- `dependabot` — GitHub's Dependabot App, the same in every org, resolved with
  the bot modes above. GitHub accepts it only on repos Dependabot runs in, so the
  entry is dropped for classes with `security.dependabot: false`.
- `release` is dropped likewise from a repo whose workflows name no `release`
  environment, since the App acts only where releases do.

A class with `rulesets.admins_only: true` drops every entry but `admins`.

The core team is intentionally **not** a bypass actor.

## `checks.yaml`

Decides which CI jobs gate the default branch and its release lines. The
`master` and `release-lines` rulesets' required checks are:

1. the **`always`** set — required on every repo regardless of class (the
   [Agent] App's `merge-gate`, `epic-freeze` and `review-gate`, and the
   `check-dependencies` job of the governance workflow of that name); plus
2. **every job declared in the repo's `.github/workflows/main.yaml`**, minus the
   **`exclude`** rules.

A job's ID is its check context, so there is nothing to derive and nothing to
drift. Workflow files are code (owned by skills / PRs), not by `repo update`;
this map only decides which of their jobs are required, and the set is applied
**wholesale** (no reconcile, no preservation of manual checks).

| Field                 | Type     | Meaning                                                                                            |
| --------------------- | -------- | -------------------------------------------------------------------------------------------------- |
| `integrations`        | map      | App name → GitHub App ID (the `integration_id` a check resolves to).                               |
| `always`              | list     | Checks required on every repo, regardless of class.                                                |
| `exclude.prefixes`    | []string | Job ids starting with one of these are not required (e.g. `report-`: reporting / post-merge jobs). |
| `exclude.if_contains` | []string | Jobs whose `if:` contains one of these are not required (master-only jobs never run on a PR).      |

Coverage is deliberately **not** among them. Codecov posts its own commit
statuses rather than running as a `main.yaml` job, and a required status check
applies to the merge **group** as well as the pull request — where no bypass can
reach it, because a merge group has no author. A provider that accepted an upload
and then never posted its status therefore stalled the queue behind an otherwise
green build. Coverage is still uploaded, reported and visible; it is advisory, and
the old gate is listed in `retired.yaml`.

## Derived, not accumulated

A repo's **rulesets are a set, and the plan names all of it.** Every ruleset comes
from the class preset, the org profile and code-driven discovery, so anything else
live on the repo is drift — whether it was dropped from these templates or added
by hand in the UI — and `repo update` **deletes it**.

That is a deliberate constraint, not a convenience. The alternative is an
ever-growing list of things to un-apply, and a live configuration that is the
templates plus an unreviewable history of whatever each repo happened to be given.
With the set derived, these files are the whole truth: what you can read here is
what governs the repos, and a reconcile is enough to prove it.

Two consequences worth stating plainly:

- **Removing a template removes the ruleset.** Nothing further is needed, and no
  migration list records that it once existed.
- **The UI is not a place to configure a governed repo.** A ruleset added there
  survives until the next reconcile and no longer. Anything genuinely wanted
  belongs in `rulesets/` and a class preset, where every repo gets it and a
  reviewer can see it.

Variance between repos is legitimate only where it is _computed_ — the `master`
ruleset's required checks differ per repo because they are read out of that repo's
`main.yaml`, not because someone set them differently. Scoped-secret access and
the release environment are read out of the same workflows.

---
name: write-github-actions
description: >
  Write or review GitHub Actions workflows, composite actions, CI jobs, required checks, and
  rulesets across both organizations. Load before changing CI or governance configuration.
---

# Writing GitHub Actions

CI is written across two surfaces. The shared building blocks are **composite actions** in
`a-novel-kit/workflows`, each at `<group>/<name>/action.yaml` under `build-actions`,
`generic-actions`, `github-pages-actions`, `go-actions`, `node-actions`, or `publish-actions`. Every
repo's `.github/workflows/*.yaml` then calls them, pinned to a release tag. Read the neighbours
before writing either — the patterns below are already in every file.

**Scope.** This skill owns authoring: workflow files, action manifests, and the repo CI config that
turns a job into a required check. `monitor-ci` owns watching a run and diagnosing a failure.
`coordinate-landing` owns the cross-repo landing saga and merge-queue semantics. `manage-versions`
owns releasing the workflows repo and re-pinning consumers. Point at them; do not restate them.

Before editing a Renovate preset or a repo's `renovate.json`, read
[Renovate presets](references/renovate-presets.md).

---

## Choosing a surface

A **composite action** packages a step sequence that runs inside someone else's job. It cannot
declare `permissions`, cannot fan out across jobs, and has no early return — a guard that must stop
the work sets an output and every later step carries an `if:` on it (`generic-actions/derive-status`
threads a `halted` output that way).

A **reusable workflow** (`.github/workflows/<name>-run.yaml`, triggered by `workflow_call`) packages
whole jobs with their own `permissions` and `secrets:` block. `merge-gate-run.yaml` is one: each
governed repo ships a thin caller so the engine lives in one place. Inside a reusable workflow, `./`
resolves against the **caller's** checkout, so its nested actions must be remote-pinned.

A **repo-local composite action** (`.github/actions/<name>/action.yml`) holds a step sequence used by
several jobs in one repo and by no other repo — `service-json-keys`' `run-migrations` is the model.

---

## Composite actions cannot reference vars, secrets, needs, or matrix

GitHub expression-evaluates the **entire manifest** when it loads a composite action: input
`description` fields, `run:` strings, bash comments inside them. Those contexts do not exist for a
composite action, so a literal `${{ vars.X }}`, `${{ secrets.X }}`, `${{ needs.* }}`, `${{ matrix.* }}`
or `${{ strategy.* }}` anywhere in the file makes it fail to load with
`Unrecognized named-value: 'vars'` — including where it appears purely as documentation of the
caller's syntax. The available contexts are `inputs`, `github`, `steps`, `runner`, `env`,
`job.container`, plus `toJSON` and `always()` / `success()` / `failure()`.

To document caller syntax inside an action, write the context name as plain text. `merge-gate` and
`board-write` describe their halt input as "threaded from the caller as
`kill_switch: vars.AGENT_KILL_SWITCH`" with no `${{ }}` around it, and load fine.

The failure is invisible on the PR that introduces it. The workflows repo's own governance callers
stay pinned to the previous release tag while the PR is open, and its `main.yaml` exercises only the
node lane through `./node-actions/lint-node`, so nothing loads the edited manifest. The breakage
surfaces the moment a consumer pins the new tag — which is how a `vars` reference in an input
description took `merge-gate` offline across both orgs until the next patch. The workflows repo's
`pnpm lint` now parses every composite manifest for these contexts
(`.github/scripts/lint-action-manifests.mjs`), and the required `lint-node` job runs it; keep it
passing rather than working around it.

Caller **workflows** carry `${{ vars.* }}` and `${{ secrets.* }}` legally. Thread the value into the
action as an ordinary input.

---

## Writing a composite action

```yaml
name: board-write
description: >
  One-line purpose, then the rationale a caller needs. This text is the README catalog entry.

inputs:
  client_id:
    required: true
    description: What it is and why the action needs it.
  dry_run:
    required: false
    default: "false"
    description: When "true", log the change that would be made without writing it.

outputs:
  changed:
    description: '"true" if the field was mutated.'
    value: ${{ steps.write.outputs.changed }}

runs:
  using: composite
  steps:
    - name: Write the field
      id: write
      shell: bash
      env:
        PROJECT_ID: ${{ inputs.project_id }}
      run: |
        set -euo pipefail
        ...
```

- Input names are `snake_case`, and every input carries a `description` — GitHub prints it and the
  README catalog is transcribed from it by hand, so an inaccurate one propagates. (`go-actions/lint-go`
  keeps a kebab-case `working-directory` from before the convention settled.)
- Booleans travel as the strings `"true"` / `"false"`; there is no boolean input type for actions.
- Every `run:` step declares `shell: bash` explicitly — a composite action has no default shell.
- Pass values into a script through `env:` and read them as shell variables, rather than
  interpolating `${{ inputs.x }}` into the script body, so a value containing shell metacharacters is
  data instead of code.
- Start each script with `set -euo pipefail`. Report failures with `::error::` so they surface as
  annotations, and write operator-facing results to `$GITHUB_STEP_SUMMARY`.
- Reference a sibling action in the same repo by its pinned tag
  (`a-novel-kit/workflows/generic-actions/pull-bot@v1.20.1`). A relative `./` path would bind a
  released action to whatever sits on `master`, so a release could not be internally consistent.
- Mint the narrowest App token the step needs: `actions/create-github-app-token` takes
  `permission-*` inputs (`permission-organization-projects: write`), which bounds a leak of that
  token to the one operation.
- Update the README catalog in the same change whenever a `name` or `description` moves.
- Non-trivial bash in an action belongs under `tests/`. The suites there extract the functions
  verbatim out of the manifest and run them against stubbed network leaves, so the shipped code is
  what executes and there is no fixture to drift.

Consumer-visible changes (a renamed input, a new preferred path) need a migration guide under
`docs/migrations/`; `prepare-release` owns sizing the release and writing it.

---

## Writing a caller workflow

`main.yaml` is a repo's CI. Libraries, the stack and the workflows repo run it on every branch
push, ignoring tags:

```yaml
on:
  push:
    tags-ignore: ["**"]
    branches: ["**"]
```

Services, platforms and infra run it on pull requests instead, so adding a label starts a run:
`with-feature-env` and the head-bound approval labels need one.

```yaml
on:
  push:
    tags-ignore: ["**"]
    branches: [master]
  pull_request:
    types: [opened, synchronize, reopened, labeled, unlabeled]
  merge_group:
```

On a pull request, `github.sha` is GitHub's temporary merge commit (`refs/pull/N/merge`), not the
pull request's head. Anything bound to a head uses `github.event.pull_request.head.sha`: a commit
status, a label approval, a proof. A job that must test exactly the approved commit checks it out
by that sha. `pull-bot`'s default, `github.head_ref`, is the branch tip, which a push can move
mid-run.

Each job declares its own `permissions` block. Read-only is the floor (`contents: read`); image
publishing adds `packages: write`, `attestations: write`, `id-token: write`; a job whose action mints
its own App token declares `permissions: {}`, because the default token is never used.

Pin every `uses:` to a release tag. Renovate groups the whole workflows repo under one
`a-novel-kit workflows` update, so a repo's references move together and must sit at one version.
Third-party actions are pinned by tag too (`actions/checkout@v7`), which keeps every reference
readable. `a-novel/infra` is the one exception: its jobs hold the production identity, so it pins
third-party actions to their release commit, as `write-infra` describes. No other repository pins
by commit.

`vars.*` and `secrets.*` belong here. The bot client ids are org variables: `AGENT_BOT_CLIENT_ID`
for governance work, `DEPENDENCY_BOT_CLIENT_ID` for the node lane and the audit fix. Their private
keys and `BWS_READ_TOKEN` are secrets of each repo's `governance` environment, which `repo update`
fills from Bitwarden. A job that reads one names `environment: {name: governance, deployment: false}`;
a job calling a reusable workflow passes the secret by name, and the called job names the
environment. The release App's key lives only in the `release` environment.

Add a `concurrency` group with `cancel-in-progress: true` only where a superseded run's result is
worthless, such as a lint-only workflow. Anything that releases or deploys must run to completion.
platform-studio's `main.yaml` groups by
`${{ github.workflow }}-${{ github.event.pull_request.number || github.run_id }}`. A pull request's
newer run cancels the older one, which could otherwise post a stale screenshot verdict last, while
every other run gets a group of its own and finishes. Cancel only where a newer head supersedes the
run, since a push moves the merge box to the new head. A cancelled job on the current head stays red
there, because the merge box shows each workflow's newest run. A gate that events on one head
re-trigger, such as `review-gate` on labels and reviews, queues with `cancel-in-progress: false`: its
runs never overlap, so the last one reads the latest state, and a newer event replaces the waiting run
before it starts. A workflow-level group on a workflow that
triggers on `labeled`, as that one does, can strand a pull request's required checks; `monitor-ci`
covers the [diagnosis and recovery](../monitor-ci/references/dependency-prs.md#stranded-required-checks).

---

## `needs:` carries data, plus one build gate

A `needs:` edge has two uses. The first is **data flow**: the downstream job consumes something
the upstream one produces, such as an image digest read as `needs.build-database.outputs.digest`, a
coverage artifact ID read as `needs.test-go.outputs.artifact-id`, or a file left on disk.

The second is a **build gate** in front of the expensive jobs. Every repo in both orgs draws on one
pool of runners, so a test or image job started on code that does not compile holds a runner that
another run is queued for. Gate the long jobs (tests, browser suites, container builds) on the cheap
jobs whose failure makes their result worthless: the build and the type check. `platform-studio`
gates its `test-*` jobs and `build-platform` on `[build-node, lint-types]`.

Keep the graph to three stages: cheap checks with no `needs:`, the gated heavy jobs, and the
`report-*` jobs that read their outputs. Each stage adds its slowest job to every run, green runs
included, so chain no further. A `test-go → build-*` edge to keep untested code out of an image is
one stage too many: those images are untagged dev artifacts, and `merge-gate` keeps `master`
green.

Pick gates that finish within the slowest cheap job, so gating adds no wall time. Leave style, lint,
audit and scan jobs out of the gate: their failures leave a test run valid, and gating on them would
hide test failures until the next push.

**A gate must be a required check that always runs.** A job whose `needs:` failed or was skipped is
itself skipped, and GitHub counts `skipped` as passing. The tests block a merge only because their
gate is required and red. Never gate on a `report-*` job, a master-only job, or one with an `if:`
that can skip it, such as `lint-translations`: its skip would pass every job behind it.

Rewiring `needs:` is safe against the ruleset: required checks derive from the **job list** in
`main.yaml` (see below), not from the graph, so cutting an edge never changes a check context and
never needs `a-novel repo update`. Adding or removing a job does.

Sequencing decisions are the skill's job, not the workflow's — do not restate this rationale as a
comment in a `main.yaml`. Comment what is specific to that file: why _this_ postgres needs a longer
health timeout, why _this_ job runs its binary twice. Reviewers get the general rule from here.

---

## Job names are check contexts

A job's ID **is** its required-check context, so name it by lane: `test-go`, `lint-go`, `lint-node`,
`lint-proto`, `generated-go`, and the `build-*` / `report-*` families. A bare verb like `test` breaks
down in a repo holding both Go and JS, where two jobs would claim the name, and the discovery map in
`cli/internal/repocfg/templates/checks.yaml` cannot tell which lane it belongs to. Some repos still
emit a legacy bare `test`, and the node lane is split between `lint-node` and an older `lint-js`;
new jobs use the lane form.

A job must also not take the name of a check-run the [Agent] App posts. `merge-gate.yaml` and
`epic-freeze.yaml` both name their job `evaluate` for that reason — the job is only the runner, and a
same-named job would collide with the App's check.

---

## Required checks and rulesets

`cli/internal/repocfg/templates/checks.yaml` decides which jobs gate the default branch. The
required set is the `always` list plus every job declared in the repo's `.github/workflows/main.yaml`,
minus two exclusions: IDs starting with `report-` (reporting and post-merge jobs), and jobs whose
`if:` restricts them to master, since a job that never runs on a PR cannot gate one.

Adding a job to `main.yaml` therefore adds a required check on the next `a-novel repo update`.
Renaming or removing one leaves its old context required, and the pull request making that change
never reports it. An admin merges that pull request past the stale context, then runs
`a-novel repo update` from up-to-date master. Coverage gates nothing: no ruleset requires a Codecov
status.

The `release-lines` ruleset requires the same set on `release/vX.Y` branches. A backport runs those
checks from its line's own workflow files, so a check added after the line's tag never reports there
and an admin merges past it (`manage-versions`).

The rulesets themselves are static YAML under `templates/rulesets/`, with the required-check list and
the bypass actors injected by the CLI. `a-novel repo update` applies them; `use-a-novel-cli` covers
the command. The templates are the whole configuration: a ruleset or setting changed in the UI is
reverted on the next reconcile.

`commit-messages` checks the subject of every commit on every branch, except the merge queue's
`gh-readonly-queue/**`, against Conventional Commits. A squash merge lands with the PR title as its
subject and the branch's commit messages as its body, so both halves are checked. The rule is active,
so automation that commits must write Conventional subjects too (`git-conventions`). Commit metadata
rules like this one need GitHub Enterprise.

Two GitHub constraints shape the bypass lists:

- **Merge-queue commits match no bypass actor.** The queue writes them, not a person or an App, so
  every PR into a protected branch must satisfy the rules itself, a bot's PR included.
- **A bypass actor must be part of the repo.** Dependabot (App 29110) is `commit-messages`' only
  bypass actor, and only on classes with `security.dependabot: true`. On a repo Dependabot never ran
  in, GitHub rejects it with HTTP 422, "Actor Dependabot integration must be part of the ruleset
  source or owner organization". The release App, a bypass actor on every repo whose workflows
  name the `release` environment, is installed on selected repositories, so a new releasing repo
  joins its installation in the org settings before its first `repo update`.

**A bypass label approves one PR head.** A label that waives a check (`allow-screenshot-change`,
`allow-incomplete-translations`, `allow-translation-drift`, `append-only-override`, `allow-dependency-change`, infra's
`allow-resource-deletion` and `allow-downtime`) approves the commit it was reviewed on, so a push removes it and a reviewer
adds it again. The removal runs in a job holding `pull-requests: write`, and the label change re-runs
the gate. A gate reads labels live through the API, or ignores them on a `synchronize` payload, since
a rerun replays its original event. `buf skip breaking` is the exception: `bufbuild/buf-action` reads
it from the event payload, so it stays across pushes, and `review-gate`'s approval of the head covers
a later push. `check-append-only` removes its label only when the step gets
`github_token`.

`with-feature-env` follows the same rule to publish rather than waive. `build-actions/docker` tags
an image only on a push to the default branch, a release, or a pull request carrying the label,
which gets the moving `pr-<N>` tag; every other build is pushed by digest alone for the jobs of its
run. The managed `feature-env.yaml` removes the label on the next push, so the tag keeps naming the
last head someone labeled. A repo's `main.yaml` must trigger on `labeled` for the label to build.

Services and platforms (`clean_images: true`) also get the managed `clean-images.yaml`, which deletes
images once the branch or pull request that built them is gone.

- Closing a pull request deletes its `pr-<N>` image at once.
- A daily sweep, which a-novel/infra's Cloud Scheduler dispatches, takes the rest: untagged builds and tags of closed pull requests, merge-queue builds,
  and old branch tags. It spares anything less than a day old, which a running job may still pull.

Release tags, `master`, every `master` build and open pull requests' images stay, together with the
manifests and attestations they reference. `build-actions/clean-images` reads a build's origin from its
`org.opencontainers.image.version` label. A build pushed without a tag has no version, so its
provenance attestation supplies the ref instead (`refs/pull/<N>/merge`). Infra does not set the flag,
since it pins its tool images by digest. A deleted version can be restored for 30 days.

`repo update` also owns the Actions security settings: a read-only workflow token that never
approves a pull request, maintainer approval for every outside contributor's fork run, and access
to the org's `scoped_secrets`. A repo can read a scoped secret only once one of its workflows names
it and a reconcile has run, so a new `secrets.X` reference ships with an `a-novel repo update`.

Pull requests are for collaborators: every class sets `features.pull_requests: collaborators_only`,
and outsiders use issues and discussions. GitHub Apps count as collaborators there, so Renovate and
the bots still open PRs. `lock-closed.yaml` locks an issue's or PR's conversation once it closes and
unlocks it on reopen. Open conversations stay unlocked: a locked conversation refuses a GitHub App's
review ("Issue is locked"), which would block the approval bot. An outsider's comment on an open
conversation is moderated by hand, and a bot that must comment on a closed one reopens it first.

Write access is split by tier. Developers hold Write, maintainers Maintain and owners Admin, each
through a `<tier>-<class>` area team that `repo update` grants from `teams.yaml`. Anyone with Write
can approve, label and dispatch, so the tiers rest on gates rather than roles. `review-gate`, required
everywhere, needs an approval of the head from a maintainer, an admin or the [Agent] App, and counts
a privileged label only when one of them applied it last; the list of privileged labels lives in the
`review-gate.yaml` template, mirrored by the Dependabot job's step. A dispatch-only workflow that
must stay with admins or maintainers runs `generic-actions/require-role` first: it checks
`github.triggering_actor`, because `github.actor` still names the original dispatcher on a re-run,
and the role name, because the legacy permission field reports Maintain as `write`. Another workflow
starts a gated one through `workflow_run`, whose run inherits the upstream run's triggering actor. A
dispatch with the workflow token runs as `github-actions[bot]`, which the gate refuses. Never exempt
that bot: any writer's branch workflow can dispatch as it.

A workflow that reads the Agent key runs from the default branch's copy, which no pull request can
rewrite. `pull_request`, `pull_request_review` and `merge_group` run the pull request's own copy, so
the gates take `pull_request_target`, and `pr-relay.yaml`, which holds no permission and no secret,
carries reviews and merge groups to them through `workflow_run`. The privileged side treats a relay
run only as a wake-up signal: `generic-actions/relay-context` admits a real review or merge group and
reads the pull request's current head through the API. A job on these triggers never checks out or
runs pull request code.

From November 2, 2026, GitHub blocks `pull_request_target` on a public repository that no Actions
event policy covers. Each org therefore holds one org-level policy, `allow-all-events`, which targets
every repository and allows every event the policies API lists. It only lifts that default block.
An event policy is an allowlist, so never narrow it to the events in use: the next new trigger would
stay blocked until someone edits the org's settings. `repo update` does not manage it; an org admin
recreates it with `POST /orgs/{org}/actions/policies`.

The `governance` environment is what makes that hold for any copy of a workflow. It admits the
default branch, plus `release/v*` lines and `v*` tags where the tags rulesets guard them, and
`deployment: false` keeps that check: a feature branch's job naming the environment is refused
before its first step, so a branch that brings back an old workflow reads no key.

The enterprise subscription is the only spend. Standard runners on public repos and the free security
features cost nothing; GitHub Code Quality bills per active committer and per AI credit on every
default-branch push, so every class sets `code_quality: false` and `repo update` keeps it off.

The governance workflows (`merge-gate.yaml`, `epic-freeze.yaml`, `derive-status.yaml`,
`release-train.yaml`, `hotfix.yaml`, `approve-pr.yaml`, `epic-rollback.yaml`, `recover-prs.yaml`,
`check-dependencies.yaml`, `feature-env.yaml`, `clean-images.yaml`, `review-gate.yaml`, `pr-relay.yaml`,
`auto-approve-dependabot.yaml`, `lock-closed.yaml`, `notify.yaml`, `notify-reviews.yaml`, `auto-assign-author.yaml`) are rendered from
`cli/internal/repocfg/templates/governance/`
and carry a "Managed by `a-novel repo update`" banner. Edit the template in the stack repo; a change to
the copy in a repo is overwritten. What those workflows mean is `coordinate-landing`'s subject.
A rendered file must pass every repo's prettier, at width 120 in the fleet and 80 in infra, so write
a list that outgrows 80 columns as a block sequence; `pnpm lint:templates` checks the narrow width.

Two of them keep dependency PRs moving without a person. `auto-approve-dependabot.yaml` approves the
bots' PRs, withholding approval while `renovate/stability-days` is pending; its `status` trigger
approves once Renovate marks the update stable, and its job needs `statuses: read` because the App
cannot read commit statuses. `recover-prs.yaml` re-arms auto-merge on a PR the merge queue dropped for
`checks_timed_out`, and dispatches Renovate when a merge leaves a dependency PR conflicting.

On `a-novel/infra` the auto-approver runs strict, because a merge there deploys production. A bot's
PR merges on its own only as a release pin, or as a version update Renovate itself put on
auto-merge whose every changed line moves only a dependency reference's version, or a digest with its
version. Anything else, Renovate's `automerge: false` updates included, waits for `approve-pr`. The
approval names the commit it classified, so a later push needs its own.

A person's PR posts to the Discord reviews feed alone, through `notify-pr-run`: a review request
pings its reviewers, a review or a drop from the merge queue pings its owner, and its merge posts
without a ping. Nothing posts its draft, assignee or reopen changes.

Each repository has its own reviews and issues channels. The `REVIEWS_WEBHOOKS` org secret and the
`.github` repo's `ISSUES_WEBHOOKS` secret map `<org>/<repo>` to that channel's webhook, and a
repository missing from a map posts to the org's channel (`REVIEWS_WEBHOOK`, `ISSUES_WEBHOOK`), which
is the `.github` repo's own. `discord-post` picks the entry and masks it: GitHub masks a JSON secret
only as a whole, so an expression such as `fromJSON(secrets.X)[repo]` would print the URL in the
step's inputs. Changing a channel rewrites the whole map, since a secret cannot be read back.

A bot's PR notifies nobody on GitHub: it has no assignee, and CODEOWNERS leaves dependency manifests
and lockfiles unowned, so a version bump requests no review. A failing one reaches the Discord
dependencies feed instead, and so does one still green and out of the merge queue 2 hours after its
last commit (`notify-stuck-run`); when that one waits on a person's review, it posts to the reviews
feed too, pinging its reviewers. A person's manifest change (`go.mod`, a tool's `.mod`, `go.work`,
`package.json`, `pnpm-workspace.yaml`) is held by `check-dependencies.yaml` until a reviewer adds
`allow-dependency-change`; lockfiles pass. The `always` list requires that check everywhere, and a
ruleset cannot exempt the merge queue's actorless merges, so the check itself passes merge-queue groups
and a PR whose author and every commit are bots.

CI warnings reach the CI channel once a day. `notify-warnings-run` reads the warning annotations of
each org's default-branch runs and posts one line per distinct warning, whatever the number of
repositories raising it, and nothing on a clean day. A pull request run's warnings stay on the pull
request. An expected warning gets an ignore rule in the org's `.github` `notify-warnings.yaml`: a
reason, plus regular expressions for `repo`, `job` or `message`. The run's step summary counts what
each rule hid; delete a rule that keeps matching nothing. A test that runs code printing
`::warning::` leaves a real annotation on its job, as the workflows repo's `test-actions` does.

---

## Verifying a change

There is no local runner, so verification is reading plus CI.

- Run `pnpm lint:stylecheck` — workflow YAML is prettier-formatted like any other file, and an
  unformatted one fails the node lane.
- For a composite action, grep the manifest for `${{ vars.`, `${{ secrets.`, `${{ needs.`,
  `${{ matrix.` and `${{ strategy.` before pushing. The workflows repo's `pnpm lint` checks this; a
  repo-local action under `.github/actions/` does not.
- In the workflows repo, `lint-shell` shellchecks every `run:` block that
  `.github/scripts/extract-shell.mjs` pulls out of the manifests and workflows. Reproduce it with
  `node .github/scripts/extract-shell.mjs <dir>`, then shellcheck the files in `<dir>`.
- A change to a shared action is not exercised by the workflows repo's own PR CI. It is proven when a
  consumer re-pins to the released tag, which `manage-versions` sequences.
- Never trigger `release.yaml` to test a change — it cuts a real release. Use its `dry_run` input.
- Hand off to `monitor-ci` once the branch is pushed.

---

## Common pitfalls

- **A `${{ vars.* }}` or `${{ secrets.* }}` expression inside `action.yaml`.** The action fails to
  load for every consumer, and the PR that introduces it is green. Write the context as plain text and
  thread the value in as an input.
- **`uses: ./<group>/<action>` inside a composite action.** It resolves against whatever the consumer
  checked out. Reference the sibling by its pinned tag.
- **A bare-verb job ID.** `test` collides across lanes and cannot be mapped to one; use `test-go` or
  `test-node`.
- **A job named after an App-posted check.** `merge-gate` and `epic-freeze` are check-run names; a job
  of that name duplicates the context.
- **A missing `permissions` block.** The job then inherits the repository default, which is wider than
  it needs. Declare the block on every job, `{}` included.
- **A gate that can be skipped.** Gating on a `report-*`, master-only, or `if:`-guarded job skips
  every job behind it whenever the gate skips, and each skipped job passes its required check.
- **`@master` or a floating ref in `uses:`.** CI stops being reproducible and a workflows release
  reaches consumers unannounced.
- **Mixed workflows-repo tags in one repo.** The actions ship as a unit; move every reference in the
  repo to the same tag.
- **A `run:` step in a composite action without `shell: bash`.** The step fails to load; composite
  actions have no default shell.
- **Editing a governance workflow in a service repo.** It carries a managed-by banner and is
  regenerated. Edit `cli/internal/repocfg/templates/governance/`.
- **Adding a `main.yaml` job without reconciling the ruleset.** The job runs but gates nothing until
  `a-novel repo update` runs.
- **Another bot's branch under `renovate/`.** Renovate prunes the branches under its prefix that its
  configuration does not produce, and closes their PRs. The Dependencies App's own PRs use
  `chore/deps/`, which `notify.yaml` watches like Renovate's branches.
- **A bot commit made with `git commit` and pushed.** The `master` and `release-lines` rulesets
  require signed commits, and GitHub signs only what an App creates through its API with no custom
  author. A pushed commit is unsigned, so its PR stays `BLOCKED` with every check green. Build it with
  the Git Data API (`git/trees`, `git/commits`, then the ref), as `refresh-apko-locks` does. When only
  git can make the change, push it, then recreate the commit from its pushed tree, as
  `backport-run.yaml` does with a cherry-pick.
- **A `schedule:` trigger on anything that must run on time.** GitHub starts scheduled workflows 4 to
  7 hours late (a-novel-kit/.github#288). A `.github` repo's timed workflow takes `workflow_dispatch`
  only, and its cron moves to `github_dispatch_schedules` in a-novel/infra's bootstrap, where a Cloud
  Scheduler job dispatches it in each org. A poll reads the window since its caller's last successful
  run, as `notify-stuck-run` does, so a missed tick is caught up instead of lost. `token-expiry` warns
  before a dispatch token lapses, the likeliest way the clock stops.
- **`lfs: true` on a checkout whose job reads a few LFS files.** The checkout downloads every LFS
  object at the ref. Limit it in the step's `env`, which actions/checkout passes to git:
  `GIT_CONFIG_COUNT: 1`, `GIT_CONFIG_KEY_0: lfs.fetchinclude`, and `GIT_CONFIG_VALUE_0: <pattern>`.
  A second checkout over an existing tree leaves its pointer files in place; run `git lfs checkout`
  after it. uikit's `main.yaml` `lint-js` does both.
- **Interpolating an input straight into a `run:` script.** Bind it through `env:` so the value cannot
  be read as shell syntax.
- **A `pnpm <script>` step in an action without reconciling the lockfile first.** pnpm 11's
  verify-deps pre-run hook spawns a _frozen_ `pnpm install` ahead of the script and aborts
  `ERR_PNPM_LOCKFILE_CONFIG_MISMATCH` whenever an earlier step (e.g. `pnpm audit --fix=override`)
  rewrote `pnpm-workspace.yaml` but left the lockfile stale. `npm_config_frozen_lockfile: "false"`
  does _not_ rescue this path — the pre-run install runs frozen regardless. Reconcile with
  `pnpm i --no-frozen-lockfile` before any `pnpm <script>`.
- **Interpolating an unvalidated value into a `search(...)` / `gh search` query.** GitHub answers a
  malformed `merged:` / `closed:` qualifier — a bad instant, or a relative word like `yesterday` —
  with _zero rows and no `errors` array_, indistinguishable from a genuinely empty result, so a
  well-formed-response guard waves it through. In the landing-saga actions an empty `merged` bucket
  reads as "nothing landed" and lifts a standing freeze. These qualifiers take a full ISO-8601
  instant (second granularity); validate every interpolated value to an exact canonical form (strict
  regex plus a calendar parse for dates) and _drop_ an unusable one for the unscoped query rather
  than pass it through — over-detecting is the safe direction, a silent all-clear is not.

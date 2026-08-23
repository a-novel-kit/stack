# Repository config templates

These YAML files are the editable source of truth for the `a-novel repo create`
and `a-novel repo update` commands. They are embedded into the CLI binary, but
an on-disk copy here (or under `$REPO_CONFIG_DIR`) takes precedence, so edits
take effect without a rebuild.

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

Independent of class, `repo update` commits a uniform `.github/CODEOWNERS`
(`governance/CODEOWNERS` — a single owner today) to every repo so review
requests route automatically, and removes any stray root `CODEOWNERS` so a repo
never carries two (GitHub honors `.github/` over the root).

It also reconciles a uniform **label set** (`governance/labels.yaml`) on every
repo: `ensure` labels are upserted (created, or recolored / re-described to
match), `retire` labels are deleted, and labels in neither list are left
untouched — apply never removes a label it was not told about. Kind
(Epic / Feature / Task / Bug / Initiative) is an issue **type** and Priority /
Size are **project fields**, so neither is a label here; the label set is the
cross-cutting signals that sort work within a board status.

## `classes/<class>.yaml` and `repos/<org>_<repo>.yaml`

Same schema. A `repos/` file is for a one-off repo and **replaces** the
class entirely (it still names a base `class` for provenance). All fields
are required unless noted.

| Field                                                           | Type   | Meaning                                                                                              |
| --------------------------------------------------------------- | ------ | ---------------------------------------------------------------------------------------------------- |
| `class`                                                         | string | Class ID (`service`, `platform`, `infra`, `library`, `workflows`, `meta`).                           |
| `features.issues` / `.wiki` / `.projects` / `.discussions`      | bool   | Repo feature toggles.                                                                                |
| `merge.squash` / `.merge_commit` / `.rebase`                    | bool   | Allowed merge methods (squash-only org-wide).                                                        |
| `merge.auto_merge`                                              | bool   | Allow auto-merge.                                                                                    |
| `merge.delete_branch_on_merge`                                  | bool   | Auto-delete head branch on merge.                                                                    |
| `merge.allow_update_branch`                                     | bool   | Offer "update branch" on out-of-date PRs.                                                            |
| `merge.signoff_required`                                        | bool   | Require `Signed-off-by` on web commits.                                                              |
| `security.secret_scanning` / `.push_protection` / `.dependabot` | bool   | Secret scanning, push protection, and Dependabot security-update PRs.                                |
| `security.dependabot_alerts`                                    | bool   | Optional explicit state for Dependabot vulnerability alerts; omission leaves the live state alone.   |
| `pages`                                                         | bool   | Reconcile Pages on (`workflow`) or off.                                                              |
| `code_quality`                                                  | bool   | Add the `code_quality` rule to the `master` ruleset (GitHub Code Quality is a separate repo toggle). |
| `rulesets.master` / `.require_approval` / `.tags`               | bool   | Apply those rulesets. `tags` locks tag (and release) creation to the agent bot + admins.             |

The `infra` class is public-by-default and deployment-only. It keeps Pages,
wiki, discussions, release workflow callers, and the tag ruleset off; when a
repo changes to this class, reconciliation also deletes managed `release-train`
and `hotfix` callers left by a release-bearing class. Create it interactively
with `a-novel repo create a-novel infra --class infra` after releasing the CLI.

## `orgs/<org>.yaml`

| Field                                       | Type   | Meaning                                                                                                                   |
| ------------------------------------------- | ------ | ------------------------------------------------------------------------------------------------------------------------- |
| `org`                                       | string | Org login (must match the filename).                                                                                      |
| `signing_required`                          | bool   | Require signed + signed-off commits.                                                                                      |
| `bots.dependencies` / `.agent` / `.publish` | int    | GitHub App IDs of the three org bots; the CLI resolves a ruleset bypass entry like `publish` to the right ID for the org. |

## `rulesets/<name>.yaml`

Static ruleset structure. The CLI injects what it cannot know statically:
the `required_status_checks` list (from discovery) and concrete
`bypass_actors` (from the `bypass` list below). On `update` of an existing
ruleset, unmanaged bypass actors already present are preserved.

| Field                                      | Type     | Meaning                                                                                                                                                                                              |
| ------------------------------------------ | -------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `name`                                     | string   | Ruleset name (reconciled by name).                                                                                                                                                                   |
| `target` / `enforcement`                   | string   | `branch` or `tag` / `active`.                                                                                                                                                                        |
| `conditions.ref_name.include` / `.exclude` | []string | Refs; `~DEFAULT_BRANCH` = the repo default, `~ALL` = all refs of the target.                                                                                                                         |
| `bypass`                                   | []string | Generic actors (see below).                                                                                                                                                                          |
| `rules.*`                                  | mixed    | Rule parameters. `creation`/`update`/`deletion` restrict that ref op to bypassers; `required_status_checks.checks` is injected; `code_quality` is dropped when the class sets `code_quality: false`. |

**Generic bypass actors** (`bypass:` entries):

- `admins` — org admins + the admin repo role, always. Org/repo-independent.
- `dependencies` / `agent` / `publish` — the org's three bot apps, resolved
  per org from `orgs/<org>.yaml`. On the `master` and `tags` rulesets the bypass
  mode is `always` — the bot writes directly (the version-bump commit, the
  release tag), no branch proxy; on the PR rulesets the mode is `exempt`.

The core team is intentionally **not** a bypass actor.

## `checks.yaml`

Decides which CI jobs gate the default branch. The `master` ruleset's required
checks are:

1. the **`always`** set — required on every repo regardless of class (the
   [Agent] App's `merge-gate` and `epic-freeze`); plus
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
`main.yaml`, not because someone set them differently.

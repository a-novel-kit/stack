# Repository config templates

These YAML files are the editable source of truth for the `a-novel repo create`
and `a-novel repo update` commands. They are embedded into the CLI binary, but
an on-disk copy here (or under `$REPO_CONFIG_DIR`) takes precedence, so edits
take effect without a rebuild.

A repo's desired config is composed from three inputs:

1. a **class** preset (`classes/<class>.yaml`) — or a **repo override**
   (`repos/<org>_<repo>.yaml`) which fully replaces the class for one repo;
2. the **org** profile (`orgs/<org>.yaml`) — bot ids + signing policy;
3. **discovered** checks/languages (`checks.yaml` + the repo's contents).

CLI flags and the interactive form override any field.

## `classes/<class>.yaml` and `repos/<org>_<repo>.yaml`

Same schema. A `repos/` file is for a one-off repo and **replaces** the
class entirely (it still names a base `class` for provenance). All fields
are required unless noted.

| Field                                                           | Type   | Meaning                                                                                                     |
| --------------------------------------------------------------- | ------ | ----------------------------------------------------------------------------------------------------------- |
| `class`                                                         | string | Class id (`service`, `library`, `workflows`, `meta`).                                                       |
| `features.issues` / `.wiki` / `.projects` / `.discussions`      | bool   | Repo feature toggles.                                                                                       |
| `merge.squash` / `.merge_commit` / `.rebase`                    | bool   | Allowed merge methods (squash-only org-wide).                                                               |
| `merge.auto_merge`                                              | bool   | Allow auto-merge.                                                                                           |
| `merge.delete_branch_on_merge`                                  | bool   | Auto-delete head branch on merge.                                                                           |
| `merge.allow_update_branch`                                     | bool   | Offer "update branch" on out-of-date PRs.                                                                   |
| `merge.signoff_required`                                        | bool   | Require `Signed-off-by` on web commits.                                                                     |
| `security.secret_scanning` / `.push_protection` / `.dependabot` | bool   | GHAS toggles (public repos).                                                                                |
| `codeql.enabled`                                                | bool   | Enable CodeQL advanced setup (commits `.github/workflows/codeql.yml`).                                      |
| `codeql.query_suite`                                            | string | `default` or `security-and-quality` (the latter feeds the `code_quality` rule). Omit when `enabled: false`. |
| `pages`                                                         | bool   | Enable a GitHub Pages site (build type: workflow).                                                          |
| `codecov`                                                       | enum   | `auto` (on when the repo has tests), `enabled`, or `disabled`.                                              |
| `code_quality`                                                  | bool   | Add the `code_quality` rule to the `master` ruleset.                                                        |
| `rulesets.master` / `.require_approval` / `.tags`               | bool   | Apply those rulesets. `tags` locks tag (and release) creation to the agent bot + admins.                    |

## `orgs/<org>.yaml`

| Field                                       | Type   | Meaning                                                                                                                   |
| ------------------------------------------- | ------ | ------------------------------------------------------------------------------------------------------------------------- |
| `org`                                       | string | Org login (must match the filename).                                                                                      |
| `signing_required`                          | bool   | Require signed + signed-off commits.                                                                                      |
| `bots.dependencies` / `.agent` / `.publish` | int    | GitHub App ids of the three org bots; the CLI resolves a ruleset bypass entry like `publish` to the right id for the org. |

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

The semantic discovery map: detected signals → required check contexts +
CodeQL languages + Dependabot ecosystems. See the inline comments there;
the CLI applies it via the `detect` package. Signals are matched by a bounded,
gitignore-aware walk, so a module in a sub-directory (e.g. stack's Go module
under `cli/`) is detected — not just one at the repo root.

| Field                                             | Type     | Meaning                                                                                              |
| ------------------------------------------------- | -------- | ---------------------------------------------------------------------------------------------------- |
| `integrations`                                    | map      | App name → GitHub App id (the `integration_id` a check resolves to).                                 |
| `always`                                          | list     | Checks required on every repo, regardless of language.                                               |
| `languages.<lang>.detect` / `.checks` / `.codeql` | mixed    | Signal paths that detect the language → its required checks + CodeQL languages.                      |
| `features.<feat>.detect` / `.checks`              | mixed    | More specific signal (e.g. `pkg/js`) → extra checks.                                                 |
| `docker.context_format`                           | string   | `build-%s` per detected Dockerfile target. Some CI jobs are hand-named and do not match (see below). |
| `codecov`                                         | mixed    | Test presence → the codecov ruleset's checks.                                                        |
| `retired`                                         | []string | Check contexts this map once emitted but has renamed/removed — see below.                            |

### Managed vs unmanaged checks (how `update` reconciles)

`a-novel repo update` reconciles the `master` ruleset's required checks rather
than overwriting them. A check context is **managed** if this map owns it — the
catalog it can emit (`always` + every language + every feature + `codecov`)
plus the `retired` names. Everything else live is **unmanaged**: a manual
ruleset addition, or a hand-named docker job context the map cannot reproduce
(`init.Dockerfile` → the job `build-job-init`, not `build-init`).

On `update` the result is **discovery ∪ (unmanaged live checks)**:

- a newly-declared managed check rolls out on the next `update`;
- a managed check the repo no longer produces is **dropped** (so stale required
  checks don't linger and block PRs on a check that never reports);
- unmanaged checks are **preserved** (a manual gate is never clobbered).

`update --full` skips the union and resets to a plain rediscovery — dropping the
unmanaged checks too, to wipe a bad manual edit.

**`retired`** is how a _renamed or removed_ managed check gets dropped: its old
name is no longer in the catalog, so without `retired` it would look unmanaged
and be preserved forever. List the OLD name there (e.g. `test`, renamed to
`test-go`). A repo whose CI still emits the old context must rename that job
**before** `update` runs on it, or the new required check will sit "Expected".

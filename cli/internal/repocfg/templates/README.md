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
the CLI applies it via the `detect` package.

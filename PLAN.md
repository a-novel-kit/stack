# PLAN — `a-novel repo` (repository config create/update)

> Temporary working spec. Not for commit. Drives a later implementation pass.
> Scope analysis done by reading live config (read-only) across both orgs,
> excluding `service-narrative-engine` and `agora-infra` per instruction.

## 1. Goal

Two **interactive, human-only** CLI verbs (TTY-gated like `publish`, agent CANNOT run):

- `a-novel repo create <org> <name> [--description …] [--template …]` — run from **stack root**; creates the repo (optionally from an org template) and applies a full config.
- `a-novel repo update` — run from **inside a checked-out repo** (cwd → `git remote origin` → org/name, like `publish` resolves the current repo); reconciles its config.

Charmbracelet/Bubble Tea UI (reuse `cli/internal/ui` styles). Every option also a CLI flag → flags skip the corresponding UI step (but the TTY gate always applies). `update` pre-ticks current state; with no changes it just refreshes/adds missing baseline at defaults.

## 0. Revision r2 — feedback + research resolved

### Free-plan reality (researched — nothing org-level is viable)

On the GitHub **Free** org plan:

- **Org-level / multi-repo rulesets: NOT available** — Team/Enterprise only ([About rulesets](https://docs.github.com/en/repositories/configuring-branches-and-merges-in-your-repository/managing-rulesets/about-rulesets)).
- **Rulesets on private/internal repos: NOT enforced** — needs Team ([community #184363](https://github.com/orgs/community/discussions/184363)). This is exactly the `assets` 403 we hit.
- **Org custom security configurations: NOT available** — Team/Enterprise. But secret-scanning / push-protection / CodeQL / Dependabot ARE free **per-repo on PUBLIC repos** (settable via `security_and_analysis` + code-scanning, which is what we do).
- Org **secrets & variables** are already centrally managed → CLI does **not** touch them.

→ **Verdict: CLI is 100% per-repo, public-repo-scoped. No org-level constructs at all. `assets` (private) gets generic settings only.** The §8 "v2 org-level" idea is dead unless you upgrade to Team.

### admin:org — you do NOT need to grant it

The CLI is human-interactive and runs as **your operator `gh` token**, not the bot. `admin:org` would be a scope on _your_ token (`gh auth refresh -h github.com -s admin:org`), **not** a change to the `anovelbot-agent` app — and the bot must never get org-admin (it would undo the blast-radius wall we just built). Given free plan (no org rulesets) + bypass actors templated in YAML (§10) + org secrets already managed, `admin:org` buys nothing the CLI needs. **Recommendation: don't grant it; your existing org-owner repo-admin is sufficient for every per-repo write.**

### Judgment calls — your answers, folded in

1. signoff → **on everywhere** + enforce **commit signing**; also add a getting-started section in `kit/.github` (how to configure signing + signoff, with commands/links). [doc task, tracked here]
2. merge → **squash-only everywhere** (settings + ruleset aligned).
3. code_quality → **baseline for all code classes** (service / go-lib / node-lib / tooling).
4. library Pages → **skip for now**.
5. security on stack/workflows/.github → **enable**; security setup is **templated + CLI-configurable** and **materializes file(s) in the repo** — interpreted as **CodeQL advanced setup** (`.github/workflows/codeql.yml`) + `dependabot.yml`, parameterized by detected languages (§11). (Confirm the file target if you meant something else.)
6. codecov ruleset → **auto-enabled when the repo has tests**; CLI flag, on by default.
7. namespace → **`a-novel repo {create,update}`**.

### New architecture directives (your feedback 2 — supersedes the hardcoded approach)

- **Rulesets + security configs are parameterized YAML templates in the stack repo**, editable without touching Go (§10).
- **Per-org variables (bypass actors, team/app IDs, signing reqs) live in separate YAML** (`repo-config/orgs/*.yaml`), loaded dynamically, editable separately from code (§10).
- **Required checks are auto-discovered semantically** from the repo's languages/features, reusing `cli/internal/detect` (§11).
- **Related/future:** per-`cmd/*` publish tags for services, like stack publishes its main (§12).

---

## 2. Findings — current state (11 in-scope repos)

### Inventory & classes

| Repo                   | Org         | Class                  | vis     | tmpl |
| ---------------------- | ----------- | ---------------------- | ------- | ---- |
| service-template       | a-novel     | **service** (template) | public  | ✅   |
| service-authentication | a-novel     | service                | public  |      |
| service-json-keys      | a-novel     | service                | public  |      |
| golib                  | a-novel-kit | **go-library**         | public  |      |
| jwt                    | a-novel-kit | go-library             | public  |      |
| nodelib                | a-novel-kit | **node-library**       | public  |      |
| workflows              | a-novel-kit | **workflows**          | public  |      |
| stack                  | a-novel-kit | **tooling** (Go CLI)   | public  |      |
| .github (×2)           | both        | **meta**               | public  |      |
| assets                 | a-novel-kit | **assets**             | private |      |

### Settings — what's uniform vs drifted

Uniform across all: `allow_squash_merge=true`, `delete_branch_on_merge=true`, `allow_update_branch=true`, `squash_merge_commit_title=COMMIT_OR_PR_TITLE`, `squash_merge_commit_message=COMMIT_MESSAGES`, `has_issues=true`, `has_projects=true`.

Drift found:

- **`web_commit_signoff_required`**: a-novel repos = **true**, a-novel-kit repos = **false**. (inconsistent across orgs)
- **merge methods**: most allow squash+merge+rebase; **golib allows squash only** (`allow_merge_commit=false`, `allow_rebase_merge=false`). jwt (same class) allows all three.
- **`allow_auto_merge`**: true everywhere except **assets=false**.
- **`has_wiki`**: true except a-novel-kit/.github and assets (false).
- **`has_discussions`**: live services (auth, json-keys) = true; **service-template=false**, everything else false.

### Security (`security_and_analysis`) — biggest drift / fixes

| Repo             | dependabot             | secret_scanning | push_protection |
| ---------------- | ---------------------- | --------------- | --------------- |
| services (3)     | ✅                     | ✅              | ✅              |
| golib, jwt       | ✅                     | ✅              | ✅              |
| **nodelib**      | ✅                     | ❌              | ❌              |
| **stack**        | ❌                     | ❌              | ❌              |
| **workflows**    | ❌                     | ❌              | ❌              |
| **.github ×2**   | ❌                     | ❌              | ❌              |
| assets (private) | null (no GHAS license) |

`secret_scanning_non_provider_patterns` and `secret_scanning_validity_checks` are **disabled everywhere** (optional hardening).

→ **FIX: every public repo should have secret_scanning + push_protection + dependabot on.** Currently 5 public repos are under-protected (nodelib partial; stack/workflows/both .github bare).

### CodeQL default setup

Configured: services, golib, jwt, nodelib. **Not configured: stack (a public Go codebase!), workflows, both .github.**
→ **FIX: stack should have CodeQL** (it's Go). workflows could scan `actions`.

### Pages (build_type=workflow, https)

Have a docs site: services (3) + **jwt**. **golib does NOT** (same class as jwt; kit "community package" obligation = docs site). nodelib none.
→ **DECISION/FIX: golib (and maybe nodelib) should get a Pages docs site for parity.**

### Rulesets — all `source=Repository` (NO org-level rulesets; all duplicated per repo)

All target `~DEFAULT_BRANCH`, `enforcement=active`. Three named rulesets:

**`master`** (every repo):

- `deletion`, `non_fast_forward`
- `merge_queue`: `{grouping_strategy:ALLGREEN, merge_method:SQUASH, check_response_timeout:60, max_build:5, max_merge:5, min_merge:1, min_merge_wait:5}`
- `required_status_checks`: `strict=true`, **check list varies by class** (see below)
- `code_quality:{severity:errors}` — **golib only** (drift; absent on services + jwt)

**`require-approval`** (every repo, identical):

- `pull_request`: `{allowed_merge_methods:[squash], required_approving_review_count:1, dismiss_stale_reviews_on_push:true, require_last_push_approval:true, required_review_thread_resolution:true, require_code_owner_review:false}`
- `copilot_code_review`: `{review_draft_pull_requests:false, review_on_push:false}`

**`codecov`** (services + nodelib only):

- `required_status_checks`: `strict=false`, checks `codecov/patch`,`codecov/project` (integration_id 254)

**Required-status-check lists captured (the main per-class variable):**

- service (service-authentication): `test, GitGuardian Security Checks, lint-go, generated-go, generated-pnpm, lint-node, build-js, build-database, build-job-init, build-migrations, test-pkg-js, build-rest, build-standalone-rest` (13)
- go-library (golib): `lint-go, test, lint-node, lint-proto, GitGuardian Security Checks, generated-go` (6)
- (others — json-keys, jwt, nodelib, stack, workflows, .github — to be captured at implementation; same rule structure, different check contexts. `update` reads them live; `create` seeds from class preset / template.)

Check `integration_id`s seen: `15368` GitHub Actions, `46505` GitGuardian, `254` Codecov.

**Bypass actors** (per ruleset): `OrganizationAdmin` (always) + `RepositoryRole#5` (admin, always) + `Team#<org-team>` (always) + per-org bot **Integration**s (exempt/always). Captured IDs:

- a-novel team `13683698`; a-novel-kit team `13765197`
- integrations: `29110` (common to both orgs), a-novel `1717331`,`1718144`; a-novel-kit `1734926`,`1734949`
- → per-org **bypass profile** must be hardcoded in the CLI (admin:org needed for dynamic `/orgs/{org}/installations` lookup, and the operator token lacks that scope — verified 404).

### Org-level

- **No org rulesets** (404 + needs `admin:org`; and per-repo rulesets confirm `source=Repository`).
- One code-security config: built-in **"GitHub recommended"** (global, **not** default-for-new-repos).
- **No custom properties** in either org.
  → **Free plan: org-level standardization is not available** (org rulesets are Team/Enterprise; private-repo rulesets need Team). All config stays per-repo. See §0 (research) + §8.

## 3. Generic modular config model

```
RepoConfig
  Class          enum: service | go-library | node-library | workflows | meta | tooling | assets
  Org, Name, Description, Homepage, Topics[]
  Visibility     public | private | internal
  Template       (create only) org template repo, e.g. a-novel/service-template
  Features       Issues, Wiki, Projects, Discussions (bools)
  Merge          Squash, MergeCommit, Rebase (bools); AutoMerge; DeleteBranchOnMerge;
                 UpdateBranch; SquashTitle; SquashMessage; SignoffRequired
  Security       SecretScanning, PushProtection, NonProviderPatterns, ValidityChecks,
                 DependabotSecurityUpdates  (skipped/omitted when private+unlicensed)
  CodeQL         Enabled; Languages[]; QuerySuite (default|extended)
  Pages          Enabled; BuildType(workflow); Cname; HttpsEnforced
  Rulesets
    Master         Enabled; RequiredChecks[]{context,integrationID}; StrictChecks;
                   MergeQueue(params); CodeQuality{enabled,severity}; Deletion; NonFastForward
    RequireApproval Enabled; ApprovingCount; DismissStale; LastPushApproval;
                    ThreadResolution; CodeOwnerReview; Copilot{draft,onPush}; SquashOnly
    Codecov        Enabled; Checks[] (default codecov/patch,codecov/project)
  BypassProfile  (derived from Org) admin-role + org-admin + team + bot integration IDs
```

Class presets seed all of the above; flags/UI override per field.

### Baseline (fixed; same for all non-private repos unless a flag overrides)

delete-branch-on-merge, allow-update-branch, squash title/msg, issues+projects on, signoff **on** (standardize), secret_scanning+push_protection+dependabot **on**, `master` + `require-approval` rulesets with the structures above, merge_queue ALLGREEN/SQUASH, 1 approval, squash-only on default branch.

### Configurable (flags / UI / class preset)

class, visibility, description/homepage/topics, wiki, discussions, merge methods (extra to squash), pages (+cname), codeql (+languages/suite), codecov ruleset, code_quality rule (+severity), required-status-checks list, is_template, non-provider-patterns / validity-checks (hardening opt-in).

### Class preset defaults (proposed)

| Field                    | service            | go-lib      | node-lib    | workflows    | meta        | tooling     | assets             |
| ------------------------ | ------------------ | ----------- | ----------- | ------------ | ----------- | ----------- | ------------------ |
| discussions              | on                 | off         | off         | off          | off         | off         | off                |
| wiki                     | off                | off         | off         | off          | off         | off         | off                |
| pages                    | on                 | on          | on          | off          | off         | off         | off                |
| codeql                   | on (go,js,actions) | on (go)     | on (js)     | on (actions) | off         | on (go)     | off                |
| codecov ruleset          | on                 | on          | on          | off          | off         | on          | off                |
| code_quality             | on                 | on          | on          | off          | off         | on          | off                |
| security (ss/pp/dep)     | on                 | on          | on          | on           | on          | on          | n/a (private)      |
| rulesets master+approval | on                 | on          | on          | on           | on          | on          | off (plan-limited) |
| merge methods            | squash-only        | squash-only | squash-only | squash-only  | squash-only | squash-only | squash+merge       |

(Proposed: standardize **squash-only** at the settings layer too, to match the `require-approval` ruleset which already forbids merge/rebase on the default branch — see decisions.)

## 4. Decisions — ANSWERED (inline replies below; consequences folded into §0)

1. **signoff**: standardize `web_commit_signoff_required` = **on** for all? (a-novel has it, kit doesn't.)
   -> Yes, lets enforce token signing and commit signoff (and add that to the getting started doc under kit/.github as well, with links / commands on how to configure it properly)
2. **merge methods**: settings → **squash-only everywhere** (match the ruleset) or keep merge/rebase allowed off-default-branch? golib is already squash-only.
   -> squash only everywhere
3. **code_quality** rule: make it baseline for all **code** classes (service/go-lib/node-lib/tooling), not just golib?
   -> Yes
4. **Pages for libraries**: add docs sites to golib + nodelib (parity with jwt)?
   -> No need for now
5. **CodeQL + security on stack/workflows/.github**: enable (recommended) — confirm meta/.github should at least get push_protection.
   -> Enable, and look to have a standard, customized security rule configured (the one that adds a file to the repo). THis security config should be templated and configurable through cli, just like rulesets.
6. **codecov ruleset on go-libs** (jwt/golib currently lack it though services+nodelib have it): add for coverage parity?
   -> Add codecov only when repo has tests, make this a cli option (on by default)
7. **Command namespace**: `a-novel repo {create,update}` (proposed) vs `a-novel admin repo …`.
   -> Go for proposed

## 5. CLI UX

**Gate**: reuse `stdinIsTTY` seam (publish_cmd.go); refuse non-interactively with the same message pattern. Human-only by construction.

**`create <org> <name>`** (from stack root):

1. Resolve class (flag `--class` or UI picker). Optional `--template` (validated against org templates via `GET /orgs/{org}/repos?type=…`/`is_template`; service-template is the only one).
2. Bubble Tea form seeded by class preset: description, visibility, features, merge, security, codeql, pages, codecov/code_quality, required-checks editor. Flags pre-fill & skip their step.
3. Confirm summary (diff-style preview) → create repo (`POST /orgs/{org}/repos` or `POST /repos/{tmpl}/generate`) → apply config (§6).

**`update`** (from a repo checkout):

1. Resolve org/name from `git remote get-url origin` (mirror publish's cwd resolution).
2. **Read live config** → infer class (or `--class`), pre-tick the form with current values.
3. Human toggles; or `--yes`/all-flags path reconciles to baseline+preset (adds missing options at defaults, leaves rest). Summary diff → apply.

**Charmbracelet**: grouped checklist (sections: General / Features / Merge / Security / CodeQL / Pages / Rulesets), `[x]` toggles, text inputs for checks/topics/description, a final review pane (reuse `ui` report styles). Coherent with `run ui` / publish styling.

## 6. Apply / reconcile (idempotent)

| Concern                                    | Endpoint                                                                                                                                                                        | Idempotency                                                      |
| ------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------- |
| general + features + merge + signoff       | `PATCH /repos/{o}/{r}`                                                                                                                                                          | set desired state                                                |
| security                                   | `PATCH /repos/{o}/{r}` `security_and_analysis`                                                                                                                                  | set desired state (skip block if private+unlicensed → 422 guard) |
| code scanning (CodeQL)                     | render `security/codeql.yaml` → commit `.github/workflows/codeql.yml` (advanced setup, templated per detected langs) via Contents API; default-setup PATCH as fileless fallback | upsert by path+sha (operator bypasses the ruleset)               |
| dependabot config                          | render `security/dependabot.yaml` → commit `.github/dependabot.yml`                                                                                                             | upsert by path+sha                                               |
| pages                                      | `POST` then `PUT /repos/{o}/{r}/pages`                                                                                                                                          | create-or-update (POST 409 → PUT)                                |
| topics                                     | `PUT /repos/{o}/{r}/topics`                                                                                                                                                     | replace                                                          |
| rulesets (master/require-approval/codecov) | `GET /repos/{o}/{r}/rulesets` → match by **name** → `PUT {id}` else `POST`                                                                                                      | reconcile by name; never touch unmanaged rulesets                |
| create                                     | `POST /orgs/{org}/repos` or `POST /repos/{tmpl}/generate`                                                                                                                       | n/a                                                              |

Reconcile loop: read current → build desired from class+flags/UI → compute per-section diff → apply changed sections only → re-read & report. `create` = same apply after repo creation.

**Private repos (assets)**: detect `visibility=private` (+ no GHAS) → skip security/codeql/rulesets/pages modules with a clear "skipped (plan-limited)" note rather than erroring on 403/422.

## 7. Go implementation plan

```
cli/internal/repocfg/
  config.go      RepoConfig + Class enum; loads classes/ + orgs/ YAML (embed + on-disk override)
  templates.go   go:embed repo-config/**; text/template render of ruleset + security YAML
  discover.go    semantic check discovery (wraps cli/internal/detect; reads checks.yaml)
  github.go      thin REST client (net/http like bot_cmd.go): repo, security, code-scanning,
                 pages, topics, rulesets, contents (commit codeql.yml / dependabot.yml)
  read.go        live config → RepoConfig (update pre-fill + class inference)
  apply.go       idempotent reconcile (per-section diff + apply) + summary
repo-config/     (stack root) editable YAML templates — see §10
cli/internal/cli/repo_cmd.go   newRepoCmd(): create + update, flags, TTY gate (stdinIsTTY)
cli/internal/ui/repocfg_form.go  Bubble Tea grouped checklist + review/diff pane
```

Wiring: `newRepoCmd()` registered on root (next to publish). Reuse: `stdinIsTTY` (TTY gate), `orgAnovel`/`orgAnovelKit` consts, the bot_cmd.go REST pattern, `ui` styles, and `detect` for discovery. Per-org bypass/team/app IDs live in `repo-config/orgs/*.yaml` (editable, **not** hardcoded).

Flags (all optional; presence skips the matching UI step; TTY still required):
`--class --visibility --description --homepage --topic(repeatable) --template --discussions --wiki --[no-]pages --pages-cname --[no-]codeql --codeql-lang(repeatable) --[no-]codecov --[no-]code-quality --code-quality-severity --[no-]secret-scanning --[no-]push-protection --[no-]dependabot --merge(squash,merge,rebase) --[no-]signoff --required-check(repeatable) --is-template --yes`

## 8. Out of scope for v1

- **Anything org-level** — org rulesets, org custom security configurations, custom properties are **Team/Enterprise only** (researched, §0); not viable on Free. No `admin:org` needed. If you upgrade to Team, the §10 ruleset templates port directly to org rulesets keyed on a `tier` custom property (§12).
- Org **secrets / variables** — already centrally managed; explicitly excluded.
- Bypass-actor **discovery** via `/orgs/{org}/installations` — replaced by the editable `repo-config/orgs/*.yaml` (§10); no `admin:org` dependency.

## 9. Risks

- Bypass-actor integration IDs are environment-specific; wrong IDs → a ruleset that locks out a bot or over-permits. Capture carefully per org; verify against a live `master` ruleset before writing.
- `required_status_checks` contexts must match exactly (incl. `integration_id`) or checks silently never satisfy. `update` should diff against live to avoid clobbering hand-tuned lists.
- `code-scanning/default-setup` PATCH can fail if a repo already has advanced (custom workflow) setup → detect and skip/warn.
- Private repo 403/422 on GHAS/rulesets → guard by visibility, don't hard-fail.
- Committing `codeql.yml`/`dependabot.yml` to a repo whose `master` ruleset blocks direct push → operator is a bypass actor (org-admin), so a direct Contents-API commit succeeds; otherwise fall back to a short-lived PR. `create` writes them before rulesets exist (clean).

---

## 10. Templated config architecture (feedback 2)

Templates live at **stack root** in `repo-config/`, `go:embed`-ed for defaults, with on-disk files (same path) taking precedence so they're editable without a rebuild:

```
repo-config/
  classes/<class>.yaml      # preset: features, merge, security toggles, codeql langs, pages, codecov default
  rulesets/master.yaml      # templated: {{.Checks}}, {{.CodeQuality}}, merge_queue params, {{.Bypass}}
  rulesets/require-approval.yaml   # near-static (1 approval, squash-only, copilot) + {{.Bypass}}
  rulesets/codecov.yaml     # {{.Bypass}}; checks codecov/patch, codecov/project
  security/codeql.yaml      # templated CodeQL advanced-setup workflow, {{.Languages}}
  security/dependabot.yaml  # templated dependabot config, {{.Ecosystems}}
  orgs/<org>.yaml           # bypass profile: team_id, app integration ids (+modes), signing reqs
  checks.yaml               # language/feature → check-context mapping (the §11 rules)
```

Render flow: Go `text/template` over the YAML → unmarshal → REST payload (or committed file). Editing a ruleset / security policy = edit YAML; no recompile. `orgs/*.yaml` decouples the per-org IDs captured in §2 from code. The class preset + per-org file + discovered checks are the three inputs to every render.

## 11. Semantic check discovery (feedback 2)

On create/update the CLI probes the repo (local checkout for `update`; chosen class/template for `create`) and derives the `master` required-checks list + CodeQL/Dependabot languages, via `cli/internal/detect` + the declarative `checks.yaml`:

| Signal detected                                  | Adds checks                                                                                                                     | Security langs / ecosystems                     |
| ------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------- |
| `go.mod`                                         | lint-go, generated-go, test                                                                                                     | codeql: go · dependabot: gomod                  |
| `package.json` (real pkg)                        | lint-node, generated-pnpm                                                                                                       | codeql: javascript-typescript · dependabot: npm |
| `pkg/js/`                                        | test-pkg-js, build-js                                                                                                           |                                                 |
| `*.proto` / `buf.yaml`                           | lint-proto                                                                                                                      |                                                 |
| Dockerfiles / compose services (`detect.Detect`) | build-`<target>` per detected target (e.g. build-rest, build-migrations, build-database, build-job-init, build-standalone-rest) |                                                 |
| has tests (`detect.DetectTests`)                 | → enable `codecov` ruleset (codecov/patch, codecov/project)                                                                     |                                                 |
| `.github/workflows` present                      |                                                                                                                                 | codeql: actions                                 |
| always                                           | GitGuardian Security Checks                                                                                                     |                                                 |

So "Go + JS + a rest Dockerfile + tests" auto-yields `lint-go, generated-go, test, lint-node, generated-pnpm, build-rest, GitGuardian…` + codecov. The UI shows the discovered set pre-ticked & editable; `--required-check`/`--no-check` adjust manually. `update` **diffs discovered-vs-live** and surfaces drift (missing/extra checks) rather than blindly overwriting hand-tuned lists. Integration IDs (15368 Actions, 46505 GitGuardian, 254 Codecov) are attached per-check from `checks.yaml`.

## 12. Related / future ideas

- **Per-`cmd/*` publish tags.** Services carry `cmd/rest`, `cmd/jobs`, `cmd/migrations`; stack already publishes its `cmd/a-novel` main. Extend `a-novel publish` to discover `cmd/*` mains and cut per-binary tags (monorepo-style `cmd/rest/vX.Y.Z`), enabling `go install …/cmd/rest@vX`. A repo's release setup (tags, release workflow, install targets) is part of its "config," so it's a natural sibling to `repo` — but it's a `publish` concern, post-v1.
- **`a-novel repo check`** — read-only, **agent-safe** drift report: diff every in-scope repo against its class template, print drift. The complement to the human-only create/update; good for cron/CI.
- **kit/.github getting-started doc** — the signoff/signing setup section promised in §0(1); ship alongside v1 since it's the user-facing half of the signoff decision.
- **Org upgrade path** — if you move to GitHub Team: migrate per-repo `master`/`require-approval` rulesets to org rulesets targeting a `tier` custom property; §10 templates port directly.

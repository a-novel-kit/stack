# Tooling and tests

- [Native first](#native-first), [Tests that earn their place](#tests-that-earn-their-place)
- [Required checks and workflows](#required-checks-and-workflows),
  [Local verification](#local-verification), [Known traps](#known-traps)

## Native first

Before writing code, check OpenTofu, the provider, Cloud Run, systemd and GitHub Actions.

| Need                          | Native answer                                                   |
| ----------------------------- | --------------------------------------------------------------- |
| Run migrations before traffic | job `run_execution_token` and `depends_on`                      |
| Gate traffic on readiness     | Cloud Run startup probe and `LATEST` traffic                    |
| Cross-root values             | `terraform_remote_state` on the producing root's outputs        |
| Deletion gate                 | a jq filter over `tofu show -json`, plus the PR label           |
| Image provenance and copy     | `gh attestation verify`; `skopeo copy --all --preserve-digests` |
| Serialize deploys             | a workflow `concurrency` group with `cancel-in-progress: false` |
| Freeze production             | `gh workflow disable deploy.yaml`                               |

- **Inline workflow steps** call `tofu`, `gh`, `gcloud`, `jq` and `curl` directly.
- **A shared step sequence** becomes a repository composite action:
  - `.github/actions/tofu` plans, enforces the policy, copies images and applies;
  - `.github/actions/health` checks production health with the shared
    `modules/smoke-check/smoke.sh`, which the caller checks out, and the ID token it passes;
  - `.github/actions/roll` moves a service's hosts onto their applied configuration.
- **Service roots are found on disk.** `deploy.yaml` applies and rolls out each one through a
  matrix, and `drift.yaml` checks each one. Only what GitHub cannot derive is listed by hand: a
  root's `plan-<root>` required check and its `roll-database.yaml` option, which
  `check-service-roots.sh` enforces.
- **Go** is reserved for programs that run on hosts. **Bash** is reserved for host scripts and
  workflow steps.
- **There is no Node toolchain.** CI runs Prettier and `renovate-config-validator` through `npx`, at
  versions Renovate tracks.

## Tests that earn their place

- **Test the committed production inputs,** because `tofu test` auto-loads `terraform.tfvars`. Mock
  providers, `override_data` the remote state, and assert security contracts:
  - no external IP;
  - internal-only ingress;
  - numeric secret versions;
  - digest-pinned images;
  - deny-all egress below the allows;
  - no owner or editor roles.
- **Do not assert literals that only restate an argument.** Do not grep docs or workflow text.
- **Test assertions see a child module's outputs, never its resources.** A resource moved into a
  module takes its assertions to that module's own `tests/`, which `validate-opentofu` runs.
- **Mocked providers panic on `import` blocks:** `tofu test` cannot run configuration-driven
  imports, and a root's tests plan its real `terraform.tfvars`. Add tests for a root only after its
  imports are removed. In a root that already has tests, gate the import behind a variable its tests
  set to `false`, or do without it.
- **Mocks need realistic values.** Service-account `email`, `member` and `name` must be well-formed,
  or provider validation fails. A data-source mock cannot override a non-computed field such as
  `name`.
- **Go tests** cover only the host programs. `tests/backup` runs inside the real database image,
  offline, for the cases this repository owns.

## Required checks and workflows

- **Every `main.yaml` job ID is a required check.**
  - Matrix jobs and reusable-workflow jobs report different context names, so give each root its
    own `plan-<root>` job that calls the composite action.
  - Renaming or removing a job needs an admin-bypass merge, then `a-novel repo update` on `master`.
- **Local actions use GitHub's `$/.github/actions/<name>` self-repository syntax.** The repository's
  actionlint config ignores its false positive; zizmor prefers it.
- **Third-party actions are pinned to their release commit,** as `owner/repo@<sha> # vX.Y.Z`. The
  rest of the fleet pins by tag; infra is the exception because its jobs mint the production
  identity, and a moved tag would run whatever it points to.
  - Before adding one, resolve the tag's commit with
    `gh api repos/<owner>/<repo>/commits/<tag> --jq .sha`, and check that it is an immutable
    release or lies on the project's default branch.
  - Renovate keeps the pins current by semver (`helpers:pinGitHubActionDigestsToSemver`). A digest
    update, the same version on another commit, means the tag moved, so it waits for review. New
    third-party releases wait three days.
  - The `a-novel-kit/workflows` actions and the managed governance workflows stay on release tags.
    `sha_pinning_required` stays off, since the shared actions reference each other by tag.
- **Only jobs that ship nothing may reuse state from other runs.** Lint and test jobs may restore
  the caches `master` runs save: `lint-repository` lints through the shared `go-actions/lint-go`. A
  job that builds, publishes or deploys what reaches production restores nothing, so its build
  state lives and dies with the job.
- **Label events re-run the whole workflow.** Never skip non-plan jobs on label events: a later
  skipped check replaces an earlier failure.
- **Keep a state change and its best-effort follow-up in separate jobs.** `downtime.yaml` writes the
  downtime in one job and hands its announcements to Google in another: an announcement failure
  leaves the downtime job green, and "Re-run failed jobs" retries only the announcements.

## Local verification

Run the whole gate the way CI does:

- `tofu fmt -check -recursive`;
- `init -backend=false`, `validate` and `test` for every root and every tested module;
- TFLint through `ghcr.io/terraform-linters/tflint` with `--recursive`;
- actionlint;
- zizmor with the fleet config (`kit/workflows/security-actions/lint-workflows/zizmor.yml`);
- shellcheck through `koalaman/shellcheck`;
- Prettier and the Renovate validator through `npx`;
- `go test` and golangci-lint through `go tool -modfile=golangci-lint.mod`.

## Known traps

- **The shell is zsh, and it does not word-split.** `set -- $pair` receives one argument. Use arrays,
  or `gh … --field Name --value V`.
- **GitHub's stock Terraform `.gitignore` ignores `*.tfvars`.** Re-include `terraform.tfvars`.
- **`git rm` and `git mv` stage immediately.** Unstage everything before committing a logical subset
  with explicit paths.
- **Provider v8 returns `retention_policy.retention_period` as a string,** so compare with
  `tonumber()`.
- **`format()` errors when its string has no verb but receives arguments.**
- **Cloud Run v2 `uri` is the hashed `*.a.run.app` URL.** Read it from a data source rather than
  copying it.
- **A plan that runs during a deploy reads half-applied state.** It shows a just-merged change as
  still to create, and template updates in roots the pull request never touched. Wait for the
  `deploy.yaml` runs to finish, then re-run the pull request's checks before comparing the plan.
- **A quota ceiling below recent usage passes the plan and fails the apply.** Google refuses it
  (`QUOTA_DECREASE_BELOW_USAGE`), and the deploy stops at foundation until a revert merges. Its
  "recent usage" outlasts the 10-minute average the quota pages describe: Cloud Run CPU was still
  refused 1.5 hours after an instance cap cut usage. Set a new ceiling at the current peak and keep
  it; bound actual usage with instance caps instead.
- **A workload identity provider's display name is capped at 32 characters,** and only the apply
  checks it. Bootstrap's test enforces the limit.
- **Cloud Workflows refuses environment variable names that begin with `GOOGLE` or `WORKFLOWS`,**
  and only the apply checks it. Bootstrap's test enforces the rule.
- **An alert condition with `evaluation_missing_data` needs a non-zero `duration`.** The plan
  passes, and Cloud Monitoring refuses the apply with a 400.
- **`gcloud` asks before cancelling or deleting, and nobody answers in CI.** Set
  `CLOUDSDK_CORE_DISABLE_PROMPTS: "1"` on the step and pass `--quiet`. A test stub that accepts every
  call hides the prompt, so make stubs refuse an unconfirmed call.
- **GitHub starts this repository's scheduled workflows hours late:** every 4 to 7 hours in
  October 2026, whatever the cron (a-novel-kit/.github#288). Never rely on `schedule:` for a
  deadline. The downtime announcements run on Cloud Workflows for that reason, the `github-dispatch`
  clock dispatches each `.github` repo's timed workflows at their own crons from Cloud Scheduler
  (`github_dispatch_schedules`), and `drift.yaml` runs far less often than its cron says.
- **A `github.token` dispatch of `deploy.yaml` fails its `require-role admin` gate.** The run belongs
  to `github-actions[bot]`, which
  [no role gate exempts](../../write-github-actions/SKILL.md#required-checks-and-rulesets).
  `deploy.yaml` runs on the `workflow_run` of `downtime` on `master` instead. That run inherits the
  downtime run's triggering actor, so the same gate holds.

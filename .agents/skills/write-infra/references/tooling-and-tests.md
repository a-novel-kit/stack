# Tooling and tests

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
  - `.github/actions/health` checks production health.
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
- **Mocked providers panic on `import` blocks.** Add tests for a root only after its imports are
  removed.
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
- **Label events re-run the whole workflow.** Never skip non-plan jobs on label events: a later
  skipped check replaces an earlier failure.

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

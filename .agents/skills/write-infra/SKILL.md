---
name: write-infra
description: >
  Plan, change, review or operate a-novel/infra: OpenTofu roots and modules, the plan-on-PR and
  apply-on-merge pipeline, Google Cloud identities and networks, the production cluster with its
  workloads and databases, recovery drills, and runbooks. Load for any change under that repository,
  and before running or handing over any `gcloud` command. Service code and migrations belong to
  their service skills.
---

# Maintain infrastructure

`a-novel/infra` is a standard GitOps OpenTofu repository:

- a pull request shows each root's plan from a read-only identity;
- merging to `master` applies the roots the merge changed since the last successful deploy:
  bootstrap, then foundation, then the services in parallel;
- a daily drift check re-plans everything.

Services deploy independently. When one needs a change from another, that change merges first, in
its own pull request.

Infrastructure only moves forward, and no procedure or workflow undoes a change. A faulty change is
fixed by a new pull request, which may revert it, and a bad release by a hotfix. Damaged data goes
back through recovery. Where a fix is slow, speed up the hotfix path rather than adding an undo.

Keep it that way. Prefer OpenTofu, provider features, Kubernetes and its operators, and native
GitHub Actions over custom code. Custom code exists only for the workflow rules that can stop or
misdeploy production, in tested `.github/scripts`, and for the health check every service shares,
`modules/smoke-check/smoke.sh`.

Load `prefer-small-solutions` throughout, `write-github-actions` for workflows, `plan-feature` for
architecture, and `choose-dependency` before adding a tool. Prose follows `document-code`.

## The repository

| Path                                 | Owns                                                                               |
| ------------------------------------ | ---------------------------------------------------------------------------------- |
| `bootstrap/`                         | Management project: state bucket, CI identities and trust, secrets, Barman buckets |
| `environments/production/foundation` | Workload project, cluster and node pool, network, egress, perimeter, budget        |
| `environments/production/cluster`    | Namespaces, add-ons, admission, workloads, databases, network policies             |
| `environments/production/<service>`  | One service's release pins and secret versions, which the cluster root reads       |
| `environments/production/grafana`    | Grafana Cloud's alerts and dashboards                                              |
| `environments/production/github`     | Both organizations' repositories, with their rulesets, teams and environments      |
| `environments/recovery-*`            | The recovery drill: a throwaway cluster, then the databases it restores            |
| `modules/`                           | Only code with two callers or one shared invariant                                 |

- **Production is one workload project and one cluster, and Kubernetes keeps the services apart.**
  Each service's namespace runs under Pod Security's restricted level, two admission policies and
  default-deny network policies, with its own service accounts, budget, database and secrets. The
  deploy checks after each apply that admission still refuses what it must. Never propose a project
  per service.
- **Inputs live in each root's committed `terraform.tfvars`.** Secret values never do: ops add them
  with `gcloud`, and External Secrets generates those the cluster chooses itself, as
  [Trust boundaries](references/change-safety.md#trust-boundaries) describes.
- **Images are pinned** as `ghcr.io/…:tag@sha256:…`. Every plan verifies each release image it
  changes: its release tag's own run built it, the release App published that tag as an immutable
  GitHub Release, and its version is not below the one deployed. The cluster's deploy then copies
  them to Artifact Registry with `skopeo --preserve-digests`.
- **Releases pin themselves.** Once all its images are published, a service or platform release
  dispatches `pin-release.yaml` through the shared `publish-actions/deploy-release` action. That
  opens their pull request as the Dependencies App, so it merges like a Renovate update. Renovate
  leaves `ghcr.io/a-novel/**` alone.
- **Bot pull requests merge alone only when they are safe to.** The auto-approver runs strict here:
  a release pin, or an update Renovate itself put on auto-merge that only moves versions. Renovate
  arms every minor and patch update except OpenTofu's and the Google providers', and a plan needing
  `allow-downtime` or `allow-resource-deletion` still holds it for a maintainer. OpenTofu and Google
  provider updates, majors, a moved action tag, and anything else a bot opens wait for a
  maintainer's or an admin's review on GitHub.
- **Secrets are pinned** as numeric versions.
- **The docs** are in `docs/ops-basics.md`, `docs/architecture.md` and `docs/runbooks/`.

## Authority

- **Agents never apply, dispatch or re-run production workflows, or change IAM by hand.** Push a
  branch and let the pull request plan it. A failed deploy job that needs a re-run goes to the
  developer as `gh run rerun <run ID> --repo a-novel/infra --failed`.
- **Use `gcloud` only for reads,** and only when the user allows it in the session and holds ops
  access ([check-access](../check-access/SKILL.md)). A useful read:
  `tofu plan -refresh=false -lock=false` with `GOOGLE_OAUTH_ACCESS_TOKEN=$(gcloud auth print-access-token)`.
  Without ops access, prove plans offline as [Change safety](references/change-safety.md) shows.
- **Name every `gcloud` target explicitly,** in a command you run and in one you hand to the
  developer: `--project`, and `--zone`, `--region` or `--location` whenever the command takes one,
  as literal values. Never rely on the active `gcloud config`, a `CLOUDSDK_*` variable, or a shell
  variable such as the runbooks' `$PROD` that the reader may not have set. A default left by other
  work runs the command on the wrong project or cluster. The values are the `*_project_id`,
  `cluster_region` and `cluster.zones` inputs in `environments/production/foundation/terraform.tfvars`.
- **Ops** hold read access, to the project and to the services' namespaces but never their
  Secrets, and secret-version rights. The named operators run Authentication's maintenance Jobs.
  Other developers hold none of them. Everything else goes through `deploy.yaml`, `downtime.yaml`
  for planned downtimes, or `recovery.yaml` for drills.
- **Write runbook commands for the identity that actually runs them.** A command ops cannot run
  belongs in a workflow.
- **Deleting, replacing or forgetting a resource, or weakening its protection,** needs the
  `allow-resource-deletion` label on the pull request, applied by a maintainer or an admin.
- **A PostgreSQL major upgrade** needs the `allow-downtime` label, applied the same way, and that
  service's planned downtime, scheduled by core, in progress.

## Route the work

- Adding a service, or changing resources, state layout, releases, databases, network policies or
  recovery: read [Change safety](references/change-safety.md).
- Adding a service or a platform, or a release that changes the spans, tables or promises Grafana
  reads: read [Grafana coverage](references/grafana.md).
- Workflows, tests, linting or tooling: read [Tooling and tests](references/tooling-and-tests.md).
- Creating, archiving or configuring a repository, or changing its rulesets, teams, labels,
  environments or secret access: edit the GitHub root, as infra's
  [Repository config](https://github.com/a-novel/infra/blob/master/docs/architecture.md#repository-config)
  describes. A repository is an entry of its `terraform.tfvars`, and its class sets the rest. A
  scoped secret joins that file only after an admin switches the org secret to selected
  repositories: one still visible to every repository fails each grant with HTTP 409. Each plan of
  the root spends the Plan App's hourly API budget, which every pull request shares, so
  `plan-github` plans only a pull request that changes the root (a-novel/infra#1380). Count the
  calls a new data source makes. `github_repository_file` pages through each file's commit
  history, so discovery reads files through `github_rest_api`, about 400 calls a plan
  (a-novel/infra#1385).
- Admitting a repository to the Drive visual-test identities: follow
  [Subscribe a repository to Drive visual tests](../write-frontend-tests/references/drive-subscription.md),
  whose later steps run after the merge.

## Deliver

Prove a refactor is a no-op before review. Report each root's expected plan in the pull request:
imports, in-place changes and deletions, with the reason for each. Separate stages that need the
deletion or downtime label from those that do not. Stack them when they depend on one another. A
stacked pull request plans only after its parent merges,
[on its next push or reopen](references/change-safety.md#trust-boundaries). In the handoff, list
what must be done by hand and who holds it: the label (a maintainer), then the admin-bypass merge
after a required-check rename and the GitHub settings the root does not manage (core).

`master` requires an approval the author's account cannot give. When the user is an admin of
a-novel/infra and opened the pull request, they review, then record it with
`gh workflow run approve-pr.yaml --repo a-novel/infra -f pull_request=<PR URL>`. The workflow
refuses anyone else's pull request, a bot's included: those wait for a maintainer's or an admin's
review on GitHub. Never dispatch it yourself: that records a review nobody did.

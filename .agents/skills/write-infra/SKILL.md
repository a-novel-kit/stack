---
name: write-infra
description: >
  Plan, change, review or operate a-novel/infra: OpenTofu roots and modules, the plan-on-PR and
  apply-on-merge pipeline, Google Cloud identities and networks, database and backup hosts, recovery
  drills, and runbooks. Load for any change under that repository. Service code and migrations
  belong to their service skills.
---

# Maintain infrastructure

`a-novel/infra` is a standard GitOps OpenTofu repository:

- a pull request shows each root's plan from a read-only identity;
- merging to `master` applies the roots the merge changed since the last successful deploy:
  bootstrap, then foundation, then the services in parallel;
- a daily drift check re-plans everything.

Services deploy independently. When one needs a change from another, that change merges first, in
its own pull request.

Keep it that way. Prefer OpenTofu, provider features and native GitHub Actions over custom code.
Custom code exists only for software that runs **on the VMs**: the TLS loader and the restore worker.

Load `prefer-small-solutions` throughout, `write-github-actions` for workflows, `plan-feature` for
architecture, and `choose-dependency` before adding a tool. Prose follows `document-code`.

## The repository

| Path                                 | Owns                                                                         |
| ------------------------------------ | ---------------------------------------------------------------------------- |
| `bootstrap/`                         | Management project: state bucket, CI identities and trust, secret containers |
| `environments/production/foundation` | Projects, VPC, firewall, database hosts, shared IAM, budget, alerts          |
| `environments/production/<service>`  | One service: Cloud Run services and jobs, backup repository, alerts          |
| `environments/recovery`              | One manual disaster-recovery drill host per disposable project               |
| `modules/`                           | Only code with two callers or one shared invariant                           |

- **Inputs live in each root's committed `terraform.tfvars`.** Secret values never do: humans add
  them with `gcloud`.
- **Images are pinned** as `ghcr.io/…:tag@sha256:…`. Every plan verifies each release image it
  changes: its release tag's own run built it, the release App published that tag as an immutable
  GitHub Release, and its version is not below the one deployed. Deploy then copies the images to
  Artifact Registry with `skopeo --preserve-digests`.
- **Releases pin themselves.** Once all its images are published, a service or platform release
  dispatches `pin-release.yaml` through the shared `publish-actions/deploy-release` action. That
  opens their pull request as the Dependencies App, so it merges like a Renovate update. Renovate
  leaves `ghcr.io/a-novel/**` alone.
- **Bot pull requests merge alone only when they are safe to.** The auto-approver runs strict here:
  a release pin, or an update Renovate itself put on auto-merge that only moves versions. Provider,
  OpenTofu and major updates, a moved action tag, and anything else a bot opens wait for
  `approve-pr`.
- **Secrets are pinned** as numeric versions.
- **The docs** are in `docs/ops-basics.md`, `docs/architecture.md` and `docs/runbooks/`.

## Authority

- **Agents never apply, dispatch production workflows or change IAM by hand.** Push a branch and let
  the pull request plan it.
- **Use `gcloud` only for reads,** and only when the user allows it in the session. A useful read:
  `tofu plan -refresh=false -lock=false` with `GOOGLE_OAUTH_ACCESS_TOKEN=$(gcloud auth print-access-token)`.
- **Humans** hold read access, IAP SSH and secret-version rights. Everything else goes through
  `deploy.yaml`, `downtime.yaml` for planned downtimes, `roll-database.yaml` for database hosts, or
  `recovery.yaml` for drills.
- **Write runbook commands for the identity that actually runs them.** A command a human cannot
  run belongs in a workflow.
- **Deleting, replacing or forgetting a resource, or weakening its protection,** needs the
  `allow-resource-deletion` label on the pull request.
- **Changing a database host's template or group configuration** needs the `allow-downtime` label,
  and that service's planned downtime in progress.

## Route the work

- Adding a service, or changing resources, state layout, releases, database hosts or recovery:
  read [Change safety](references/change-safety.md).
- Workflows, tests, linting or tooling: read [Tooling and tests](references/tooling-and-tests.md).

## Deliver

Prove a refactor is a no-op before review. Report each root's expected plan in the pull request:
imports, in-place changes and deletions, with the reason for each. Separate stages that need the
deletion or downtime label from those that do not. Stack them when they depend on one another. In the handoff,
list what the user must do by hand: label, admin-bypass merge after a required-check rename, GitHub
settings, then `a-novel repo update`.

`master` requires an approval the author's account cannot give. The user reviews, then records it
with `gh workflow run approve-pr.yaml --repo a-novel/infra -f pull_request=<PR URL>`. Never
dispatch it yourself: that records a review nobody did.

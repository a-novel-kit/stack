---
name: write-infra
description: >
  Plan, audit, and maintain a-novel/infra: OpenTofu roots, deployment tooling, Google Cloud
  identities and networks, database hosts, backups, recovery, and operator runbooks. Use for
  infrastructure isolation, rollout safety, or tooling simplification. Application migrations
  and service business logic belong to their service skills.
---

# Maintain infrastructure

Treat infrastructure as a set of independently owned service lifecycles. A successful apply proves
that a resource operation completed; it does not prove application health, data recovery, or isolation.

Load `plan-feature` for architecture changes and `choose-dependency` for build-versus-buy decisions.
Use `git-conventions` for the checkout and `document-code` for prose. Pair workflow edits with
`write-github-actions`; pair implementation and tests with the relevant language skills. Use
`use-a-novel-cli` for local validation and `open-pull-request` when shipping.

## Establish the boundary

For an operational change or audit, read the checked-out architecture, relevant root READMEs,
applicable runbook, workflow, and called tooling. Record the commit being inspected. Reconstruct the
current resource owners from source; old receipts, incident commands, and conversation history can
describe retired infrastructure.

Distinguish an audit, a repository change, and a live operation. Each has its own authority. Honor the
repository's human-only cloud execution rules: an agent must not run `gcloud` or a live OpenTofu apply.
Do not turn authorization to prepare a PR into permission to execute its migration, approve an
environment, change IAM, or bypass a deletion gate. A prior prelaunch data-loss exception belongs to
that operation. Leave unrelated automation enabled unless the maintainer explicitly requests a change.

For read-only work, inspect source, public documentation, GitHub metadata, and supplied sanitized
outputs. Never fetch secret payloads to prove configuration. Ask for the narrowest human-run check
when cloud evidence is necessary, using explicit project and location arguments where the command
supports them, from the documented environment variables. Say which conclusions are static findings
and which have live evidence.

Keep mechanical documentation edits proportional: inspect the changed text and its context, then run
the applicable formatting or link checks. They need no architecture issue or recovery drill.

## Route the work

- For deployment, IAM, networking, state ownership, database lifecycle, or recovery changes, read
  [Service isolation and failure safety](references/isolation-and-safety.md).
- For language consolidation, dependencies, code reduction, CI validation, or test removal, read
  [Tooling and tests](references/tooling-and-tests.md).
- For an operator runbook, read both references and exercise its non-mutating command path with
  fixtures. Keep human-only actions explicit and make failure stop the sequence.

## Deliver reviewable changes

Use version tags for maintained container image dependencies in infra, including Dockerfile bases.
Keep the complete published version and any required distribution suffix. Do not append SHA digests
or enable Renovate digest pinning. Generated deployments, provenance checks and recovery records may
retain resolved digests. Keep version selection separate from those exact-runtime records; never
re-resolve a historical version to decide what a rollback should restore.

Tie each batch to an end-to-end outcome and, when planning applies, its owning issue. For simplification,
start with responsibility and ownership boundaries, not the next file or language extension to port.
Separate behavior-preserving ports from changed authorization, resource ownership, or rollout policy.
Count handwritten code and tests separately from generated files, locks, and documentation; report
removed responsibilities and handoffs as well as size, without promising a percentage before measurement.

State migrations require a reviewed old-to-new ownership map, private state backup, reconciliation
plan, and rollback procedure. A moved resource must have exactly one writer throughout the transition.
Keep deployment serialization until shared state, receipts, credentials, and cleanup paths are proven
safe for independent operation.

In the handoff, distinguish code tested locally, CI results, planned cloud changes, and actions still
requiring a human. A green mocked test suite is not a completed live drill. Keep unresolved migration
safety and configurable shutdown work as linked follow-ups instead of claiming they are solved by a
tooling refactor.

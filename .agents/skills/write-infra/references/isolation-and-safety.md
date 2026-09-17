# Service isolation and failure safety

## Trace one selected service end to end

Follow image selection, provenance checks, secret metadata, planning, database changes, migrations,
candidate health, traffic, receipt publication, and compensation. For each phase, identify the
resources it reads, mutates, and can access through its identity. Check the negative case: the peer's
registry, secret, database, or backup is unavailable while the selected service's prerequisites work.

Separate service publication from runtime dependencies. Authentication can depend on the JSON Keys
API without requiring JSON Keys redeployment. A selected candidate's real dependency failure should
block promotion. An unrelated service's initializer secret or backup should not become a deployment
prerequisite merely because a shared compiler or monitor lists it.

Trace authority as well as resource layout. Dedicated VMs can still share backup credentials,
restoration authority, a deployment identity, a mutable receipt, or a state lock. Inspect the effective
plan for peer changes, including imports, replacements, scheduler toggles, and shared configuration.
Do not infer isolation from a service selector or from `-target`; a targeted plan is not an ownership
boundary.

Keep foundation, release, and recovery responsibilities explicit. Give each mutable resource and field
one owner. Exchange minimal versioned coordinates instead of giving consumers access to another
root's complete state. Review any new shared helper for authority aggregation, not just code reuse.

## Preserve the trust boundary

Treat candidate code, manifests, workflow inputs, and artifacts as untrusted until validated by
protected tooling. A plan can execute providers or external programs even with read-only credentials.
Keep untrusted PR checks cloud-blind; any credentialed candidate assessment follows the reviewed
maintainer-authorization path. Redacting output does not sandbox executable candidate code.

Validate complete image families in the deployment workflow before credentials or mutations that
require them. A required PR check cannot protect against every merge bypass. Verify immutable digests
and producer provenance; keep unchanged peer image evidence from a trusted receipt rather than
silently accepting unverified inputs.

Bind each private plan to its root, service scope, commit, configuration, content hash, and allowed
attempt. Recheck the applicable deletion authorization and policy at consumption. Unknown values or
missing evidence fail closed. Keep plans, state, provider diagnostics, and credentials out of public
logs and artifacts. A label authorizes the reviewed destructive scope; it does not waive data guards.

Inventory IAM by principal and resource. Runtime, backup, metadata-only monitor, restore, and deploy
roles have different needs. Avoid granting a monitor database passwords or backup payload access
when a metadata API can answer the check. Keep create-only backup writers and explicitly reviewed
restore authority. Check IAM inheritance and additive bindings when live evidence is available.

Review VPC routes, private DNS, ingress and egress, service invocation, database users, secret access,
and host metadata access together. Private addressing alone is insufficient. Identify paths that
bypass VPC egress controls, such as public egress from a `PRIVATE_RANGES_ONLY` Cloud Run service.
Keep public API access intentional and service-to-service access authenticated.

## Define the failure contract

An established API rollout keeps the serving revision while the candidate receives no ordinary
traffic. Verify the exact candidate with an application-level probe before promotion; readiness or
an open port is not dependency health. Describe first launch separately: no prior revision exists.
Use liveness for a stuck process, avoiding dependency-driven restart cascades.

Application compensation restores serving revisions and compatible configuration. It does not undo
migrations or concurrent database writes. Never automatically restore an old data backup as ordinary
deployment rollback. Destructive recovery needs explicit write quiescence, a reviewed recovery point,
and acceptance of the lost-write window. Expand/contract migrations remain a service responsibility.

A singleton database restart interrupts that service even when its API has multiple instances. Avoid
restarting an unchanged database. If a changed database requires disruption, bound and report it;
do not advertise zero downtime without an HA design and measured evidence. Minimum Cloud Run instances
reduce cold starts but do not make an instance permanent or in-memory work durable.

Model timeout, cancellation, runner loss, and a lost API response alongside ordinary command errors.
When a coordinator compensates after cancellation, stop and wait for its local child processes first;
give compensation a separate bounded context. This cannot cancel an already accepted cloud operation,
so reconcile live state before restoring anything.
Record sufficient private operation identity before mutation to reconcile an interrupted attempt.
Retry only operations whose safety is established by preconditions or exact execution identity.
Do not re-run a migration because its response was lost. Recovery must survive loss of the original
runner and must not alter an unselected service.

## Prove recovery rather than backup existence

Bind a recovery point to its source service and database identity, compatible database image and
major version, backup object generation, integrity evidence, and required secret versions. A completed
upload is not a restore test. Preserve old backup readers until retained recovery points expire or
have a reviewed replacement.

Restore into an isolated target before exposing services. Keep schedulers, public ingress, and the
human-only initializer absent unless the recovery plan explicitly needs them. Verify dependency
health from the permitted network, measure the full operator recovery time separately from automated
restore time, then revoke temporary access and remove only the approved disposable target.

Do not change storage lifecycle rules independently of the backup engine. Physical backups and WAL
form recovery chains; generic age-based deletion can invalidate retained backups. A new backup tool
needs an explicit IAM, retention, format-transition, and restore-proof assessment.

## Primary references

- [Cloud Run runtime contract](https://docs.cloud.google.com/run/docs/container-contract) governs
  shutdown and instance lifecycle; recheck it before making availability promises.
- [OpenTofu saved plans](https://opentofu.org/docs/cli/commands/plan/) can contain sensitive values.
- [OpenTofu remote state](https://opentofu.org/docs/language/state/remote-state-data/) explains why
  access to outputs also permits reading the underlying snapshot.
- [GitHub concurrency](https://docs.github.com/en/actions/how-tos/write-workflows/choose-when-workflows-run/control-workflow-concurrency)
  documents queue behavior; a mutex alone does not prove every release will run in dispatch order.

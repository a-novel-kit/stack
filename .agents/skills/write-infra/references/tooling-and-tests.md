# Tooling and tests

## Keep infrastructure declarative

Check OpenTofu and provider capabilities before choosing an imperative implementation. Resource
configuration, straightforward derivations, input constraints, and resource-graph tests belong in HCL
when it expresses them clearly. Aim for declarative definitions to dominate the maintained
implementation by removing custom machinery. Generating HCL from code or hiding scripts in provisioners
does not advance that goal; do not inflate configuration to meet a file or line ratio.

Use blocking validations or preconditions for deployment gates. OpenTofu `check` assertions only warn;
their failure does not stop a plan or apply. Preserve the existing fail-closed boundary when moving
validation out of a script, including private diagnostics and receipt compatibility.

## Choose from the execution environments

Inventory operator machines, CI runners, database hosts, and recovery containers before selecting a
language. The repository's accepted design owns the choice. Declarative HCL and YAML, SQL executed by
PostgreSQL, and third-party executables do not require translating everything into a general-purpose
language. Authored imperative policy should have one home; a thin launcher may only invoke it.

Consider the dependency and artifact distribution cost, credentials, host restrictions, debugging,
signal handling, and testing. Do not compare interpreter speed for control-plane code. A single
language hidden inside several shell wrappers with validation and retry logic is still several
implementations. Conversely, a new compiled CLI is not simpler merely because it is one binary.

Separate runtime dependencies from development tools with native package-manager filters. Verify a
fresh install from an empty store: a small final dependency tree does not prove unused packages were
never fetched or built. Keep development caches out of operational jobs and disable lifecycle scripts
when the operational dependencies do not need them.

Build reviewed binaries before protected inputs or cloud credentials exist; do not defer dependency
resolution to a later privileged `go run`. Embed reviewed schemas when that removes working-directory
or runtime package dependencies, and disable external schema loaders.

Keep production tooling in the infra repository unless a genuinely shared contract justifies moving
it. The local workspace CLI is not a production dependency. Publish host/job helpers as reviewed,
pinned artifacts with provenance and a usable rollback version. Prove they can run with the exact
database image before adopting a runtime that those images do not contain.

## Buy the mechanism, retain the policy

Use `choose-dependency` to compare the standard library, installed tools, and maintained dependencies.
Prefer provider resource management, official cloud clients, registry tools, and schema validators to
handwritten HTTP authentication, pagination, upload retries, format parsers, or credential helpers.
Reuse mature executables through structured argument arrays; never build command strings containing
untrusted input or secrets.

Check a cloud module's released provider constraints, transitive providers, and default IAM/lifecycle
behavior against the repository's pinned toolchain. A maintained module can still be incompatible.
Do not relax its constraints or downgrade a working provider merely to adopt it; a small native HCL
boundary can be cheaper to maintain until a compatible module removes meaningful responsibility.

Schema errors can contain private values and unexpected property names. Expose fixed messages or
reviewed schema-rule locations in public logs. Compare coercion behavior as well as valid outputs when
replacing a validator; record intentional input tightening separately from behavioral parity.

Keep the repository's service ownership, image-family contract, deletion decision, and receipt rules
explicit. A generic orchestrator does not automatically preserve them. Before adding a platform,
identify the custom files it replaces and the new operational services, IAM, state, and deployment
artifacts it introduces. Do not make both OpenTofu and a deployment controller own the same field.

Native workflow environments and concurrency should do the work they support. Verify their current
behavior from official documentation rather than recreating it or assuming they guarantee ordering,
unbounded queues, or recovery after runner loss.

GitHub's job-rerun API also reruns dependent jobs. Pin the reviewed workflow blob and verify the
dependency graph before automating a gate refresh; a safe job name alone is not an authorization
boundary. Exercise duplicate notifications, partial reruns, stale commits, and concurrent requests
without turning a failed assessment into an automatic retry loop.

Prefer a native operation ID over before/after resource-list discovery. Verify its expected scope and
commit before reporting success. A lost dispatch response is an uncertain mutation, not proof that
nothing happened: stop with an inspection path instead of resending unless the API provides a supported
idempotency guarantee. Official clients can own typed API decoding, authentication and operation
waiting without owning policy. Check retry defaults on mutation methods; retain a small adapter test
using the real client against a local server for ambiguous dispatch and cancellation. Do not build
another HTTP client, polling engine or interface hierarchy merely to test the SDK boundary.

Check the lifetime of a provider's request-ID guarantee; a bounded deduplication window is not a
permanent replay defense. A create-only intent reservation differs from a success receipt: finding
identical saved intent does not prove dispatch never happened. Preserve ambiguous intent and reconcile
the exact native identity read-only. Scope reservations to the domain operation; changing a retry UUID
must not open another dispatch path for that intent. Bind saved intent to native resource UIDs where
names can be reused. Keep rendering, rollout and final receipt completion distinct.

Consolidate duplicated policy into named domain operations. Use a standard option parser with a
separate option set per operation; it can reject irrelevant flags without a second permission matrix.
Explicit project, region, service, and operation inputs make one-shot commands reproducible. Derive
ephemeral coordinates instead of requiring users to keep a large shell session alive. Return opaque
identifiers on stdout and bounded, non-sensitive diagnostics on stderr.

## Simplify ownership before implementation

Trace a complete release or recovery, including failure, before choosing a batch. Map its resource
writers, credentials, durable records, and configuration transformations. Shared ownership can create
whole families of peer-preservation guards and compensation code; changing that boundary can remove
more machinery than porting its helpers. Preserve the guards until the replacement boundary is proven.

Compare delegating the whole capability with retaining a small implementation. Name the code, formats,
and tests each option removes, and the services, IAM, configuration, and recovery obligations it adds.
Fewer sources of truth and cross-layer handoffs matter more than a short file. A locally larger module
is worthwhile when it retires a subsystem; a generic step engine wrapped around old scripts is not.

Keep navigation predictable: repeated service declarations, explicit domain operations, typed internal
contracts, and narrow adapters to maintained tools. Confine untyped external documents to boundaries.
Do not duplicate schema rules in Go or create interface layers without a real boundary to isolate.
Keep exceptional bootstrap and recovery paths out of routine rollout logic where their contracts allow.

Retire a complete capability and its redundant tests together; porting tests first is not a prerequisite.
Trace entry points, docs, and trusted workflow references. Measure production tooling and tests separately,
including new builds, wrappers, and dependencies. A test-only language port does not reduce operational
Bash; a larger compatibility layer is not consolidation.

Treat the launcher contract as part of a language transition: prerequisites, checkout selection,
stdout, failure codes, and cancellation must remain usable from the documented shell. `go run`
normalizes a program's nonzero exit status; callers must stop on any failure rather than depend on
its numeric code. Trace indirect callers when replacing a shared helper: a surviving shell command
or its CI job may now need Go before it can validate its inputs. Reuse an existing toolchain-equipped
test job where practical rather than multiplying setup steps. Report both the batch delta and the
cumulative delta from the initiative baseline. Separate authored code from dependency metadata,
including isolated development-tool pins; a smaller source diff can still grow the repository.

Verify dependency automation discovers isolated tool modules and refreshes their checksums, not just
their visible version pins. Prefer manager-native artifact updates before allowing custom update commands.

Exercise the same fixtures against old and new implementations during the transition. Once parity and
rollout evidence are accepted, delete the superseded implementation and temporary parity harness in that
batch. A later incident must not leave two active code paths with different safety behavior.

Separate policy changes from language ports when both are needed. Preserve receipt and backup format
compatibility until their retention obligations end. Retire one-time migration code only after source,
state, and supported recovery artifacts no longer depend on it; age alone is not evidence.

## Keep tests that protect a decision

Use a small set of test layers with different jobs:

- Pure contract tests cover scope selection, image-family consistency, schema validation, and plan
  policy, including malformed and unknown inputs.
- Workflow tests inspect structured permissions, checkout provenance, environment selection, and
  credential ordering. Prefer parsing YAML over matching its formatting.
- Adapter and state-transition tests inject errors before and after each meaningful mutation. Verify
  private output handling, ambiguous outcomes, bounded retries, and peer preservation.
- Mocked provider tests check the resource graph and IAM/network contracts. Confirm every provider is
  mocked and the backend disabled before running them without cloud authority.
- Human-approved isolated drills establish real restore, traffic, and failure evidence when a change
  affects those guarantees. Mock success cannot substitute for them; unrelated mechanical edits do
  not need a new drill.

For each existing test, name the regression it catches and whether another layer already covers that
contract. Keep one authoritative check at the layer that owns it. Remove assertions about internal
variable names, incidental wording, or exact source spelling once behavior is covered. Retain focused
static checks when the trust boundary itself is static, such as an untrusted checkout before cloud
authentication. Parse documented commands with their native shell. Execute credential-sensitive
examples only with isolated command fakes: verify that failures stop subsequent requests and that
tokens stay out of arguments and output. Syntax checks alone cannot prove those properties.

Use explicit table cases and small fakes, not Cartesian products of unrelated conditions.
Keep each case's input change and expected verdict visible together. Prefer a small semantic result
comparison over assertion walls; do not hide evidence in a fixture DSL or compress cases into long lines.
Prefer standard test servers over CLI emulators. Keep orchestration fixtures minimal; invoke the real
compiler at compiler-to-adapter boundaries instead of repeating its full setup for every state-machine
case. Reuse its receipt builder when testing compensation artifacts, while retaining focused CLI
contract tests. Give subprocess tests an explicit environment and
allowlisted executable path so a missing fake cannot invoke a real cloud client. Record unexpected
fake calls separately: an adapter's expected error mapping must not hide a broken fixture.
For in-memory Go polling tests, use `testing/synctest` to exercise real timers and cancellation
without production clock-injection hooks. Keep subprocess fixtures outside the virtual-time bubble.
Readiness fixtures should distinguish temporary initialization from the final serving process and
exercise the real cleanup path. Bound helper-process lifetimes independently of that cleanup; killing
the parent command does not guarantee its descendants exit.
Exercise independent rejection conditions separately so one failure cannot mask another missing check.
Give secret-version fixtures distinct values, including
different current and rollback versions, to detect crossed mappings and checks against the wrong
configuration. Assert safety-critical ordering inside the fake mutation:
a saved plan must already be consumed when apply starts. Checking only the final state misses a
replay window. A replacement dependency still needs an adapter contract
test for the assumptions the repository relies on. Prefer documented dry-run interfaces when testing
Renovate instead of importing its private modules. Verify which stages the dry run reaches: Renovate's
local lookup reports update candidates but does not create branches or enforce a PR's minimum group
size. Cover the repository's declared policy separately and limit claims to the exercised stages.

Prioritize broad, low-cost behavior coverage plus security-sensitive edge cases. Do not multiply
fixtures or assertions to chase a coverage percentage. Report test duration and maintenance burden
alongside code size; removing a critical safety check is not an acceptable line-count reduction.

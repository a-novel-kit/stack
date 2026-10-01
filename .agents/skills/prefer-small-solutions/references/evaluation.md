# Simplicity and loading evaluation

Use these scenarios when modifying shared skills. Read the entry point and the references it routes
to, then check that the expected decision and required constraints are reachable. This structural
walkthrough does not prove how a model will behave.

## Scenarios

1. **Shared-invariant bug.** A transfer endpoint fails to reject an overdraft; withdrawals use the
   same debit operation. Expect caller inspection, a repair at the invariant's owner, and a
   regression covering the unnamed sibling path. Reject a transfer-only patch or caller-specific
   policy moved into a generic helper.
2. **Small service change.** Add a core operation with one DAO dependency. Load base Go, service
   architecture, operations, data access if transactions are affected, telemetry, and relevant tests.
   Preserve the consumer interface, validation ownership, error identity, and per-layer reporting.
3. **Existing capability.** A shared package already exposes the required logging or parsing
   behavior. Reuse its public API and preserve the local integration contract. Do not introduce a
   duplicate wrapper or repeat a full dependency survey for unchanged, documented usage.
4. **Dependency decision.** A requirement needs a mature parser or cryptographic subsystem.
   Load `choose-dependency`; evaluate vetted candidates. Reject a small bespoke implementation
   justified only by dependency avoidance.
5. **Native UI control.** A date field can use native HTML. Load frontend and applicable UI,
   framework, and test guidance. Preserve the agreed interaction, controller contract where
   stateful, browser baseline, accessibility, and live Storybook review. Reject a one-line control
   that drops those requirements.
6. **Already-small implementation.** A clear direct function meets its contract. Make no cosmetic
   rewrite or new abstraction just to demonstrate the ladder. A required one-line security check
   is still worth testing; line count does not determine risk.
7. **Cache semantics.** A request requires expiring data. Reuse a cache only if expiry,
   invalidation, and concurrency meet the contract. Reject replacing TTL with an unbounded-lifetime
   memoizer because its invocation is shorter.
8. **Routine documentation edit.** Update one verified configuration row in an existing README.
   Load applicable structure/editorial/input guidance; retain unknown content. Do not load every
   scaffold template, recreate the file, or ask again for known repository metadata.
9. **Scoped Go test.** Update an existing regression. Load the CLI entry point and
   `testing-building.md`, Go test conventions/patterns, and the applicable service layer pattern.
   Do not load daemon, secrets, repository governance, or release runbooks unless the operation
   actually needs them.
10. **PR status versus feedback.** A status query loads the survey and reports evidence. An
    authorized feedback task also loads classification and closing-the-loop guidance before
    replies or resolution. Preserve unresolved discussions and the bot-comment identity rule.
11. **Publication after agreement.** An approved change is ready to publish. Reuse the agreement;
    load authoring, applicable checks, and handoff rules at their stages. Do not restart abstract
    planning or infer permission to merge from green CI.
12. **Transaction and error behavior.** A shorter DAO implementation appears to remove a guard or
    conversion. Verify atomicity, absence semantics, public error mapping, and observability before
    deletion. A database constraint does not replace authorization.

## Routing regression checks

These cases verify that shorter descriptions and split files still reach the original rules. Check
the catalog trigger first, then the entry point and selected references; preserved text alone is
insufficient if the task no longer loads it.

- **Testing-only request:** `develop-feature` still governs test timing without a production edit.
- **Compatible API addition across repos:** `manage-versions` loads for proto/REST contract changes,
  including non-breaking ones; it is not limited to breaking symbols or unreleased dependencies.
- **Existing API composition:** client-defined JSON, several service calls, or a long-running/paid
  operation loads `plan-client-server-boundary` even without changing an endpoint signature.
- **Review without edits:** Go service reviews load the affected layer and telemetry references;
  Go test reviews load patterns and applicable fixtures. Missing spans must not bypass telemetry.
- **Existing fixture change:** editing or reviewing test data reaches the fixture rules for
  `testdata/`, format choice, reuse, and isolation from production packages.
- **Branch naming only:** Git routing reaches the commit types and scope table before naming.
- **Push to an existing draft:** readiness is rechecked after the push; when completion gates pass,
  load authoring and tracking metadata and mark ready in the same turn.
- **Frontend companion skills:** Svelte tests/stories reach framework and test rules; uikit reaches
  design-system rules; a service REST client reaches both `write-js-package` and `write-frontend`.
- **Debugging a Svelte module:** the trigger reaches `write-svelte` for `.svelte.ts`, route loads,
  actions/hooks, package exports, and compiler configuration.
- **Library placement without implementation:** deciding whether something belongs in `golib`
  loads `write-go-kit` and its stricter admission/dependency rules.

## Optional agent comparison

When evaluating behavioral claims, run the same representative repository tasks in fresh, isolated
contexts with the same model and tool configuration. Compare the previous skills with the proposed
skills; add an upstream Ponytail condition only when evaluating direct adoption. Prevent plugins or
global instructions from contaminating the baseline. Repeat tasks to expose nondeterminism.

Treat required behavior, security, architecture, tests, and authorized scope as pass/fail gates.
Then compare maintained code after formatting, dependencies, files, loaded instruction volume,
elapsed time, token use where available, and reviewer corrections. Exclude generated artifacts
from maintained-code counts. Record model, source revisions, prompts, failures, and limitations.
Do not infer runtime or cost savings from fewer skill lines or reuse upstream benchmark percentages.

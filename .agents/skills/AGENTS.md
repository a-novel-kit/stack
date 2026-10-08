# Maintaining the shared skills

Keep the authoritative skill sources in stack. Publish changes on a feature branch with a PR;
preserve workspace discovery links and keep personal installations outside this workflow.

## Choose the smallest useful instruction change

1. Identify the observed failure or repeated loading cost. Check the existing owner before adding
   another instruction, skill, reference, or script.
2. Preserve established product, architecture, security, dependency, testing, and publication
   decisions. Reorganization changes where a rule is read, not what it permits.
3. Prefer one authoritative rule with links from related skills. Merge only when tasks share the
   same trigger and workflow; split when a distinct task can load a materially smaller relevant
   reference. File count and word count are signals, not goals.
4. Make directions operational: state what to inspect or do, the qualifying condition, and when
   to stop. A decision ladder must preserve the same required behavior at every step.
5. Keep skill names stable unless a rename is needed. Put the task and trigger first in a concise
   description, and retain explicit boundaries. Check companion guidance and UI metadata after edits.

## Route detailed material

Keep mandatory cross-cutting constraints and the loading decision in `SKILL.md`. Move long command
catalogs, templates, examples, and layer-specific procedures to `references/` when they are needed
for only a subset of invocations. State exactly when each reference must be read before action.
Link every reference directly from its owning entry point; avoid chains of routing documents.
References over 100 lines need a contents list that names their useful sections.

A selected reference's rules remain mandatory. Instructions already available in context need not
be reread. Resume from the current task and stage; do not restart the entire workflow after loading
a companion skill. Preserve consent and decisions already given.

## Verify the result

- Check frontmatter, skill names, descriptions, and relevant `agents/openai.yaml` metadata.
- Check local links, moved section anchors, and inbound links across the skillset. Keep code samples
  and template links distinct from links to skill resources.
- Account for moved instructions and review every deletion for lost policy, exceptions, or failure
  handling. Preserve examples verbatim unless their content itself needs correction.
- Walk representative prompts through the entry points and the references they select. Use
  [the simplicity and routing scenarios](prefer-small-solutions/references/evaluation.md) when
  changing shared guidance. Record a structural walkthrough separately from actual agent trials.
- Run the applicable repository formatting and lint checks. Report any unavailable checks.
- Describe meaningful behavior changes, loading changes, and validation in the PR. Do not claim
  model-quality, latency, or token savings from document size alone.

When adapting third-party text, retain source revision and licensing notices near the adapted
material. Upstream suggestions remain subordinate to the workspace's established decisions.

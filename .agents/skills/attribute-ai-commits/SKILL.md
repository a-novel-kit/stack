---
name: attribute-ai-commits
description: >
  Add GitHub-linked AI co-authors to Git commit trailers without inventing provider identities.
  Use whenever an AI agent that created or materially changed committed content writes, amends, or
  reviews a commit message. Trust the repository registry for known agents and defer missing-agent
  discovery to the register-ai-agent skill.
---

# Attribute AI Commits

Keep the human as Git author and committer. Add the contributing AI agent as a standard
`Co-authored-by:` trailer only when the repository registry maps its email to a real,
provider-controlled GitHub account.

## Resolve the current agent

Determine the agent product that performed the work. Do not substitute the model provider for a
third-party agent client: a Mistral model running inside another coding agent belongs to that
client.

Look up the agent without network access:

```bash
node <skill-dir>/scripts/coauthor-registry.mjs lookup <agent>
```

Interpret the result:

- Exit `0`, `status: verified`: use the registered identity. Do not revalidate it.
- Exit `2`, `status: unavailable`: omit the co-author trailer. Do not search again.
- Exit `3`, `status: missing`: load and follow the `register-ai-agent` skill.
- Exit `1`: treat it as an operational error and omit the trailer.

The pushed registry is the durable memory. Do not create a private cache, Git config entry, expiry,
or periodic verification job for registered agents. The initial registry is an exhaustive baseline
of mainstream repository-capable coding and code-review agents as of its `as_of` date; inspect it
with `coauthor-registry.mjs list`.

## Write the trailer

When a verified agent materially contributed to the commit, append:

```text
<commit subject and optional body>

Co-authored-by: <display name> <verified email>
```

Use the exact token `Co-authored-by:` and keep one blank line before the trailer block. Put each
co-author on its own line with no blank lines between co-authors.

Build the display name as follows:

- Start with the registry `label`.
- Append the exact model display name when the runtime exposes it reliably.
- Do not guess or derive a hidden model name.

Examples:

```text
Co-authored-by: Codex GPT-5.6 Sol <noreply@openai.com>
Co-authored-by: Claude Opus 4.8 (1M context) <noreply@anthropic.com>
Co-authored-by: gemini-cli Gemini 3.5 Pro <218195315+gemini-cli@users.noreply.github.com>
```

GitHub links by the verified email, so adding the model to the display name preserves the provider
account mapping while making model usage visible.

Do not add the trailer when the agent only inspected or reviewed the work, formatted a user-written
commit message, or performed no material work included in the commit.

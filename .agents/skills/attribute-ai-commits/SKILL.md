---
name: attribute-ai-commits
description: >
  Add GitHub-linked AI co-authors to Git commit trailers without inventing provider identities.
  Use whenever an AI agent that created or materially changed committed content writes, amends, or
  reviews a commit message. Trust the repository registry for known agents; when the current agent
  is missing, discover and verify its own identity and open a PR that updates the registry.
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
python3 <skill-dir>/scripts/coauthor_registry.py lookup <agent>
```

Interpret the result:

- Exit `0`, `status: verified`: use the registered identity. Do not revalidate it.
- Exit `2`, `status: unavailable`: omit the co-author trailer. Do not search again.
- Exit `3`, `status: missing`: follow [Register a missing agent](#register-a-missing-agent).
- Exit `1`: treat it as an operational error and omit the trailer.

The pushed registry is the durable memory. Do not create a private cache, Git config entry, expiry,
or periodic verification job for registered agents. The initial registry is an exhaustive baseline
of mainstream repository-capable coding and code-review agents as of its `as_of` date; inspect it
with `coauthor_registry.py list`.

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

## Register a missing agent

Only the missing agent may register its own identity. Never guess another provider's account or use
another provider's bot.

1. Search official provider documentation, official repositories, and GitHub for the agent's
   canonical co-author identity.
2. Require evidence that the exact email resolves to the expected provider-controlled account:
   - A numeric `users.noreply.github.com` address must contain the matching GitHub account ID and
     login.
   - A provider-domain address needs a public proof commit where GitHub's rendered author mapping
     links that email to the expected account.
3. Verify the candidate programmatically:

   ```bash
   python3 <skill-dir>/scripts/coauthor_registry.py verify \
     --agent <agent> \
     --label '<display label>' \
     --github-login '<login>' \
     --email '<email>' \
     [--proof-commit '<github-commit-url>']
   ```

4. On success, add the emitted entry to `references/providers.json` with `status: verified`.
5. If diligent official-source research finds no valid identity, add an `unavailable` entry with a
   concise reason and evidence URL. Do not use a syntactically plausible but unlinked email.
6. Validate the registry, commit the change, push it, and open a PR. Include it in the current
   feature PR when one already exists; otherwise use a dedicated `docs/skills/register-<agent>` PR.
7. Use the new verified identity only after its registry entry exists on the branch. Never rewrite
   already-published commits merely to add attribution.

The registry review is the trust boundary. Existing entries are intentionally not rechecked during
ordinary commits; correcting or retiring one requires another reviewed registry change.

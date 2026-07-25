---
name: register-ai-agent
description: >
  Discover, programmatically verify, and register a GitHub-linked AI co-author identity. Use only
  when attribute-ai-commits reports status missing for the current agent, or when explicitly asked
  to add a new agent to the repository co-author registry. Publish the registry change
  through a pull request.
---

# Register AI Agent

Register only the current missing agent. Never guess another provider's account or use another
provider's bot. The registry review is the trust boundary.

## Discover and verify

1. Search official provider documentation, official repositories, and GitHub for the agent's
   canonical co-author identity.
2. Require evidence that the exact email resolves to the expected provider-controlled account:
   - A numeric `users.noreply.github.com` address must contain the matching GitHub account ID and
     login.
   - A provider-domain address needs a public proof commit where GitHub's rendered author mapping
     links that email to the expected account.
3. Verify the candidate programmatically:

   ```bash
   node <skill-dir>/scripts/verify-coauthor.mjs \
     --agent <agent> \
     --label '<display label>' \
     --github-login '<login>' \
     --email '<email>' \
     [--proof-commit '<github-commit-url>']
   ```

The command exits `0` only when the identity resolves to the expected GitHub account. It emits a
registry-ready verified entry and stores rendered proof hashes in validated fragments so secret
scanners do not mistake public commit IDs for credentials.

## Update the registry

1. On successful verification, add the emitted entry to the sibling
   `attribute-ai-commits/references/providers.json` registry.
2. If diligent official-source research finds no valid identity, add an `unavailable` entry with
   `aliases`, `status`, `reason`, `evidence`, and `checked_at`. Do not use a syntactically plausible
   but unlinked email.
3. Update the registry `as_of` date and validate it:

   ```bash
   node <skills-dir>/attribute-ai-commits/scripts/coauthor-registry.mjs validate
   ```

4. Commit the change, push it, and open a PR. Include it in the current feature PR when one already
   exists; otherwise use a dedicated `docs/skills/register-<agent>` PR.
5. Use the new verified identity only after its registry entry exists on the branch. Never rewrite
   already-published commits merely to add attribution.

Existing registry entries are durable memory and are intentionally not rechecked during ordinary
commits. Correct or retire one only through another reviewed registry change.

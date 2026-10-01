# Document inputs and missing values

Read this reference when routed here by [write-project-docs](../SKILL.md). Its rules apply to the selected work.

## Phase 1: Collect Required Inputs

Before scaffolding a new file, ask the user for the inputs below, in a single message rather than
one question at a time. In **update** mode, ask only for the inputs relevant to the section being
edited.

### 1.1 Always required

| Input                | Example                     | Default for Agora        |
| -------------------- | --------------------------- | ------------------------ |
| Project display name | `JSON Keys service`         | (ask)                    |
| Repo path (org/repo) | `a-novel/service-json-keys` | `a-novel/<service-slug>` |
| Main branch          | `master`                    | `master`                 |

### 1.2 Required for README.md

| Input               | Example                                                   | Default / Fallback                                                                                       |
| ------------------- | --------------------------------------------------------- | -------------------------------------------------------------------------------------------------------- |
| Main CI workflow    | `main.yaml`                                               | `main.yaml`                                                                                              |
| Codecov graph token | `almKepuGQE` (public token, safe to commit)               | Omit the `?token=…` query parameter entirely and leave a `TODO(project-docs)` comment next to the badge. |
| Twitter handle      | `agorastoryverse`                                         | `agorastoryverse`                                                                                        |
| Discord invite ID   | numeric ID `1315240114691248138` + invite code `rp4Qr8cA` | same as existing services                                                                                |

The codecov graph token is **public** — it controls badge/graph rendering only, not repo access,
so committing it is safe (see [Badge Catalog](badges.md#badge-catalog)).

### 1.3 Required for SECURITY.md

| Input                       | Example                                | Default                                |
| --------------------------- | -------------------------------------- | -------------------------------------- |
| Org / project display label | `A-Novel` (used in running prose)      | `A-Novel`                              |
| Security contact email      | `geoffroy.vincent@agorastoryverse.com` | `geoffroy.vincent@agorastoryverse.com` |

### 1.4 Required for CONTRIBUTING.md

| Input                    | Example                                     | Default                                                                                                                        |
| ------------------------ | ------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------ |
| Project slug             | `service-json-keys`                         | (ask — used in page title)                                                                                                     |
| Developer onboarding URL | `a-novel-kit/.github/blob/master/README.md` | `a-novel-kit/.github/blob/master/README.md` (single canonical onboarding doc for both orgs; there is NO org `CONTRIBUTING.md`) |

### 1.5 Capability flags (shape template output)

Ask the user which of these apply. Each flag turns a section of README/CONTRIBUTING on or
off:

| Flag               | What it enables                                                     |
| ------------------ | ------------------------------------------------------------------- |
| `has-grpc`         | gRPC compose examples + `grpcurl` interaction snippets              |
| `has-rest`         | REST compose examples + `curl` interaction snippets                 |
| `has-standalone`   | Standalone (all-in-one) image in addition to split images           |
| `has-go-client`    | `pkg/go` usage example in README, Go client section in CONTRIBUTING |
| `has-js-client`    | `pkg/js` usage example in README, JS client section in CONTRIBUTING |
| `has-openapi-docs` | Link to GitHub Pages docs in README + redocly/scalar mention        |
| `has-cron-jobs`    | Scheduled-job section (like rotate-keys) in CONTRIBUTING            |

When an input is unknown or the user declines to provide it, insert an HTML TODO comment
(see [Handling Missing Values](#phase-3-handling-missing-values)) rather than guessing or leaving
the field blank.

---


## Phase 3: Handling Missing Values

When an input is required but not available, write an HTML TODO comment at the exact
location where the value belongs. Format:

```markdown
<!-- TODO(project-docs): <what is missing> — <where to get it> -->
```

HTML comments do not render in GitHub's Markdown preview, so the file still looks clean to
visitors, while grep finds them instantly:

```bash
grep -rn "TODO(project-docs)" .
```

Examples:

```markdown
[![codecov](https://codecov.io/gh/a-novel/service-json-keys/graph/badge.svg)](https://codecov.io/gh/a-novel/service-json-keys) <!-- TODO(project-docs): add ?token=<graph-token> from codecov.io/gh/a-novel/service-json-keys/settings > Badge if a tokenized badge is required -->
```

```markdown
Report security bugs by emailing the lead maintainer at <!-- TODO(project-docs): security contact email -->.
```

**Do not** invent values. A fake email or a `YOUR_TOKEN_HERE` placeholder that a reader might
mistake for real content is worse than a visible TODO.

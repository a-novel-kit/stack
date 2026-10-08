# Badge patterns

Read this reference when routed here by [write-project-docs](../SKILL.md). Its rules apply to the selected work.

## Badge Catalog

Copy these patterns verbatim — the exact URL format matters (shields.io is strict about
path segments). Parameters are marked with `{{…}}`.

| Badge                    | Markdown pattern                                                                                                                                                                                                                     |
| ------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| Twitter follow           | `[![X (formerly Twitter) Follow](https://img.shields.io/twitter/follow/{{twitter-handle}})](https://twitter.com/{{twitter-handle}})`                                                                                                 |
| Discord                  | `[![Discord](https://img.shields.io/discord/{{discord-id}}?logo=discord)](https://discord.gg/{{discord-invite-code}})`                                                                                                               |
| Go version (from go.mod) | `![GitHub go.mod Go version](https://img.shields.io/github/go-mod/go-version/{{repo-path}})`                                                                                                                                         |
| File count               | `![GitHub repo file or directory count](https://img.shields.io/github/directory-file-count/{{repo-path}})`                                                                                                                           |
| Code size                | `![GitHub code size in bytes](https://img.shields.io/github/languages/code-size/{{repo-path}})`                                                                                                                                      |
| CI workflow status       | `![GitHub Actions Workflow Status](https://img.shields.io/github/actions/workflow/status/{{repo-path}}/{{main-workflow-file}})`                                                                                                      |
| Codecov badge            | Default (no token): `[![codecov](https://codecov.io/gh/{{repo-path}}/graph/badge.svg)](https://codecov.io/gh/{{repo-path}})` — add `?token={{codecov-graph-token}}` to the badge URL only when the repo requires a tokenized variant |
| Codecov sunburst         | Default (no token): `![Coverage graph](https://codecov.io/gh/{{repo-path}}/graphs/sunburst.svg)` — add `?token={{codecov-graph-token}}` to the image URL only when a tokenized variant is required                                   |

**Codecov graph token — not a secret.** It is the public badge token from
`codecov.io/gh/<repo>/settings > Badge`, and committing it is intentional. The private upload
token (used by the CI `report-codecov` job) lives in repo secrets, never here.

# Working principles

Workspace skills live in `.agents/skills/` and are shared through `a-novel-kit/stack`.
Publish skill changes on a feature branch with a pull request. Use these repository copies
in this environment; keep personal skill installations outside this workflow.

Cleanup only ever touches what your own session created, unless the developer explicitly names a
wider scope. "Clean up the worktrees", "close the issues" and every request like them mean this
session's worktrees, branches, stacks, containers, issues and pull requests, never the rest of the
workspace. Another session's checkout can be clean, merged and idle while that session still runs in
it, so leave everything you did not create in this session untouched; when ownership is unclear, ask.
See [workspace hygiene](.agents/skills/git-conventions/references/workspace.md#when-you-are-done).

Prefer the smallest complete solution: fewer lines of maintained code, fewer moving parts,
and clear, idiomatic control flow. Preserve required behavior and the project's architecture,
dependency policy, security, and testing standards. Simplify the whole affected path before
shortening individual functions; do not compress formatting or hide complexity to reduce line count.

Before planning, writing, changing, or reviewing code, load
[prefer-small-solutions](.agents/skills/prefer-small-solutions/SKILL.md). Keep its decision rule active
through implementation and final review. For software development, also load
[develop-feature](.agents/skills/develop-feature/SKILL.md) to choose the current stage: local draft,
issue and draft-PR review, approved-scope testing, or final cleanup. Load other skills according to
the work they govern.

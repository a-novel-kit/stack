# Working principles

Workspace skills live in `.agents/skills/` and are shared through `a-novel-kit/stack`.
Publish skill changes on a feature branch with a pull request. Use these repository copies
in this environment; keep personal skill installations outside this workflow.

Prefer the smallest complete solution: fewer lines of maintained code, fewer moving parts,
and clear, idiomatic control flow. Preserve required behavior and the project's architecture,
dependency policy, security, and testing standards. Simplify the whole affected path before
shortening individual functions; do not compress formatting or hide complexity to reduce line count.

Before planning, writing, changing, or reviewing code, load
[prefer-small-solutions](.agents/skills/prefer-small-solutions/SKILL.md). Keep its decision rule active
through implementation and final review. Load other skills according to the work they govern.

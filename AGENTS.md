# Working principles

Workspace skills live in `.agents/skills/` and are shared through `a-novel-kit/stack`.
Publish skill changes on a feature branch with a pull request. Use these repository copies
in this environment; keep personal skill installations outside this workflow.

Never touch what you do not own unless the developer explicitly says to. You own only what your
current session created: its worktrees, branches, stacks, containers, issues and pull requests. A
bare "clean up" or "clear", of worktrees, issues or anything else, stays inside that set; a global
cleanup that owns the whole workspace is always requested explicitly. Another session's checkout can
be clean, merged and idle while that session still runs in it, so when ownership is unclear, ask.
Remove what you own by exact name, never with a store-wide prune, and state that rule in every prompt
that delegates work able to create or remove anything. See
[workspace hygiene](.agents/skills/git-conventions/references/workspace.md#when-you-are-done).

When the developer asks for a command, your next reply is that command and nothing else. Read only
what its arguments need, then hand it over. Leave out the procedure around it, the follow-up
commands and the checks it suggests: the developer is usually acting on a clock, as during a
planned downtime, and each extra step delays them. Verify and monitor after the hand-off, and
report only what they ask for.

When a step needs the developer's own hands, such as a secret, a web console setting or an approval,
hand it over ready to act on: the exact command with every value you know filled in, a direct link
to the runbook section that covers it, or both when the step mixes a console and a command. Never
only name the step.

You act with the developer's access, and most developers hold less than these skills describe.
Before an operation they may lack, such as an approval, a release, a privileged label, a repository
or org setting, a secret, Google Cloud or Discord, check their access with
[check-access](.agents/skills/check-access/SKILL.md). When they lack it, say what is missing and who
holds it, then stop: never look for a workaround. A command they ask for but cannot run gets that
answer instead of the command.

Commands handed to the developer to run must paste cleanly into zsh with oh-my-zsh, which bash
accepts too. Put a space before a `;`, `&` or `|` that follows a URL: `url-quote-magic` escapes it
on paste, so `…/pull/12; done` arrives as `…/pull/12\; done` and fails to parse. Quote arguments
holding `[]`, `?` or `*`, list loop items literally, and quote every expansion: zsh neither splits
an unquoted `$VAR` into words nor passes an unmatched glob through.

Write no em dash (—) in anything you produce: interface text, documentation, comments, commits,
issues, pull requests and replies. It spends width for nothing and reads as machine-written. Use a
period, a colon, a comma or parentheses instead. A page title separates the page from the brand
with `|`, and an empty table cell stays empty.

Prefer the smallest complete solution: fewer instructions for a reader to hold, fewer moving parts,
and clear, idiomatic control flow. Preserve required behavior and the project's architecture,
dependency policy, security, and testing standards. Simplify the whole affected path before
shortening individual functions; do not compress formatting or hide complexity to reduce line count.

Before planning, writing, changing, or reviewing code, load
[prefer-small-solutions](.agents/skills/prefer-small-solutions/SKILL.md). Keep its decision rule active
through implementation and final review. For software development, also load
[develop-feature](.agents/skills/develop-feature/SKILL.md) to choose the current stage: local draft,
issue and draft-PR review, approved-scope testing, or final cleanup. Load other skills according to
the work they govern.

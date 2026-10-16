# Work with an agent

Most work on Agora goes through an AI agent. The stack ships the team's conventions as skills, and an agent started at its root follows them across every repository.

## Start a session

[Set up an agent](./setup) first, then open Claude Code at the stack root, from anywhere:

```bash
a-novel claude
```

With Codex, run `codex` from the stack root.

## Hand over a task

Describe the outcome in plain words, such as a bug to fix or a feature to build. The agent takes it from draft to pull request, running the CLI itself.

Review its work as you would a colleague's, and answer when it asks.

## What stays yours

Security-key touches, secret values, approvals, releases and repository settings need you, by design.

## Run several agents

Give each agent its own [stack](/cli/stacks), so their branches and services never collide. Their notes on GitHub go out [as the bot](/cli/bot-comments).

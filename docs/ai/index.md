# Work with an agent

Most of the work on Agora goes through an AI agent. The stack ships the team's conventions as skills: how work is planned, written, tested and reviewed in every kind of repository. An agent started at the stack root follows them, with every repository in reach.

## Start a session

[Set up an agent](./setup) first, then open Claude Code at the stack root, from anywhere:

```bash
a-novel claude
```

With Codex, run `codex` from the stack root.

## Hand over a task

Describe the outcome in plain words: a bug to fix, a feature to build, a pull request to review. The agent follows the team's process from draft to pull request. It plans, writes the code, runs tests and services through the CLI, commits and opens the pull request.

Review its work as you would a colleague's, and answer when it asks.

## What stays yours

Some steps need you, by design:

- touching your security key when it signs commits;
- typing secret values, which the CLI never takes from a pipe;
- approving pull requests, releasing, and applying repository settings.

## Run several agents

Give each agent its own [stack](/cli/stacks), so their branches and services never collide. Their notes on GitHub go out [as the bot](/cli/bot-comments).

# Work with an agent

The workspace is optimized for AI agents. The stack ships the team's conventions as skills, and an agent started at its root follows them across every repository. Agents are a tool, not a requirement: the code and tools stay easy to audit and use by hand, as the [runbooks](/cli/ui) show.

## Start a session

Open your agent with the stack root as its workspace, as [set up](./setup) for your tool.

## Hand over a task

Describe the outcome in plain words, such as a bug to fix or a feature to build. The agent takes it from draft to pull request, running the CLI itself.

Review its work as you would a colleague's, and answer when it asks. Secret values stay yours to type: [`secrets set`](/cli/secrets) reads them from your keyboard, and no agent can read them back.

## Run several agents

Give each agent its own [stack](/cli/stacks), so their branches and services never collide. Their notes on GitHub go out [as the bot](/cli/bot-comments).

# Manage secrets

Some services need a secret to run, such as an API key. The CLI keeps such values encrypted on your machine and hands them only to the programs that need them, through their environment. No command ever prints a value, so a secret stays out of your terminal, your logs and your shell history, even when an AI agent drives the toolchain.

## Store a secret

Create the store once:

```bash
a-novel secrets init
```

Then store each secret under an ID. The value is typed with no echo, and the command refuses to read from a pipe, so set secrets yourself from a terminal:

```bash
SECRET_ID=openai-key
a-novel secrets set "$SECRET_ID"
```

```bash
a-novel secrets ls                 # the IDs, never the values
a-novel secrets rm "$SECRET_ID"    # delete one
```

## Which secrets a repository needs

A repository that needs secrets commits a manifest, `.a-novel/secrets.yaml`, mapping each environment variable to a secret ID. It holds no values, so it is safe to commit:

```yaml
secrets:
  - env: OPENAI_API_KEY
    id: openai-key
    description: OpenAI API key used by narrative generation.
```

`a-novel test`, `a-novel run` and the terminal UI read the manifest and pass each declared secret into the environment of what they start. A secret you have not set is skipped with a warning naming its ID and description, so the rest still runs. Ask the team on Discord for a value you do not have.

## The GitHub Packages token

Platform repositories build their images with your [GitHub Packages token](/dependencies/node#configure), under the ID `github-packages-token`. Store the same token you put in `~/.npmrc`:

```bash
a-novel secrets set github-packages-token
```

## Run any command with a secret

`secrets exec` passes secrets to a command outside the CLI's own verbs. Each `--env` maps an environment variable to a secret ID:

```bash
a-novel secrets exec --env OPENAI_API_KEY=openai-key -- go run ./cmd/rest
```

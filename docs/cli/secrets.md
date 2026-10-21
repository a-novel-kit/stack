# Manage secrets

Some services need secrets, such as API keys. The CLI stores them encrypted and passes them only to the programs that need them, through their environment. No `a-novel` command prints a value.

## Store a secret

Store each secret under an ID, typing the value yourself:

```bash
SECRET_ID=openai-key
a-novel secrets set "$SECRET_ID"
```

```bash
a-novel secrets ls                 # the IDs, never the values
a-novel secrets rm "$SECRET_ID"    # delete one
```

## What a repository needs

A repository declares its secrets in `.a-novel/secrets.yaml`, by ID and without values:

```yaml
secrets:
  - env: OPENAI_API_KEY
    id: openai-key
    description: OpenAI API key used by narrative generation.
```

`test`, `build`, `run` and the UI pass them along. A missing secret prints a warning that names it. Ask the team on Discord for its value.

## The GitHub Packages token

Platform image builds need your [GitHub Packages token](/dependencies/node#configure):

```bash
a-novel secrets set github-packages-token
```

## Any other command

```bash
a-novel secrets exec --env OPENAI_API_KEY=openai-key -- go run ./cmd/rest
```

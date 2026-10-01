# Installation, launch, and secrets

Read this reference when routed here by [use-a-novel-cli](../SKILL.md). Its rules apply to the selected work.

## `a-novel secrets` — local encrypted secrets

A local encrypted store for values a repo needs but must never commit — injected into a child
process's environment **only**, never printed, logged, or placed on a command line by any command.
Secrets are encrypted at rest with AES-256-GCM under a `0600` local key; `set` reads the value with
no echo.

```bash
a-novel secrets init                              # create the local key + store dir (idempotent)
a-novel secrets set <id>                          # read a value with no echo, store it encrypted
a-novel secrets ls                                # list secret ids (never values)
a-novel secrets rm <id>                           # delete a secret
a-novel secrets exec --env NAME=<id> -- <cmd>     # run <cmd> with the secret in its env only
```

**Auto-injection.** A service repo can commit a value-free manifest at `.a-novel/secrets.yaml` — a
`secrets:` list of `{env, id, optional description}` — and the declared secrets are injected
automatically into the child env of `a-novel test`, `a-novel run` and `a-novel run ui`. A
declared-but-unset secret is **skipped with a descriptive warning**, never failed silently. The
manifest carries no values, so it is safe to commit.

## `a-novel install` — rebuild and reinstall the CLI

The dev-loop reinstall cycle in one command: checkpoint daemon state, rebuild and install the binary
from source, then restart the daemon and replay the checkpoint — so the running containers and
go-exec targets survive the swap. Equivalent to `a-novel core prepare-reinstall` →
`go install ./cmd/a-novel` → `a-novel core start`, in order.

```bash
a-novel install                                   # rebuild from <default-stack>/cli, state-preserving
a-novel install --source ~/forks/stack/cli        # build from a different checkout
```

Source defaults to `<default-stack>/cli` (typically `~/git-projects/a-novel/cli`). After it exits,
`a-novel core status` reports the freshly-built binary's version and the same targets that were
running before. Run it after editing the CLI itself.

## `a-novel claude` — launch Claude Code from the stack root

Launches Claude Code with the stack root as its working directory, so the domain skills in
`.agents/skills` and the whole `app/` + `kit/` workspace are in scope no matter where you invoked it
from. Arguments pass straight through to the underlying `claude` CLI.

```bash
a-novel claude                                    # interactive session, rooted at the stack
a-novel claude -p "<prompt>"                      # non-interactive: print and exit
```

## `a-novel version` — print the CLI version

```bash
a-novel version                                   # print the installed a-novel CLI version
```

The binary version the daemon reports (`a-novel core status`) can diverge from this after you
rebuild the source without reinstalling; `a-novel install` is what reconciles the two.

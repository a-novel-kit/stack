# Local service operations

Read this reference when routed here by [use-a-novel-cli](../SKILL.md). Its rules apply to the selected work.

## `a-novel run` — daemon-backed service operations

The entire surface for starting, stopping, observing, and inspecting locally-running
services. Requires the a-novel daemon (`a-novel core start`; lives in `~/.zshrc`
after `a-novel core setup`).

Run it from a single repo, or from the stack root — where it fans out across every `app/service-*`
**and** `app/platform-*` checkout, so a platform's dev-server `run`/`run:*` script shows up in the
picker beside the services' targets.

### Lifecycle

```bash
a-novel run start <service>/<target>          # go-exec mode (default)
a-novel run start <service>/<target> --mode=container
a-novel run kill <service>/<target>
a-novel run restart <service>/<target>
a-novel run service infra start <service>     # bring up infra + auto-run one-shots
a-novel run service infra kill <service>      # refuses if any target running
a-novel run service infra kill <service> --force  # cascade-kill
```

The supervisor **auto-walks dependencies**: `a-novel run start service-X/rest`
brings up postgres, runs migrations + rotate-keys (one-shots), then starts rest.
Mutual exclusion is enforced (refuses with hint if the target is already running
in the other mode). One-shots are tracked per infra-up session and re-run on every
`infra start`; they are idempotent by contract, so re-applying migrations locally
is by design.

### Observability

```bash
a-novel run ps                                # list services + target states
a-novel run topology --service=<svc>          # ASCII dep tree
a-novel run logs <service>/<target>           # snapshot
a-novel run logs <service>/<target> --follow  # stream live
a-novel run logs <service>/<target> --previous  # most recent archived run
a-novel run env <service>                     # shell-evalable env block
eval "$(a-novel run env <service>)"           # inject env into your shell
```

The daemon writes JSON-line logs to `~/.local/state/a-novel/logs/...` (current +
5 archived runs per target). `run logs` reads from there; `--follow` subscribes
through the daemon so multiple followers see the same stream.

### Volumes (service-scoped)

```bash
a-novel run volume list <service>
a-novel run volume backup <service> --tag=<label>
a-novel run volume restore <service> [--from=<timestamp>]
a-novel run volume clear <service> [--no-backup]
```

All destructive ops (backup/restore/clear) refuse while the service is up. Pass
`--force` to cascade-stop first. Backups land in `~/.local/share/a-novel/backups/`
as `tar.zst` archives (max 5 per volume, oldest pruned).

### TUI

```bash
a-novel run ui                                # full-screen TUI
# Inside: ? for help, Esc for command palette, q to quit
```

The TUI is a thin client over the same RPCs as the CLI — actions taken in the UI
are observable from `a-novel run watch` and vice-versa. Agents and CI use the discrete
verbs instead, see [Driving the CLI non-interactively](../SKILL.md#driving-the-cli-non-interactively-agents-ci-scripts).

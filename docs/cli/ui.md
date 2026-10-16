# Use the terminal UI

The terminal UI is the everyday view of the workspace: every service and its state, each target's live logs, and a command palette to start and stop them. It is a client of the daemon like the commands are, so anything you do in it shows in `a-novel run ps`, and the other way around.

```bash
a-novel run ui
```

The left pane lists the services. The right pane holds one tab per target, a runnable program of the service, and per infrastructure container such as its database, with the selected tab's logs below.

## Move around

| Keys                     | Action                                              |
| ------------------------ | --------------------------------------------------- |
| `↑` `↓`, or `j` `k`      | Select a service                                    |
| `←` `→`, `h` `l`, or Tab | Select a target or infrastructure tab               |
| PgUp, PgDn               | Scroll the logs half a page; PgUp pauses the follow |
| `Ctrl+↑`, `Ctrl+↓`       | Scroll the logs one line                            |
| Home or `g`, End or `G`  | Jump to the oldest line, or back to the live tail   |
| `?`                      | Show every key and command                          |
| `q` or `Ctrl+C`          | Quit; the daemon and its services keep running      |

## Run commands

Press Esc to open the palette, type a command, then Enter. Commands act on the selected service and tab.

| Command            | Action                                                                 |
| ------------------ | ---------------------------------------------------------------------- |
| `:start`           | Start the target as a host process, its dependencies first             |
| `:start container` | Start the target in its container                                      |
| `:kill`            | Stop the selected target or infrastructure container                   |
| `:restart`         | Stop, then start again                                                 |
| `:infra-start`     | Bring up the service's infrastructure and run its one-shot jobs        |
| `:infra-kill`      | Stop the service's infrastructure; add `force` to stop its targets too |
| `:volume-backup`   | Snapshot the service's data                                            |
| `:topology`        | Show the dependency graph                                              |
| `:refresh`         | Reload services and logs now                                           |

The UI needs a person at the keyboard. Scripts and agents use the commands of [Run services](./services) instead.

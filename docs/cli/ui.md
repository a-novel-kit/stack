# Use the terminal UI

The terminal UI is the everyday view of the workspace: every service and its state, each target's live logs, and a command palette to start and stop them. It shares its state with the commands: what you start in one, the other sees.

```bash
a-novel run ui
```

The left pane lists the services; the right pane shows the selected one's targets and infrastructure as tabs, with the logs below. Move with the arrow keys. Esc opens the command palette, which starts, stops and restarts what is selected, and `?` lists every key and command. `q` quits and leaves the services running.

The UI needs a person at the keyboard. Scripts and agents use the commands of [Run services](./services) instead.

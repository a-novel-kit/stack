// Package tui implements the terminal UI behind `a-novel run ui`, the
// daemon-backed dashboard for the local service stack. Every action
// routes through the same RPC client the CLI uses, so the UI and CLI
// always observe the same daemon state.
//
// The screen is two frames side by side, the service navigator and the
// selected service's pane, above a status bar and a footer hint.
//
//	╭────────────────────────╮╭────────────────────────────────────────────╮
//	│ Services               ││infra ● {postgres}                          │
//	│                        ││target ● [grpc]  ○ [rest]                   │
//	│ ▸ service-x            ││──────────────────────────────────────────  │
//	│     ● 1/2 targets      ││grpc · go-exec · running pid=4242           │
//	│     ● 1/1 infra        ││15:04:05 out <log line>                     │
//	│                        ││15:04:05 err <log line>                     │
//	│   service-y            ││                                            │
//	│     ○ 0/1 targets      ││                                            │
//	│       (no infra)       ││                                            │
//	╰────────────────────────╯╰────────────────────────────────────────────╯
//	✓  Started grpc
//	? help  ·  Esc command  ·  ↑↓ service  ·  ←→ target  ·  PgUp/PgDn scroll logs  ·  q quit
//
// The footer lists the common keys, Esc opens a command palette with
// autocomplete, and ? opens the help screen.
package tui

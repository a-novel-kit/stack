// Package tui implements the terminal UI behind `a-novel run ui`, the
// daemon-backed dashboard for the local service stack. Every action
// routes through the same RPC client the CLI uses, so the UI and CLI
// always observe the same daemon state.
//
// The screen is a two-column dashboard: a service navigator on the
// left and a per-target detail-and-log pane on the right, with a
// status bar and a footer hint below.
//
//	┌──────────────┬──────────────────────────────────────────────────┐
//	│ Services     │ Tabs: [t1] [t2] [t3]                             │
//	│              │ ┌──────────────────────────────────────────────┐ │
//	│ ● svc-X 2/4  │ │ <target detail header>                       │ │
//	│ ○ svc-Y 0/4  │ │ ────────────────────────────────────────── │ │
//	│              │ │ <log lines, follow-end>                      │ │
//	│              │ │                                              │ │
//	│              │ └──────────────────────────────────────────────┘ │
//	│              │ <footer hint>                          [Esc] cmd │
//	└──────────────┴──────────────────────────────────────────────────┘
//
// The footer lists the common keys, Esc opens a command palette with
// autocomplete, and ? opens the help screen.
package tui

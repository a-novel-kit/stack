// Package logs stores each target's output and streams it live.
//
// A target's runs live in one directory, which keeps the five latest archived
// runs:
//
//	$XDG_STATE_HOME/a-novel/logs/<stack>/<service>/<target>/
//	├── current.log              ← the run in progress
//	└── run-<timestamp>.log      ← an archived run
//
// Each line is one JSON object, {ts, stream, line}, with the ANSI escapes of the
// line kept, so the CLI and TUI render colored output and jq filters by stream.
//
// The Store is also the streaming hub. Every line the runner appends reaches each
// subscriber of the target, and the subscribers' channels close when the process
// terminates.
package logs

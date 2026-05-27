// Package logs owns the daemon-side log storage and streaming hub.
//
// Storage shape (spec §7.1):
//
//	$XDG_STATE_HOME/a-novel/logs/<stack>/<service>/<target>/
//	├── current.log              ← currently-streaming run
//	├── run-<timestamp>.log      ← archived previous runs (max 5)
//	└── ...
//
// Format: one JSON object per line — {ts, stream, line} — preserving
// ANSI escapes in `line` so the CLI/TUI can render colored output and
// `jq` can filter by stream.
//
// Streaming hub: the Store also owns subscriber channels per (stack,
// service, target). When the runner appends a line, every subscriber
// gets it; the hub closes the channel when the proc terminates. The
// StreamLogs RPC handler attaches a subscriber and forwards lines over
// connect-rpc until the channel closes or the client disconnects.
package logs

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/a-novel-kit/stack/cli/internal/shared/paths"
)

// Stream is one of stdout / stderr — matches the proto enum but kept as
// a local type so this package doesn't depend on the generated code.
type Stream string

const (
	StreamStdout Stream = "stdout"
	StreamStderr Stream = "stderr"
)

// Line is one log entry as we serialize it to disk and forward to
// subscribers.
type Line struct {
	Ts     time.Time `json:"ts"`
	Stream Stream    `json:"stream"`
	Line   string    `json:"line"`
}

// maxArchivedRuns is the per-target retention. On every fresh start, the
// existing current.log rotates to run-<timestamp>.log and the oldest
// archives past this count are pruned.
const maxArchivedRuns = 5

// flushInterval bounds how long a buffered line can sit in memory
// before reaching disk + subscribers. 100ms keeps live `--follow`
// feeling instant (spec §7.1 performance note).
const flushInterval = 100 * time.Millisecond

// Store is the daemon's per-target log registry. Concurrency-safe;
// one Store per daemon.
type Store struct {
	mu      sync.RWMutex
	streams map[string]*targetStream // key: targetID = stack/service/target
}

// targetStream owns the file handle, the subscriber list, and the
// buffered writer for one target. Created lazily on first OpenForWrite.
type targetStream struct {
	mu          sync.Mutex
	id          string // targetID
	dir         string // logs dir for this target
	current     *os.File
	encoder     *json.Encoder
	subscribers []chan Line // each subscriber's delivery channel
	closed      bool        // once the proc terminates, no new lines
}

// New returns an empty Store. Subscriber maps populate on OpenForWrite /
// Subscribe; nothing is touched on disk until a target actually emits.
func New() *Store {
	return &Store{streams: make(map[string]*targetStream)}
}

// OpenForWrite prepares the log file for a fresh run of `targetID`. The
// existing current.log (if any) rotates to a timestamped archive, the
// oldest archives are pruned, and a new current.log is created. Returns
// a Writer the runner can pipe stdout / stderr through.
func (s *Store) OpenForWrite(targetID, stack, service, target string) (*Writer, error) {
	dir := filepath.Join(paths.LogsRoot(), stack, service, target)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("mkdir log dir %s: %w", dir, err)
	}
	currentPath := filepath.Join(dir, "current.log")
	// Rotate the existing current.log if present.
	if info, err := os.Stat(currentPath); err == nil && info.Size() > 0 {
		stamp := info.ModTime().UTC().Format("2006-01-02T15-04-05Z")
		archivePath := filepath.Join(dir, "run-"+stamp+".log")
		_ = os.Rename(currentPath, archivePath)
		pruneOldRuns(dir)
	}
	f, err := os.OpenFile(currentPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", currentPath, err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	ts := &targetStream{
		id:      targetID,
		dir:     dir,
		current: f,
		encoder: json.NewEncoder(f),
	}
	// Replace any prior stream record for this target (the prior run is
	// over; its subscribers were closed when the previous proc died).
	if prior, ok := s.streams[targetID]; ok {
		prior.close()
	}
	s.streams[targetID] = ts
	return &Writer{ts: ts}, nil
}

// Subscribe adds a delivery channel for `targetID` and returns it. The
// channel is buffered modestly (32 slots); slow subscribers that don't
// drain get dropped lines silently — the daemon never blocks the runner
// on a stuck client. Caller MUST receive from the channel until it's
// closed; the close signal means "no more lines coming."
//
// Returns (nil, false) if no stream exists yet for the target — the
// caller can poll back later or read the archived run instead.
func (s *Store) Subscribe(targetID string) (<-chan Line, bool) {
	s.mu.RLock()
	ts, ok := s.streams[targetID]
	s.mu.RUnlock()
	if !ok {
		return nil, false
	}
	ts.mu.Lock()
	defer ts.mu.Unlock()
	if ts.closed {
		// Stream is already over; return a closed channel so the
		// subscriber gets a clean EOF immediately.
		ch := make(chan Line)
		close(ch)
		return ch, true
	}
	ch := make(chan Line, 32)
	ts.subscribers = append(ts.subscribers, ch)
	return ch, true
}

// CloseTarget marks the target's stream as done — closes the file
// handle, closes every subscriber channel, removes the stream record.
// Called by the runner when an instance reaches PHASE_TERMINATED.
func (s *Store) CloseTarget(targetID string) {
	s.mu.Lock()
	ts, ok := s.streams[targetID]
	if ok {
		delete(s.streams, targetID)
	}
	s.mu.Unlock()
	if ts != nil {
		ts.close()
	}
}

// ListRuns returns the timestamps of every archived run for `targetID`,
// newest first.
func (s *Store) ListRuns(stack, service, target string) []string {
	dir := filepath.Join(paths.LogsRoot(), stack, service, target)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		name := e.Name()
		if len(name) < len("run-") || name[:4] != "run-" {
			continue
		}
		// strip "run-" prefix and ".log" suffix
		stamp := name[4 : len(name)-len(".log")]
		out = append(out, stamp)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(out)))
	return out
}

// CurrentPath returns the path of the active current.log for a target.
func (s *Store) CurrentPath(stack, service, target string) string {
	return filepath.Join(paths.LogsRoot(), stack, service, target, "current.log")
}

// RunPath returns the path of an archived run.
func (s *Store) RunPath(stack, service, target, runID string) string {
	return filepath.Join(paths.LogsRoot(), stack, service, target, "run-"+runID+".log")
}

// close terminates the stream: encodes any pending buffered lines (none
// — the Writer flushes per write), closes the file, closes every
// subscriber channel.
func (ts *targetStream) close() {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	if ts.closed {
		return
	}
	ts.closed = true
	if ts.current != nil {
		_ = ts.current.Close()
	}
	for _, ch := range ts.subscribers {
		close(ch)
	}
	ts.subscribers = nil
}

// pruneOldRuns removes archived runs beyond maxArchivedRuns, oldest first.
func pruneOldRuns(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	type run struct {
		name string
		mt   time.Time
	}
	var runs []run
	for _, e := range entries {
		name := e.Name()
		if len(name) < len("run-") || name[:4] != "run-" {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		runs = append(runs, run{name: name, mt: info.ModTime()})
	}
	if len(runs) <= maxArchivedRuns {
		return
	}
	// Oldest first; remove all but the newest maxArchivedRuns.
	sort.Slice(runs, func(i, j int) bool { return runs[i].mt.Before(runs[j].mt) })
	for _, r := range runs[:len(runs)-maxArchivedRuns] {
		_ = os.Remove(filepath.Join(dir, r.name))
	}
}

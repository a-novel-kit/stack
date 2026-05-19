package runner

import (
	"regexp"
	"strings"
)

// maxLines bounds a process's retained scrollback. A local dev tool does not
// need infinite history, and an unbounded buffer is exactly what made the
// viewport re-wrap an ever-growing multi-KB string every tick and flicker.
const maxLines = 5000

// maxPartial caps the un-terminated tail. A process that never emits a newline
// (rare, but a raw byte stream could) must not grow memory without bound; past
// this we flush what we have as a line.
const maxPartial = 64 << 10

// Escape-sequence sanitation. A raw subprocess emits far more than colour:
// cursor moves, erase-line, scroll-region, alt-screen, OSC title/clipboard,
// DCS. Dumped verbatim into a Bubble Tea viewport these corrupt lipgloss's
// width/wrap maths and, because some move the real cursor, visibly bleed into
// the host terminal. We keep SGR (colour) — which lipgloss renders correctly —
// and strip everything else.
var (
	// CSI: ESC [ params intermediates final. SGR ends in 'm' and is kept;
	// every other final (J/K/H/l/h/A/B…) is dropped.
	csiRe = regexp.MustCompile("\x1b\\[[0-9;?]*[ -/]*[@-~]")
	// OSC: ESC ] … (BEL | ST). Titles, colour queries, clipboard — all dropped.
	oscRe = regexp.MustCompile("\x1b\\][^\x07\x1b]*(?:\x07|\x1b\\\\)")
	// Lone C0/C1 controls except TAB (0x09), LF (0x0a), CR (0x0d, handled
	// separately) and ESC (0x1b — must survive here so the SGR sequences
	// kept by csiRe above are not decapitated).
	ctlRe = regexp.MustCompile("[\x00-\x08\x0b\x0c\x0e-\x1a\x1c-\x1f\x7f]")
)

// SanitizeLine makes one raw output line safe to place inside a lipgloss /
// viewport region: carriage-return progress collapsed to its final state,
// non-SGR escapes removed, stray control bytes removed. Colour survives.
// Exported so the UI can run console-command output through the exact same
// containment (raw \r / cursor moves were bleeding into the log column).
func SanitizeLine(s string) string {
	// CR progress (spinners, percentage bars) overwrites the line in a real
	// terminal; keep only the segment after the last CR so it does not
	// accumulate as dozens of stale frames.
	if i := strings.LastIndexByte(s, '\r'); i >= 0 {
		s = s[i+1:]
	}
	s = oscRe.ReplaceAllString(s, "")
	s = csiRe.ReplaceAllStringFunc(s, func(seq string) string {
		if strings.HasSuffix(seq, "m") { // SGR colour — keep
			return seq
		}
		return ""
	})
	return ctlRe.ReplaceAllString(s, "")
}

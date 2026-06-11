package discovery

import (
	"bufio"
	"os"
	"strings"
)

// dockerfileHasHealthcheck reports whether the given Dockerfile contains a
// non-NONE HEALTHCHECK instruction. Used as a fallback for classification
// when the compose service itself doesn't declare a healthcheck (§11 rule 2
// allows either location during the migration window).
//
// We do a structural scan rather than a full Dockerfile parse: we read line
// by line, skipping continuations (`\` at EOL), and look for a top-level
// instruction starting with `HEALTHCHECK`. `HEALTHCHECK NONE` is treated as
// "no healthcheck" (the official way to opt out of an inherited check).
//
// Errors (missing file, unreadable) return (false, nil) — discovery is
// lenient about Dockerfile parsing because the compose file is the
// authoritative source for classification, and a missing Dockerfile fails
// loudly at build/run time anyway.
func dockerfileHasHealthcheck(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer func() { _ = f.Close() }()

	sc := bufio.NewScanner(f)
	// Allow long lines — multi-line RUN blocks with continuations easily
	// blow past 64KB.
	sc.Buffer(make([]byte, 0, 1<<20), 1<<20)
	var logical strings.Builder
	for sc.Scan() {
		line := sc.Text()
		trimmed := strings.TrimRight(line, " \t")
		// Continuation: append the line minus the trailing backslash, keep
		// accumulating until the logical line ends.
		if strings.HasSuffix(trimmed, "\\") {
			logical.WriteString(strings.TrimSuffix(trimmed, "\\"))
			logical.WriteString(" ")
			continue
		}
		logical.WriteString(trimmed)
		text := strings.TrimSpace(logical.String())
		logical.Reset()
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}
		upper := strings.ToUpper(text)
		if strings.HasPrefix(upper, "HEALTHCHECK ") || upper == "HEALTHCHECK" {
			// Reject the explicit-disable form. Anything else with
			// HEALTHCHECK at the start counts as declaring a healthcheck.
			rest := strings.TrimSpace(text[len("HEALTHCHECK"):])
			if strings.EqualFold(rest, "NONE") {
				continue
			}
			return true
		}
	}
	return false
}

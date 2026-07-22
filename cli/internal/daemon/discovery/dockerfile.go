package discovery

import (
	"bufio"
	"os"
	"strings"
)

// dockerfileHasHealthcheck reports whether the Dockerfile at path declares a
// HEALTHCHECK other than NONE. It classifies a target when the compose service
// declares no healthcheck of its own, since the check may live in either place.
//
// The scan is structural: it reads line by line, joins continuations, and looks
// for a top-level instruction starting with `HEALTHCHECK`. `HEALTHCHECK NONE`
// counts as no healthcheck, being the official opt-out from an inherited check.
// A missing or unreadable file reports false, since the compose file is the
// authoritative source and a missing Dockerfile fails loudly at build time.
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
		// Continuation: drop the trailing backslash and keep accumulating
		// until the logical line ends.
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
			// HEALTHCHECK NONE is the explicit-disable form; anything else
			// counts as declaring a healthcheck.
			rest := strings.TrimSpace(text[len("HEALTHCHECK"):])
			if strings.EqualFold(rest, "NONE") {
				continue
			}
			return true
		}
	}
	return false
}

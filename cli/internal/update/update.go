// Package update implements a best-effort "a newer version is available" notice
// for the a-novel CLI. It is deliberately non-fatal: every failure (offline,
// proxy down, unparseable data, unwritable cache) is swallowed, because a
// version check must never disrupt or slow a command. The latest version is
// cached so the network is hit at most once per checkInterval, not on every
// invocation; in between, the cached value drives the notice.
package update

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/mod/semver"

	"github.com/a-novel-kit/stack/cli/internal/shared/paths"
)

const (
	// latestURL is the Go module proxy's "latest version" endpoint for the CLI
	// module — the very source `go install …@latest` resolves against, so the
	// suggested version is exactly what an update would install. The trailing
	// path is the cli/ submodule; the proxy maps it to the latest cli/vX.Y.Z tag.
	latestURL = "https://proxy.golang.org/github.com/a-novel-kit/stack/cli/@latest"

	// installPath is the package users `go install` to update.
	installPath = "github.com/a-novel-kit/stack/cli/cmd/a-novel"

	// disableEnv opts out of the check entirely (for CI or anyone who finds it
	// noisy), matching the convention of other update notifiers.
	disableEnv = "A_NOVEL_NO_UPDATE_CHECK"

	// checkInterval throttles the network check; between checks the cached
	// latest version is reused.
	checkInterval = 24 * time.Hour

	// httpTimeout caps the proxy request so a slow network never stalls a
	// command by more than this on the once-a-day refresh.
	httpTimeout = 2 * time.Second

	// maxResponseBytes bounds how much of the proxy response is read before
	// decoding — the @latest JSON is a few hundred bytes; 64 KiB is ample.
	maxResponseBytes = 64 << 10

	cacheFile = "update-check.json"
)

// cacheEntry is the on-disk record of the last check.
type cacheEntry struct {
	LastCheck time.Time `json:"lastCheck"`
	Latest    string    `json:"latest"`
}

// Notify writes a short "update available" suggestion to w — two lines: the
// available version, then the install command — when a tagged release newer than
// current exists. current is version.String(): a non-release value (a commit
// hash, "dev", "(devel)") is skipped, since there is nothing to compare a local
// build against. Honors the A_NOVEL_NO_UPDATE_CHECK opt-out. All errors are
// intentionally ignored.
func Notify(w io.Writer, current string) {
	if os.Getenv(disableEnv) != "" || !semver.IsValid(current) {
		return
	}
	latest := latestVersion()
	if !shouldNotify(current, latest) {
		return
	}
	_, _ = fmt.Fprintf(w, "a-novel %s is available — you're on %s.\n", latest, current)
	_, _ = fmt.Fprintf(w, "  Update: go install %s@latest\n", installPath)
}

// shouldNotify reports whether latest is a valid release strictly newer than
// current (both already known to be candidate versions). Pure, so the decision
// is unit-testable without touching the network or the cache.
func shouldNotify(current, latest string) bool {
	return semver.IsValid(current) && semver.IsValid(latest) && semver.Compare(latest, current) > 0
}

// latestVersion returns the latest CLI release version, served from the cache
// while it is fresh and otherwise fetched from the proxy (refreshing the cache).
// Returns "" on any failure.
func latestVersion() string {
	path := filepath.Join(paths.State(), cacheFile)
	// Require the cached version to be valid, not just fresh — otherwise a
	// corrupt / hand-edited cache with a recent timestamp would suppress the
	// check for the whole interval instead of triggering a re-fetch.
	if entry, err := readCache(path); err == nil &&
		time.Since(entry.LastCheck) < checkInterval && semver.IsValid(entry.Latest) {
		return entry.Latest
	}
	latest := fetchLatest()
	if latest != "" {
		_ = writeCache(path, cacheEntry{LastCheck: time.Now(), Latest: latest})
	}
	return latest
}

// fetchLatest queries the module proxy for the latest version, bounded by
// httpTimeout. Returns "" on any error or non-200 response.
func fetchLatest() string {
	ctx, cancel := context.WithTimeout(context.Background(), httpTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, latestURL, nil)
	if err != nil {
		return ""
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return ""
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return ""
	}

	var info struct {
		Version string `json:"Version"`
	}
	// The @latest payload is a few hundred bytes; bound the read so an
	// unexpectedly large response can't balloon memory.
	if json.NewDecoder(io.LimitReader(resp.Body, maxResponseBytes)).Decode(&info) != nil {
		return ""
	}
	return info.Version
}

func readCache(path string) (cacheEntry, error) {
	var entry cacheEntry
	raw, err := os.ReadFile(path)
	if err != nil {
		return entry, err
	}
	err = json.Unmarshal(raw, &entry)
	return entry, err
}

// writeCache writes entry atomically — to a temp file in the same directory,
// then rename into place — so a concurrent invocation or an interrupted write
// can never leave a half-written cache that disables the check until the
// interval expires.
func writeCache(path string, entry cacheEntry) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	raw, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), cacheFile+".*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }() // no-op once the rename succeeds
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

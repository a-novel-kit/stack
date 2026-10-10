// Package retention bounds the archives a directory accumulates, such as
// archived log runs and volume backups, by keeping the most recently modified.
package retention

import (
	"os"
	"path/filepath"
	"slices"
	"time"
)

// Files returns the paths of the regular files in dir whose name satisfies
// match, oldest modification first. An unreadable dir yields none.
func Files(dir string, match func(name string) bool) []string {
	entries, _ := os.ReadDir(dir)
	type file struct {
		path string
		mod  time.Time
	}
	var files []file
	for _, e := range entries {
		if e.IsDir() || !match(e.Name()) {
			continue
		}
		if info, err := e.Info(); err == nil {
			files = append(files, file{path: filepath.Join(dir, e.Name()), mod: info.ModTime()})
		}
	}
	slices.SortStableFunc(files, func(a, b file) int { return a.mod.Compare(b.mod) })
	paths := make([]string, len(files))
	for i, f := range files {
		paths[i] = f.path
	}
	return paths
}

// Prune removes every file Files returns except the keep most recent.
func Prune(dir string, match func(name string) bool, keep int) {
	files := Files(dir, match)
	for _, path := range files[:max(len(files)-keep, 0)] {
		_ = os.Remove(path)
	}
}

// Package fsutiltest writes the file trees tests build their fixtures from.
package fsutiltest

import (
	"os"
	"path/filepath"
	"testing"
)

// Write creates each file of files under root, keyed by its slash-separated
// path relative to root, with any missing parent directory. A write error is a
// broken test setup, so it panics.
func Write(t *testing.T, root string, files map[string]string) {
	t.Helper()

	for name, content := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			panic(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			panic(err)
		}
	}
}

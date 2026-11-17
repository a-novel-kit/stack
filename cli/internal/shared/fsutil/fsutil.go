package fsutil

import "os"

// Exists reports whether path exists. A worktree's .git is a file, so a
// checkout test cannot require a directory.
func Exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

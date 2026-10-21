package setup

import (
	"os"
	"path/filepath"
	"testing"
)

// TestCloneStack pins that a failed clone removes only a path the call created.
//
// Not parallel: t.Setenv fails every ssh connection and hides the user's git
// config, so no case reaches the network or a security key.
func TestCloneStack(t *testing.T) {
	t.Setenv("GIT_SSH_COMMAND", "false")
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")

	testCases := []struct {
		name string
		// seed creates what path holds before the clone; nil leaves it absent.
		seed       func(path string) error
		wantExists bool
	}{
		{name: "Error/MissingPathRemoved"},
		{
			name:       "Error/EmptyDirKept",
			seed:       func(path string) error { return os.Mkdir(path, 0o700) },
			wantExists: true,
		},
		{
			name:       "Error/FileKept",
			seed:       func(path string) error { return os.WriteFile(path, []byte("notes\n"), 0o600) },
			wantExists: true,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "stack")
			if testCase.seed != nil {
				if err := testCase.seed(path); err != nil {
					t.Fatalf("seed %s: %v", path, err)
				}
			}
			if err := CloneStack(path); err == nil {
				t.Fatal("CloneStack succeeded with ssh disabled")
			}
			if _, err := os.Lstat(path); (err == nil) != testCase.wantExists {
				t.Errorf("after the failed clone, Lstat(%s) = %v, want exists %v", path, err, testCase.wantExists)
			}
		})
	}
}

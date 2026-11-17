package setup_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/a-novel-kit/stack/cli/internal/setup"
	"github.com/a-novel-kit/stack/cli/internal/shared/fsutil/fsutiltest"
)

// TestCloneStack pins that a failed clone removes only a path the call created.
//
//nolint:paralleltest // t.Setenv fails every ssh connection and hides the user's git config, so no case reaches the network or a security key.
func TestCloneStack(t *testing.T) {
	t.Setenv("GIT_SSH_COMMAND", "false")
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")

	testCases := []struct {
		name string
		// seed creates what path holds before the clone; nil leaves it absent.
		seed       func(t *testing.T, path string)
		wantExists bool
	}{
		{name: "Error/MissingPathRemoved"},
		{
			name: "Error/EmptyDirKept",
			seed: func(t *testing.T, path string) {
				t.Helper()

				if err := os.Mkdir(path, 0o700); err != nil {
					panic(err)
				}
			},
			wantExists: true,
		},
		{
			name: "Error/FileKept",
			seed: func(t *testing.T, path string) {
				t.Helper()

				fsutiltest.Write(t, filepath.Dir(path), map[string]string{filepath.Base(path): "notes\n"})
			},
			wantExists: true,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "stack")
			if testCase.seed != nil {
				testCase.seed(t, path)
			}

			require.Error(t, setup.CloneStack(path), "CloneStack succeeded with ssh disabled")

			_, err := os.Lstat(path)
			if testCase.wantExists {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, os.ErrNotExist)
			}
		})
	}
}

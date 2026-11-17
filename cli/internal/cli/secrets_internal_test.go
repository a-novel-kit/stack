package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/a-novel-kit/stack/cli/internal/shared/seamtest"
)

// TestNewSecretsSetCmd pins the TTY gate of `secrets set`: it refuses when stdin
// is not a terminal, so a value is never piped in. On the interactive path, a
// stubbed no-echo reader shows the value is stored but never printed.
//
//nolint:paralleltest // swaps the package-level stdinIsTTY and readPassword seams, and t.Setenv redirects XDG_DATA_HOME.
func TestNewSecretsSetCmd(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		dataHome := t.TempDir()
		t.Setenv("XDG_DATA_HOME", dataHome)
		const secretValue = "sk-super-secret"
		seamtest.Swap(t, &stdinIsTTY, func() bool { return true })
		seamtest.Swap(t, &readPassword, func() ([]byte, error) { return []byte(secretValue), nil })

		out, err := runCmd(t, newSecretsSetCmd(), "openai-key")
		require.NoError(t, err)
		require.Contains(t, out, "set openai-key")
		require.NotContains(t, out, secretValue)

		// The encrypted store must exist and must not contain the plaintext value.
		blob, err := os.ReadFile(filepath.Join(dataHome, "a-novel", "secrets", "store.enc"))
		require.NoError(t, err)
		require.NotContains(t, string(blob), secretValue, "plaintext secret value found in the at-rest store: not encrypted")
	})

	t.Run("Error/NonInteractive", func(t *testing.T) {
		seamtest.Swap(t, &stdinIsTTY, func() bool { return false })

		_, err := runCmd(t, newSecretsSetCmd(), "some-id")
		require.ErrorContains(t, err, "non-interactively")
	})
}

// TestNewSecretsExecCmd verifies `secrets exec` refuses with no --env. Not
// parallel: t.Setenv points XDG_DATA_HOME, the secrets store's root, at a temp
// dir.
func TestNewSecretsExecCmd(t *testing.T) {
	t.Run("Error/NoEnvFlag", func(t *testing.T) {
		t.Setenv("XDG_DATA_HOME", t.TempDir())

		_, err := runCmd(t, newSecretsExecCmd(), "true")
		require.Error(t, err)
	})
}

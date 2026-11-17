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
//nolint:paralleltest // swaps the package-level stdinIsTTY and readPassword seams.
func TestNewSecretsSetCmd(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		root := t.TempDir()
		const secretValue = "sk-super-secret"
		seamtest.Swap(t, &stdinIsTTY, func() bool { return true })
		seamtest.Swap(t, &readPassword, func() ([]byte, error) { return []byte(secretValue), nil })

		out, err := runCmd(t, newSecretsSetCmd(root), "openai-key")
		require.NoError(t, err)
		require.Contains(t, out, "set openai-key")
		require.NotContains(t, out, secretValue)

		// The encrypted store must exist and must not contain the plaintext value.
		blob, err := os.ReadFile(filepath.Join(root, "store.enc"))
		require.NoError(t, err)
		require.NotContains(t, string(blob), secretValue, "plaintext secret value found in the at-rest store: not encrypted")
	})

	t.Run("Error/NonInteractive", func(t *testing.T) {
		seamtest.Swap(t, &stdinIsTTY, func() bool { return false })

		_, err := runCmd(t, newSecretsSetCmd(t.TempDir()), "some-id")
		require.ErrorContains(t, err, "non-interactively")
	})
}

// TestNewSecretsExecCmd verifies `secrets exec` refuses with no --env.
func TestNewSecretsExecCmd(t *testing.T) {
	t.Parallel()

	t.Run("Error/NoEnvFlag", func(t *testing.T) {
		t.Parallel()

		_, err := runCmd(t, newSecretsExecCmd(t.TempDir()), "true")
		require.Error(t, err)
	})
}

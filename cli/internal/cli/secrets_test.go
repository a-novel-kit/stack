package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSecretsSetRefusesNonInteractive verifies the TTY gate: `secrets set`
// must refuse when stdin is not a terminal so a value is never piped in.
//
// Not parallel: swaps the package-level stdinIsTTY seam.
func TestSecretsSetRefusesNonInteractive(t *testing.T) {
	swap(t, &stdinIsTTY, func() bool { return false })

	if _, err := runCmd(t, newSecretsSetCmd(), "some-id"); err == nil || !strings.Contains(err.Error(), "non-interactively") {
		t.Fatalf("set = %v, want an interactive-only refusal", err)
	}
}

// TestSecretsSetStoresWithoutEchoingValue drives the interactive path with a
// stubbed no-echo reader and asserts the value is stored but never printed.
func TestSecretsSetStoresWithoutEchoingValue(t *testing.T) {
	dataHome := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dataHome)
	const secretValue = "sk-super-secret"
	swap(t, &stdinIsTTY, func() bool { return true })
	swap(t, &readPassword, func() ([]byte, error) { return []byte(secretValue), nil })

	out, err := runCmd(t, newSecretsSetCmd(), "openai-key")
	if err != nil {
		t.Fatalf("set: %v", err)
	}
	if strings.Contains(out, secretValue) || !strings.Contains(out, "set openai-key") {
		t.Fatalf("output = %q, want the `set openai-key` confirmation and never the value", out)
	}

	// The encrypted store must exist and must not contain the plaintext value.
	blob, err := os.ReadFile(filepath.Join(dataHome, "a-novel", "secrets", "store.enc"))
	if err != nil {
		t.Fatalf("read store: %v", err)
	}
	if bytes.Contains(blob, []byte(secretValue)) {
		t.Fatal("plaintext secret value found in the at-rest store — not encrypted")
	}
}

// TestSecretsExecRequiresEnvFlag verifies `secrets exec` refuses with no --env.
func TestSecretsExecRequiresEnvFlag(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	if _, err := runCmd(t, newSecretsExecCmd(), "true"); err == nil {
		t.Fatal("expected `secrets exec` with no --env to be refused")
	}
}

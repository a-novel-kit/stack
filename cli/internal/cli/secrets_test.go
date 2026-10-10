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
// Not parallel: swaps the package-level secretsStdinIsTTY seam (same pattern as
// publish_cmd_test.go's stdinIsTTY).
func TestSecretsSetRefusesNonInteractive(t *testing.T) {
	orig := secretsStdinIsTTY
	secretsStdinIsTTY = func() bool { return false }
	t.Cleanup(func() { secretsStdinIsTTY = orig })

	cmd := newSecretsSetCmd()
	cmd.SetArgs([]string{"some-id"})
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected non-interactive `secrets set` to be refused, got nil")
	}
	if !strings.Contains(err.Error(), "non-interactively") {
		t.Fatalf("expected an interactive-only refusal, got: %v", err)
	}
}

// TestSecretsSetStoresWithoutEchoingValue drives the interactive path with a
// stubbed no-echo reader and asserts the value is stored but never printed.
func TestSecretsSetStoresWithoutEchoingValue(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	origTTY := secretsStdinIsTTY
	secretsStdinIsTTY = func() bool { return true }
	t.Cleanup(func() { secretsStdinIsTTY = origTTY })

	const secretValue = "sk-super-secret"
	origRead := readPassword
	readPassword = func() ([]byte, error) { return []byte(secretValue), nil }
	t.Cleanup(func() { readPassword = origRead })

	cmd := newSecretsSetCmd()
	cmd.SetArgs([]string{"openai-key"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	if err := cmd.Execute(); err != nil {
		t.Fatalf("set: %v", err)
	}
	if strings.Contains(out.String(), secretValue) {
		t.Fatalf("command output leaked the secret value:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "set openai-key") {
		t.Fatalf("expected confirmation `set openai-key`, got:\n%s", out.String())
	}

	// The encrypted store must exist and must not contain the plaintext value.
	storePath := filepath.Join(os.Getenv("XDG_DATA_HOME"), "a-novel", "secrets", "store.enc")
	blob, err := os.ReadFile(storePath)
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

	cmd := newSecretsExecCmd()
	cmd.SetArgs([]string{"true"})
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	if err := cmd.Execute(); err == nil {
		t.Fatal("expected `secrets exec` with no --env to be refused")
	}
}

package secrets_test

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/a-novel-kit/stack/cli/internal/secrets"
	"github.com/a-novel-kit/stack/cli/internal/shared/paths"
)

// TestStore exercises the encrypted store's round-trip, listing, removal,
// nonce freshness, tamper detection, and at-rest file permissions.
//
// Not parallel: every sub-test calls t.Setenv("XDG_DATA_HOME", ...), which is
// incompatible with t.Parallel().
func TestStore(t *testing.T) {
	t.Run("RoundTrip", func(t *testing.T) {
		t.Setenv("XDG_DATA_HOME", t.TempDir())
		values := map[string]string{"openai-key": "sk-secret-value", "anthropic-key": "ak-other-value"}
		saveStore(t, values)

		// Re-open from scratch and read the values back.
		reopened := openStore(t, nil)
		for id, want := range values {
			if got, ok := reopened.Get(id); !ok || got != want {
				t.Errorf("get %s = %q, %v; want %q, true", id, got, ok, want)
			}
		}
		if _, ok := reopened.Get("missing"); ok {
			t.Error("get missing returned ok=true")
		}
	})

	t.Run("List", func(t *testing.T) {
		t.Setenv("XDG_DATA_HOME", t.TempDir())
		got := openStore(t, map[string]string{"zeta": "v", "alpha": "v", "mike": "v"}).List()
		if want := []string{"alpha", "mike", "zeta"}; !slices.Equal(got, want) { // sorted, ids only
			t.Fatalf("List = %v, want %v", got, want)
		}
	})

	t.Run("Remove", func(t *testing.T) {
		t.Setenv("XDG_DATA_HOME", t.TempDir())
		st := openStore(t, map[string]string{"keep": "v1", "drop": "v2"})
		st.Remove("drop")
		if err := st.Save(); err != nil {
			t.Fatalf("save: %v", err)
		}

		reopened := openStore(t, nil)
		if _, ok := reopened.Get("drop"); ok {
			t.Error("removed secret still present after reload")
		}
		if _, ok := reopened.Get("keep"); !ok {
			t.Error("kept secret missing after reload")
		}
	})

	t.Run("NonceMakesCiphertextDiffer", func(t *testing.T) {
		t.Setenv("XDG_DATA_HOME", t.TempDir())
		write := func() []byte {
			saveStore(t, map[string]string{"k": "same-value"})
			blob, err := os.ReadFile(filepath.Join(paths.SecretsRoot(), "store.enc"))
			if err != nil {
				t.Fatalf("read store: %v", err)
			}
			return blob
		}
		if bytes.Equal(write(), write()) {
			t.Fatal("two writes of the same value produced identical ciphertext — nonce not random")
		}
	})

	t.Run("TamperDetected", func(t *testing.T) {
		t.Setenv("XDG_DATA_HOME", t.TempDir())
		saveStore(t, map[string]string{"k": "v"})

		storePath := filepath.Join(paths.SecretsRoot(), "store.enc")
		blob, err := os.ReadFile(storePath)
		if err != nil {
			t.Fatalf("read store: %v", err)
		}
		// Flip the last byte (inside the GCM tag / ciphertext) — gcm.Open
		// must reject it.
		blob[len(blob)-1] ^= 0xFF
		if err := os.WriteFile(storePath, blob, 0o600); err != nil {
			t.Fatalf("write tampered store: %v", err)
		}
		if _, err := secrets.Open(); err == nil {
			t.Fatal("expected decrypt to fail on a tampered store, got nil")
		}
	})

	t.Run("FilePermissions", func(t *testing.T) {
		t.Setenv("XDG_DATA_HOME", t.TempDir())
		saveStore(t, map[string]string{"k": "v"})

		root := paths.SecretsRoot()
		for path, want := range map[string]os.FileMode{
			filepath.Join(root, "key"):       0o600,
			filepath.Join(root, "store.enc"): 0o600,
			root:                             0o700,
		} {
			info, err := os.Stat(path)
			if err != nil {
				t.Fatalf("stat %s: %v", path, err)
			}
			if perm := info.Mode().Perm(); perm != want {
				t.Errorf("%s mode = %o, want %o", path, perm, want)
			}
		}
	})

	t.Run("InitIdempotentNeverOverwritesKey", func(t *testing.T) {
		t.Setenv("XDG_DATA_HOME", t.TempDir())
		keyPath := filepath.Join(paths.SecretsRoot(), "key")
		keys := make([][]byte, 2)
		for i, wantCreated := range []bool{true, false} {
			created, err := secrets.Init()
			if err != nil || created != wantCreated {
				t.Fatalf("init #%d = created %v, err %v; want created %v (only the first creates a key)",
					i+1, created, err, wantCreated)
			}
			key, err := os.ReadFile(keyPath)
			if err != nil {
				t.Fatalf("read key: %v", err)
			}
			keys[i] = key
		}
		if !bytes.Equal(keys[0], keys[1]) {
			t.Fatal("init overwrote an existing key — must be idempotent")
		}
	})
}

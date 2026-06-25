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

		st, err := secrets.Open()
		if err != nil {
			t.Fatalf("open: %v", err)
		}
		st.Set("openai-key", "sk-secret-value")
		st.Set("anthropic-key", "ak-other-value")
		if err := st.Save(); err != nil {
			t.Fatalf("save: %v", err)
		}

		// Re-open from scratch and read the values back.
		reopened, err := secrets.Open()
		if err != nil {
			t.Fatalf("reopen: %v", err)
		}
		got, ok := reopened.Get("openai-key")
		if !ok || got != "sk-secret-value" {
			t.Fatalf("get openai-key = %q, %v; want %q, true", got, ok, "sk-secret-value")
		}
		got, ok = reopened.Get("anthropic-key")
		if !ok || got != "ak-other-value" {
			t.Fatalf("get anthropic-key = %q, %v; want %q, true", got, ok, "ak-other-value")
		}
		if _, ok := reopened.Get("missing"); ok {
			t.Fatal("get missing returned ok=true")
		}
	})

	t.Run("List", func(t *testing.T) {
		t.Setenv("XDG_DATA_HOME", t.TempDir())

		st, err := secrets.Open()
		if err != nil {
			t.Fatalf("open: %v", err)
		}
		st.Set("zeta", "v")
		st.Set("alpha", "v")
		st.Set("mike", "v")

		got := st.List()
		want := []string{"alpha", "mike", "zeta"} // sorted, ids only
		if !slices.Equal(got, want) {
			t.Fatalf("List = %v, want %v", got, want)
		}
	})

	t.Run("Remove", func(t *testing.T) {
		t.Setenv("XDG_DATA_HOME", t.TempDir())

		st, err := secrets.Open()
		if err != nil {
			t.Fatalf("open: %v", err)
		}
		st.Set("keep", "v1")
		st.Set("drop", "v2")
		st.Remove("drop")
		if err := st.Save(); err != nil {
			t.Fatalf("save: %v", err)
		}

		reopened, err := secrets.Open()
		if err != nil {
			t.Fatalf("reopen: %v", err)
		}
		if _, ok := reopened.Get("drop"); ok {
			t.Fatal("removed secret still present after reload")
		}
		if _, ok := reopened.Get("keep"); !ok {
			t.Fatal("kept secret missing after reload")
		}
	})

	t.Run("NonceMakesCiphertextDiffer", func(t *testing.T) {
		t.Setenv("XDG_DATA_HOME", t.TempDir())

		write := func() []byte {
			st, err := secrets.Open()
			if err != nil {
				t.Fatalf("open: %v", err)
			}
			st.Set("k", "same-value")
			if err := st.Save(); err != nil {
				t.Fatalf("save: %v", err)
			}
			blob, err := os.ReadFile(filepath.Join(paths.SecretsRoot(), "store.enc"))
			if err != nil {
				t.Fatalf("read store: %v", err)
			}
			return blob
		}
		first := write()
		second := write()
		if bytes.Equal(first, second) {
			t.Fatal("two writes of the same value produced identical ciphertext — nonce not random")
		}
	})

	t.Run("TamperDetected", func(t *testing.T) {
		t.Setenv("XDG_DATA_HOME", t.TempDir())

		st, err := secrets.Open()
		if err != nil {
			t.Fatalf("open: %v", err)
		}
		st.Set("k", "v")
		if err := st.Save(); err != nil {
			t.Fatalf("save: %v", err)
		}

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

		st, err := secrets.Open()
		if err != nil {
			t.Fatalf("open: %v", err)
		}
		st.Set("k", "v")
		if err := st.Save(); err != nil {
			t.Fatalf("save: %v", err)
		}

		keyInfo, err := os.Stat(filepath.Join(paths.SecretsRoot(), "key"))
		if err != nil {
			t.Fatalf("stat key: %v", err)
		}
		if perm := keyInfo.Mode().Perm(); perm != 0o600 {
			t.Errorf("key file mode = %o, want 600", perm)
		}
		storeInfo, err := os.Stat(filepath.Join(paths.SecretsRoot(), "store.enc"))
		if err != nil {
			t.Fatalf("stat store: %v", err)
		}
		if perm := storeInfo.Mode().Perm(); perm != 0o600 {
			t.Errorf("store file mode = %o, want 600", perm)
		}
		dirInfo, err := os.Stat(paths.SecretsRoot())
		if err != nil {
			t.Fatalf("stat dir: %v", err)
		}
		if perm := dirInfo.Mode().Perm(); perm != 0o700 {
			t.Errorf("secrets dir mode = %o, want 700", perm)
		}
	})

	t.Run("InitIdempotentNeverOverwritesKey", func(t *testing.T) {
		t.Setenv("XDG_DATA_HOME", t.TempDir())

		created, err := secrets.Init()
		if err != nil {
			t.Fatalf("first init: %v", err)
		}
		if !created {
			t.Fatal("first init should report a newly created key")
		}
		keyPath := filepath.Join(paths.SecretsRoot(), "key")
		original, err := os.ReadFile(keyPath)
		if err != nil {
			t.Fatalf("read key: %v", err)
		}

		created, err = secrets.Init()
		if err != nil {
			t.Fatalf("second init: %v", err)
		}
		if created {
			t.Fatal("second init should be a no-op (key already exists)")
		}
		again, err := os.ReadFile(keyPath)
		if err != nil {
			t.Fatalf("re-read key: %v", err)
		}
		if !bytes.Equal(original, again) {
			t.Fatal("init overwrote an existing key — must be idempotent")
		}
	})
}

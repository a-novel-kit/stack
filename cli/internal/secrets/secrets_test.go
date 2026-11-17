package secrets_test

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/a-novel-kit/stack/cli/internal/secrets"
	"github.com/a-novel-kit/stack/cli/internal/shared/paths"
)

// TestStore exercises the encrypted store's round-trip, listing, removal,
// nonce freshness, tamper detection, and at-rest file permissions.
//
// Not parallel: every sub-test sets XDG_DATA_HOME to isolate the store.
func TestStore(t *testing.T) {
	t.Run("Success/RoundTrip", func(t *testing.T) {
		t.Setenv("XDG_DATA_HOME", t.TempDir())
		values := map[string]string{"openai-key": "sk-secret-value", "anthropic-key": "ak-other-value"}
		saveStore(t, values)

		// Re-open from scratch and read the values back.
		reopened := openStore(t, nil)
		for id, want := range values {
			got, ok := reopened.Get(id)
			require.True(t, ok, "get %s", id)
			require.Equal(t, want, got)
		}
		_, ok := reopened.Get("missing")
		require.False(t, ok, "get missing")
	})

	t.Run("Success/List", func(t *testing.T) {
		t.Setenv("XDG_DATA_HOME", t.TempDir())
		got := openStore(t, map[string]string{"zeta": "v", "alpha": "v", "mike": "v"}).List()
		require.Equal(t, []string{"alpha", "mike", "zeta"}, got) // sorted, ids only
	})

	t.Run("Success/Remove", func(t *testing.T) {
		t.Setenv("XDG_DATA_HOME", t.TempDir())
		st := openStore(t, map[string]string{"keep": "v1", "drop": "v2"})
		st.Remove("drop")
		require.NoError(t, st.Save())

		reopened := openStore(t, nil)
		_, ok := reopened.Get("drop")
		require.False(t, ok, "removed secret still present after reload")
		_, ok = reopened.Get("keep")
		require.True(t, ok, "kept secret missing after reload")
	})

	t.Run("Success/NonceMakesCiphertextDiffer", func(t *testing.T) {
		t.Setenv("XDG_DATA_HOME", t.TempDir())
		write := func() []byte {
			saveStore(t, map[string]string{"k": "same-value"})
			blob, err := os.ReadFile(filepath.Join(paths.SecretsRoot(), "store.enc"))
			require.NoError(t, err)
			return blob
		}
		first, second := write(), write()
		require.NotEqual(t, first, second, "two writes of the same value produced identical ciphertext: nonce not random")
	})

	t.Run("Success/ReadsNonceThenCiphertext", func(t *testing.T) {
		// A store sealed by hand as `nonce || ciphertext`, the layout every
		// release writes, opens.
		t.Setenv("XDG_DATA_HOME", t.TempDir())
		_, err := secrets.Init()
		require.NoError(t, err)
		root := paths.SecretsRoot()
		key, err := os.ReadFile(filepath.Join(root, "key"))
		require.NoError(t, err)
		block, err := aes.NewCipher(key)
		require.NoError(t, err)
		gcm, err := cipher.NewGCM(block)
		require.NoError(t, err)
		plain, err := json.Marshal(map[string]string{"k": "v"})
		require.NoError(t, err)
		nonce := make([]byte, gcm.NonceSize())
		_, _ = rand.Read(nonce) // never fails: it crashes the program instead
		require.NoError(t, os.WriteFile(filepath.Join(root, "store.enc"), gcm.Seal(nonce, nonce, plain, nil), 0o600))
		got, ok := openStore(t, nil).Get("k")
		require.True(t, ok, "get k")
		require.Equal(t, "v", got)
	})

	t.Run("Error/TamperDetected", func(t *testing.T) {
		t.Setenv("XDG_DATA_HOME", t.TempDir())
		saveStore(t, map[string]string{"k": "v"})

		storePath := filepath.Join(paths.SecretsRoot(), "store.enc")
		blob, err := os.ReadFile(storePath)
		require.NoError(t, err)
		// Flip the last byte (inside the GCM tag / ciphertext): gcm.Open
		// must reject it.
		blob[len(blob)-1] ^= 0xFF
		require.NoError(t, os.WriteFile(storePath, blob, 0o600))
		_, err = secrets.Open()
		require.Error(t, err, "decrypt of a tampered store")
	})

	t.Run("Success/FilePermissions", func(t *testing.T) {
		t.Setenv("XDG_DATA_HOME", t.TempDir())
		saveStore(t, map[string]string{"k": "v"})

		root := paths.SecretsRoot()
		wantModes := map[string]os.FileMode{
			filepath.Join(root, "key"):       0o600,
			filepath.Join(root, "store.enc"): 0o600,
			root:                             0o700,
		}

		for path, want := range wantModes {
			info, err := os.Stat(path)
			require.NoError(t, err)
			require.Equal(t, want, info.Mode().Perm(), path)
		}
	})
}

// TestInit pins that the local key is created once: a later Init, or a
// concurrent one, never overwrites it.
//
// Not parallel: every sub-test sets XDG_DATA_HOME to isolate the store.
func TestInit(t *testing.T) {
	t.Run("Success/IdempotentNeverOverwritesKey", func(t *testing.T) {
		t.Setenv("XDG_DATA_HOME", t.TempDir())
		keyPath := filepath.Join(paths.SecretsRoot(), "key")
		keys := make([][]byte, 2)
		for i, wantCreated := range []bool{true, false} {
			created, err := secrets.Init()
			require.NoError(t, err)
			require.Equal(t, wantCreated, created, "init #%d: only the first creates a key", i+1)
			key, err := os.ReadFile(keyPath)
			require.NoError(t, err)
			keys[i] = key
		}
		require.Equal(t, keys[0], keys[1], "init overwrote an existing key: it must be idempotent")
	})

	t.Run("Success/ConcurrentCreatesOneKey", func(t *testing.T) {
		t.Setenv("XDG_DATA_HOME", t.TempDir())
		const runs = 16
		start := make(chan struct{})
		var created atomic.Int32
		errs := make([]error, runs)
		var wg sync.WaitGroup
		for i := range runs {
			wg.Go(func() {
				<-start
				ok, err := secrets.Init()
				errs[i] = err
				if ok {
					created.Add(1)
				}
			})
		}
		close(start)
		wg.Wait()
		require.NoError(t, errors.Join(errs...))
		require.Equal(t, int32(1), created.Load(), "concurrent inits that created a key, of %d", runs)
	})
}

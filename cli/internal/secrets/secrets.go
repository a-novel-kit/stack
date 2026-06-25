// Package secrets is a local, encrypted secrets manager for the a-novel CLI.
//
// Threat model. A developer (or an AI agent) runs `a-novel test`/`run`/`ui`
// with API secrets (e.g. OPENAI_API_KEY) injected into the CHILD process env
// only. The secret is never printed, logged, committed, or passed as a CLI
// argument. The goal is to prevent accidental exposure, not to defend against a
// local attacker who already has read access to the user's home directory.
//
// Design.
//
//   - A 32-byte local key (crypto/rand) lives at SecretsRoot()/key, file mode
//     0600 inside a 0700 directory. It is generated once and never overwritten.
//   - The store at SecretsRoot()/store.enc holds a flat JSON map
//     {secretID: value} encrypted as a whole with AES-256-GCM. Each write uses
//     a fresh random 12-byte nonce, stored as `nonce || ciphertext`. Writes are
//     atomic (temp file + rename) at mode 0600.
//   - No value is ever logged or printed. Errors never embed a secret value,
//     and the Store is never formatted with %v.
//
// Only the Go standard library is used for crypto (crypto/aes, crypto/cipher,
// crypto/rand) — no third-party crypto.
package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/a-novel-kit/stack/cli/internal/shared/paths"
)

const (
	// keyLen is the AES-256 key length in bytes.
	keyLen = 32
	// dirMode / keyMode are the strict permissions for the secrets dir and the
	// at-rest files (key + store). 0700 dir, 0600 files: owner-only.
	dirMode  os.FileMode = 0o700
	keyMode  os.FileMode = 0o600
	fileMode os.FileMode = 0o600
)

// keyFile / storeFile are the fixed file names under SecretsRoot().
const (
	keyFile   = "key"
	storeFile = "store.enc"
)

// Store is an open, decrypted view of the secrets store. It holds the plaintext
// map in memory; callers mutate it via Set/Remove and persist with Save. The
// map is unexported so no caller can accidentally range-print the values.
type Store struct {
	root   string
	key    []byte
	values map[string]string
}

// Open loads (and, if missing, initializes) the local key and the encrypted
// store under paths.SecretsRoot(). A first call materializes the key but NOT
// the store file — an empty store has no file on disk until the first Save.
func Open() (*Store, error) {
	return openAt(paths.SecretsRoot())
}

// Init creates the secrets directory and the local key if they don't already
// exist. It is idempotent: an existing key is never overwritten. The bool is
// true when a new key was generated, false when one already existed.
func Init() (bool, error) {
	return initAt(paths.SecretsRoot())
}

// initAt is the testable core of Init, parameterized on the root directory.
func initAt(root string) (bool, error) {
	if err := os.MkdirAll(root, dirMode); err != nil {
		return false, fmt.Errorf("secrets: create %s: %w", root, err)
	}
	keyPath := filepath.Join(root, keyFile)
	if _, statErr := os.Stat(keyPath); statErr == nil {
		return false, nil // key already exists — never overwrite
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return false, fmt.Errorf("secrets: stat key: %w", statErr)
	}
	key := make([]byte, keyLen)
	if _, err := rand.Read(key); err != nil {
		return false, fmt.Errorf("secrets: generate key: %w", err)
	}
	if err := os.WriteFile(keyPath, key, keyMode); err != nil {
		return false, fmt.Errorf("secrets: write key: %w", err)
	}
	return true, nil
}

// openAt is the testable core of Open, parameterized on the root directory. It
// ensures the key exists (creating it on first use) and loads + decrypts the
// store if present.
func openAt(root string) (*Store, error) {
	if _, err := initAt(root); err != nil {
		return nil, err
	}
	key, err := os.ReadFile(filepath.Join(root, keyFile))
	if err != nil {
		return nil, fmt.Errorf("secrets: read key: %w", err)
	}
	if len(key) != keyLen {
		// Never echo the key material; just its length.
		return nil, fmt.Errorf("secrets: local key is %d bytes, expected %d — store is corrupt", len(key), keyLen)
	}
	st := &Store{root: root, key: key, values: map[string]string{}}
	if err := st.load(); err != nil {
		return nil, err
	}
	return st, nil
}

// load reads and decrypts the store file into st.values. An absent store file
// is not an error — it means an empty store (nothing set yet).
func (st *Store) load() error {
	raw, err := os.ReadFile(filepath.Join(st.root, storeFile))
	if errors.Is(err, os.ErrNotExist) {
		st.values = map[string]string{}
		return nil
	}
	if err != nil {
		return fmt.Errorf("secrets: read store: %w", err)
	}
	plain, err := decrypt(st.key, raw)
	if err != nil {
		return err
	}
	values := map[string]string{}
	if err := json.Unmarshal(plain, &values); err != nil {
		// Don't include the plaintext in the error.
		return errors.New("secrets: store is corrupt (decrypt succeeded but JSON is invalid)")
	}
	st.values = values
	return nil
}

// Set stores value under id in memory. Call Save to persist. The value is never
// logged.
func (st *Store) Set(id, value string) {
	st.values[id] = value
}

// Get returns the value for id and whether it exists.
func (st *Store) Get(id string) (string, bool) {
	v, ok := st.values[id]
	return v, ok
}

// Remove deletes id from the store (in memory). Call Save to persist.
func (st *Store) Remove(id string) {
	delete(st.values, id)
}

// List returns the sorted secret IDs only — never the values.
func (st *Store) List() []string {
	ids := make([]string, 0, len(st.values))
	for id := range st.values {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// Save encrypts the in-memory map and writes it atomically (temp file + rename)
// at mode 0600. A fresh random nonce is used on every write, so two saves of the
// same content produce different ciphertext.
func (st *Store) Save() error {
	plain, err := json.Marshal(st.values)
	if err != nil {
		return fmt.Errorf("secrets: encode store: %w", err)
	}
	blob, err := encrypt(st.key, plain)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(st.root, dirMode); err != nil {
		return fmt.Errorf("secrets: create %s: %w", st.root, err)
	}
	return writeFileAtomic(filepath.Join(st.root, storeFile), blob, fileMode)
}

// encrypt seals plain with AES-256-GCM under key, returning `nonce || ciphertext`.
func encrypt(key, plain []byte) ([]byte, error) {
	gcm, err := newGCM(key)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("secrets: generate nonce: %w", err)
	}
	// Seal appends the ciphertext to nonce, yielding nonce||ciphertext.
	return gcm.Seal(nonce, nonce, plain, nil), nil
}

// decrypt splits `nonce || ciphertext` and opens it with AES-256-GCM. A
// tampered blob (wrong tag) fails here.
func decrypt(key, blob []byte) ([]byte, error) {
	gcm, err := newGCM(key)
	if err != nil {
		return nil, err
	}
	ns := gcm.NonceSize()
	if len(blob) < ns {
		return nil, errors.New("secrets: store is corrupt (truncated)")
	}
	nonce, ciphertext := blob[:ns], blob[ns:]
	plain, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		// gcm.Open's error doesn't leak plaintext; wrap it generically.
		return nil, errors.New("secrets: decrypt failed (wrong key or tampered store)")
	}
	return plain, nil
}

// newGCM builds an AES-256-GCM AEAD from a 32-byte key.
func newGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("secrets: init cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("secrets: init GCM: %w", err)
	}
	return gcm, nil
}

// writeFileAtomic writes data to a temp file in the same directory and renames
// it over path, so a crash mid-write never leaves a half-written store. The temp
// file is created at the target mode and removed if the rename fails.
func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".store-*.tmp")
	if err != nil {
		return fmt.Errorf("secrets: create temp store: %w", err)
	}
	tmpName := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpName) }
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("secrets: chmod temp store: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("secrets: write temp store: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("secrets: close temp store: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		cleanup()
		return fmt.Errorf("secrets: finalize store: %w", err)
	}
	return nil
}

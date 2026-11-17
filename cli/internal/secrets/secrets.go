package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"

	"github.com/a-novel-kit/stack/cli/internal/shared/fsutil"
)

const (
	// keyLen is the AES-256 key length in bytes.
	keyLen = 32
	// dirMode keeps the secrets directory owner-only.
	dirMode os.FileMode = 0o700
	// fileMode keeps the at-rest files, the key and the store, owner-only.
	fileMode os.FileMode = 0o600
)

// keyFile / storeFile are the fixed file names under the secrets root.
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

// Init creates the secrets directory root and the local key in it if they
// don't already exist. It is idempotent: an existing key is never overwritten.
// The bool is true when a new key was generated, false when one already
// existed.
func Init(root string) (bool, error) {
	if err := os.MkdirAll(root, dirMode); err != nil {
		return false, fmt.Errorf("secrets: create %s: %w", root, err)
	}
	key := make([]byte, keyLen)
	_, _ = rand.Read(key) // never fails: it crashes the program instead
	// O_EXCL makes creating the key and checking for it one step, so of two
	// concurrent first runs exactly one writes a key.
	f, err := os.OpenFile(filepath.Join(root, keyFile), os.O_WRONLY|os.O_CREATE|os.O_EXCL, fileMode)
	if errors.Is(err, fs.ErrExist) {
		return false, nil // key already exists: never overwrite
	}
	if err != nil {
		return false, fmt.Errorf("secrets: create key: %w", err)
	}
	_, err = f.Write(key)
	if err = errors.Join(err, f.Close()); err != nil {
		return false, fmt.Errorf("secrets: write key: %w", err)
	}
	return true, nil
}

// Open loads (and, if missing, initializes) the local key and the encrypted
// store under root. A first call creates the key but not the store file: an
// empty store has no file on disk until the first Save.
func Open(root string) (*Store, error) {
	if _, err := Init(root); err != nil {
		return nil, err
	}
	key, err := os.ReadFile(filepath.Join(root, keyFile))
	if err != nil {
		return nil, fmt.Errorf("secrets: read key: %w", err)
	}
	if len(key) != keyLen {
		// Never echo the key material; just its length.
		return nil, fmt.Errorf("secrets: local key is %d bytes, expected %d: store is corrupt", len(key), keyLen)
	}
	st := &Store{root: root, key: key, values: map[string]string{}}
	if err := st.load(); err != nil {
		return nil, err
	}
	return st, nil
}

// load reads and decrypts the store file into st.values. An absent store file
// is an empty store.
func (st *Store) load() error {
	raw, err := os.ReadFile(filepath.Join(st.root, storeFile))
	if errors.Is(err, os.ErrNotExist) {
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

// List returns the sorted secret IDs, never the values.
func (st *Store) List() []string {
	return slices.Sorted(maps.Keys(st.values))
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
	// An atomic write never leaves a half-written store behind a crash.
	if err := fsutil.WriteAtomic(filepath.Join(st.root, storeFile), blob, fileMode); err != nil {
		return fmt.Errorf("secrets: write store: %w", err)
	}
	return nil
}

// encrypt seals plain with AES-256-GCM under key, returning `nonce || ciphertext`.
func encrypt(key, plain []byte) ([]byte, error) {
	gcm, err := newGCM(key)
	if err != nil {
		return nil, err
	}
	return gcm.Seal(nil, nil, plain, nil), nil
}

// decrypt opens `nonce || ciphertext` with AES-256-GCM. A tampered blob (wrong
// tag) fails here.
func decrypt(key, blob []byte) ([]byte, error) {
	gcm, err := newGCM(key)
	if err != nil {
		return nil, err
	}
	if len(blob) < gcm.Overhead() {
		return nil, errors.New("secrets: store is corrupt (truncated)")
	}
	plain, err := gcm.Open(nil, nil, blob, nil)
	if err != nil {
		// gcm.Open's error doesn't leak plaintext; wrap it generically.
		return nil, errors.New("secrets: decrypt failed (wrong key or tampered store)")
	}
	return plain, nil
}

// newGCM builds an AES-256-GCM AEAD from a 32-byte key. It draws a random
// 96-bit nonce for each Seal and prepends it to the ciphertext, the
// `nonce || ciphertext` layout of the store file.
func newGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("secrets: init cipher: %w", err)
	}
	gcm, err := cipher.NewGCMWithRandomNonce(block)
	if err != nil {
		return nil, fmt.Errorf("secrets: init GCM: %w", err)
	}
	return gcm, nil
}

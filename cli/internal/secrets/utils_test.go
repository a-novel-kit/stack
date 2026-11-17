package secrets_test

import (
	"testing"

	"github.com/a-novel-kit/stack/cli/internal/secrets"
)

// openStore opens the store under root and sets values in it, unsaved.
func openStore(t *testing.T, root string, values map[string]string) *secrets.Store {
	t.Helper()
	st, err := secrets.Open(root)
	if err != nil {
		panic(err)
	}
	for id, value := range values {
		st.Set(id, value)
	}
	return st
}

// saveStore opens the store under root, sets values and saves it.
func saveStore(t *testing.T, root string, values map[string]string) {
	t.Helper()
	if err := openStore(t, root, values).Save(); err != nil {
		panic(err)
	}
}

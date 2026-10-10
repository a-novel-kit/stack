package secrets_test

import (
	"testing"

	"github.com/a-novel-kit/stack/cli/internal/secrets"
)

// openStore opens the store under the current XDG_DATA_HOME and sets values in
// it, unsaved.
func openStore(t *testing.T, values map[string]string) *secrets.Store {
	t.Helper()
	st, err := secrets.Open()
	if err != nil {
		panic(err)
	}
	for id, value := range values {
		st.Set(id, value)
	}
	return st
}

// saveStore opens the store, sets values and saves it.
func saveStore(t *testing.T, values map[string]string) {
	t.Helper()
	if err := openStore(t, values).Save(); err != nil {
		panic(err)
	}
}

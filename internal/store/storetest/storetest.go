// Package storetest provides stores for exercising the persisted-state path.
package storetest

import (
	"path/filepath"
	"testing"

	"github.com/moabukar/miniblue/internal/store"
)

// NewFile returns a file-backed store and a restart function. Restart saves the
// current store and returns a fresh one loaded from the same file, as after a
// process restart: every value comes back as decoded JSON (maps and float64s)
// rather than the struct that handlers stored.
func NewFile(t testing.TB) (*store.Store, func() *store.Store) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "state.json")
	open := func() *store.Store { return store.NewWithBackend(store.NewFileBackend(path)) }
	cur := open()
	restart := func() *store.Store {
		t.Helper()
		if err := cur.Save(); err != nil {
			t.Fatalf("save state: %v", err)
		}
		cur = open()
		return cur
	}
	return cur, restart
}

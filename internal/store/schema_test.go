package store

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
)

// unreadableBackend behaves like Postgres when a read fails: Get and List
// report a miss and readSchema reports the error. Writes still succeed, which
// is what let a failed read overwrite the stamp.
type unreadableBackend struct {
	*MemoryBackend
	err error
}

func (unreadableBackend) Get(string) (interface{}, bool) { return nil, false }
func (unreadableBackend) List() []string                 { return nil }
func (u unreadableBackend) readSchema() (interface{}, bool, bool, error) {
	return nil, false, false, u.err
}

func TestCheckSchema(t *testing.T) {
	tests := []struct {
		name    string
		seed    map[string]interface{}
		wantErr string // substring; empty means startup continues
	}{
		{name: "empty store is stamped"},
		{name: "matching stamp", seed: map[string]interface{}{schemaKey: SchemaVersion}},
		{name: "matching stamp after a JSON round trip", seed: map[string]interface{}{schemaKey: float64(SchemaVersion)}},
		{name: "unstamped state is version 1", seed: map[string]interface{}{"subscriptions:s1": "v"}},
		{name: "older stamp", seed: map[string]interface{}{schemaKey: SchemaVersion - 1, "k": "v"}, wantErr: fmt.Sprintf("is schema %d", SchemaVersion-1)},
		{name: "newer stamp", seed: map[string]interface{}{schemaKey: SchemaVersion + 1, "k": "v"}, wantErr: fmt.Sprintf("is schema %d", SchemaVersion+1)},
		{name: "fractional stamp", seed: map[string]interface{}{schemaKey: 1.5}, wantErr: "unreadable schema stamp"},
		{name: "non-numeric stamp", seed: map[string]interface{}{schemaKey: "banana"}, wantErr: "unreadable schema stamp"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mb := NewMemoryBackend()
			for k, v := range tc.seed {
				mb.Set(k, v)
			}
			want := mb.Snapshot()

			err := CheckSchema(NewWithBackend(mb), "test-location")

			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("CheckSchema: %v", err)
				}
				want[schemaKey] = SchemaVersion
			} else {
				if err == nil {
					t.Fatal("CheckSchema succeeded, want an error")
				}
				for _, s := range []string{tc.wantErr, "test-location", fmt.Sprintf("needs %d", SchemaVersion), "TRUNCATE"} {
					if !strings.Contains(err.Error(), s) {
						t.Errorf("error %q does not contain %q", err, s)
					}
				}
			}
			if got := mb.Snapshot(); !reflect.DeepEqual(got, want) {
				t.Errorf("store = %v, want %v", got, want)
			}
		})
	}
}

func TestCheckSchemaSurvivesFileReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	fb := NewFileBackend(path)
	if err := CheckSchema(NewWithBackend(fb), path); err != nil {
		t.Fatal(err)
	}
	fb.Set("k", "v")
	if err := fb.Save(); err != nil {
		t.Fatal(err)
	}

	reloaded := NewWithBackend(NewFileBackend(path))
	if err := CheckSchema(reloaded, path); err != nil {
		t.Fatalf("CheckSchema after reload: %v", err)
	}
	if _, ok := reloaded.Get("k"); !ok {
		t.Error("state lost across reload")
	}
}

func TestResetKeepsSchemaStamp(t *testing.T) {
	s := New()
	if err := CheckSchema(s, "test-location"); err != nil {
		t.Fatal(err)
	}
	s.Set("k", "v")
	s.Reset()

	if s.Exists("k") {
		t.Error("Reset did not clear state")
	}
	if got := s.List(); len(got) != 1 || got[0] != schemaKey {
		t.Errorf("keys after Reset = %v, want only the stamp", got)
	}

	unstamped := New()
	unstamped.Set("k", "v")
	unstamped.Reset()
	if got := unstamped.List(); len(got) != 0 {
		t.Errorf("keys after Reset of an unstamped store = %v, want none", got)
	}
}

func TestConcurrentResetKeepsSchemaStamp(t *testing.T) {
	mb := NewMemoryBackend()
	fb := NewFileBackend(filepath.Join(t.TempDir(), "state.json"))
	backends := map[string]struct {
		backend  Backend
		snapshot func() map[string]interface{}
	}{
		"memory": {mb, mb.Snapshot},
		"file":   {fb, fb.mem.Snapshot},
	}

	for name, tc := range backends {
		t.Run(name, func(t *testing.T) {
			s := NewWithBackend(tc.backend)
			if err := CheckSchema(s, "test-location"); err != nil {
				t.Fatal(err)
			}

			done := make(chan struct{})
			var observer sync.WaitGroup
			observer.Add(1)
			var missing bool
			go func() {
				defer observer.Done()
				for {
					select {
					case <-done:
						return
					default:
					}
					if _, ok := tc.backend.Get(schemaKey); !ok {
						missing = true
					}
					if _, ok := tc.snapshot()[schemaKey]; !ok {
						missing = true
					}
				}
			}()

			var resets sync.WaitGroup
			for i := 0; i < 8; i++ {
				resets.Add(1)
				go func() {
					defer resets.Done()
					for j := 0; j < 200; j++ {
						s.Set("k", "v")
						s.Reset()
					}
				}()
			}
			resets.Wait()
			close(done)
			observer.Wait()

			if missing {
				t.Error("schema stamp was absent during a concurrent Reset")
			}
			if _, ok := tc.backend.Get(schemaKey); !ok {
				t.Error("schema stamp missing after concurrent Resets")
			}
		})
	}
}

func TestCheckSchemaAbortsOnFailedRead(t *testing.T) {
	readErr := errors.New("connection refused")
	mb := NewMemoryBackend()
	mb.Set(schemaKey, SchemaVersion+1)
	mb.Set("k", "v")
	want := mb.Snapshot()

	err := CheckSchema(NewWithBackend(unreadableBackend{mb, readErr}), "test-location")

	if !errors.Is(err, readErr) {
		t.Fatalf("CheckSchema error = %v, want one wrapping %v", err, readErr)
	}
	if got := mb.Snapshot(); !reflect.DeepEqual(got, want) {
		t.Errorf("store = %v, want it untouched: %v", got, want)
	}
}

func TestCheckSchemaPostgresReadFailure(t *testing.T) {
	db, err := sql.Open("postgres", "postgres://localhost/unused?sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	db.Close()

	err = CheckSchema(NewWithBackend(&PostgresBackend{db: db}), "test-location")
	if err == nil || !strings.Contains(err.Error(), "read schema stamp") {
		t.Errorf("CheckSchema error = %v, want a failed stamp read", err)
	}
}

func TestListKeysByPrefixSkipsSchemaStamp(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	s := NewWithBackend(NewFileBackend(path))
	if err := CheckSchema(s, path); err != nil {
		t.Fatal(err)
	}
	s.Set("sb:msg:ns:q:0000000002", "b")
	s.Set("sb:msg:ns:q:0000000001", "a")
	s.Set("sb:msg:ns:other:0000000001", "x")

	got := s.ListKeysByPrefix("sb:msg:ns:q:")
	if len(got) != 2 {
		t.Fatalf("ListKeysByPrefix = %v, want the 2 keys under the prefix", got)
	}
	for _, k := range got {
		if !strings.HasPrefix(k, "sb:msg:ns:q:") {
			t.Errorf("key %q does not match the prefix", k)
		}
	}
}

func TestOpenChecksSchema(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("PERSISTENCE", "1")
	t.Setenv("DATABASE_URL", "")

	dir := filepath.Join(home, ".miniblue")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "state.json")

	if err := os.WriteFile(path, []byte(fmt.Sprintf(`{"meta:schema": %d, "k": "v"}`, SchemaVersion+1)), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(); err == nil || !strings.Contains(err.Error(), path) {
		t.Errorf("Open with a mismatched stamp: err = %v, want one naming %s", err, path)
	}

	if err := os.WriteFile(path, []byte(`{"k": "v"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := Open()
	if err != nil {
		t.Fatalf("Open with unstamped state: %v", err)
	}
	if _, ok := s.Get("k"); !ok {
		t.Error("existing state not loaded")
	}
}

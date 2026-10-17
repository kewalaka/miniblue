package store

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

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

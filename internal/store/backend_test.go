package store

import (
	"errors"
	"path/filepath"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
)

func TestStoreByteKVContract(t *testing.T) {
	backends := map[string]func(t *testing.T) Backend{
		"memory": func(t *testing.T) Backend { return NewMemoryBackend() },
		"file": func(t *testing.T) Backend {
			fb, err := NewFileBackend(filepath.Join(t.TempDir(), "state.json"))
			if err != nil {
				t.Fatal(err)
			}
			return fb
		},
	}

	for name, mk := range backends {
		t.Run(name, func(t *testing.T) {
			s := NewWithBackend(mk(t))

			// Set returns an error for unmarshalable values.
			if err := s.Set("bad", make(chan int)); err == nil {
				t.Fatal("Set(chan) succeeded, want marshal error")
			}

			// Lookup returns raw JSON bytes; Get returns fresh decoded copies.
			if err := s.Set("obj", map[string]any{"k": "v"}); err != nil {
				t.Fatalf("Set: %v", err)
			}
			raw, ok, err := s.Lookup("obj")
			if err != nil || !ok || string(raw) != `{"k":"v"}` {
				t.Fatalf("Lookup = %q, %v, %v", raw, ok, err)
			}
			// Mutating the returned byte slice must not mutate stored state.
			raw[0] = 'X'
			raw2, ok, err := s.Lookup("obj")
			if err != nil || !ok || string(raw2) != `{"k":"v"}` {
				t.Fatalf("stored bytes mutated through Lookup slice: %q", raw2)
			}

			// Mutating a map returned by Get must not affect subsequent reads.
			v, ok := s.Get("obj")
			if !ok {
				t.Fatal("Get missed")
			}
			v.(map[string]any)["k"] = "mutated"
			v2, _ := s.Get("obj")
			if v2.(map[string]any)["k"] != "v" {
				t.Fatalf("Get returned shared map: got %v", v2)
			}

			// Scan returns keys sorted lexicographically and scoped to prefix.
			_ = s.Set("p:c", 3)
			_ = s.Set("p:a", 1)
			_ = s.Set("p:b", 2)
			_ = s.Set("other:x", 9)

			kvs, err := s.Scan("p:")
			if err != nil {
				t.Fatalf("Scan: %v", err)
			}
			gotKeys := make([]string, len(kvs))
			for i, kv := range kvs {
				gotKeys[i] = kv.Key
			}
			wantKeys := []string{"p:a", "p:b", "p:c"}
			if !reflect.DeepEqual(gotKeys, wantKeys) {
				t.Fatalf("Scan keys = %v, want %v", gotKeys, wantKeys)
			}
			if !reflect.DeepEqual(s.ListKeysByPrefix("p:"), wantKeys) {
				t.Fatalf("ListKeysByPrefix = %v, want %v", s.ListKeysByPrefix("p:"), wantKeys)
			}
		})
	}
}

func TestStoreSetNXAtomic(t *testing.T) {
	s := New()

	const workers = 16
	var wins atomic.Int32
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			<-start
			ok, err := s.SetNX("lock", id)
			if err != nil {
				t.Errorf("SetNX: %v", err)
				return
			}
			if ok {
				wins.Add(1)
			}
		}(i)
	}
	close(start)
	wg.Wait()

	if wins.Load() != 1 {
		t.Fatalf("SetNX succeeded %d times, want 1", wins.Load())
	}
	if s.SetIfNotExists("lock", "overwrite") {
		t.Fatal("SetIfNotExists overwrote existing key")
	}
}

func TestStoreErrorPropagation(t *testing.T) {
	boom := errors.New("backend unavailable")
	s := NewWithBackend(unreadableBackend{NewMemoryBackend(), boom})

	if _, _, err := s.Lookup("k"); !errors.Is(err, boom) {
		t.Errorf("Lookup err = %v, want %v", err, boom)
	}
	if _, err := s.Scan("p:"); !errors.Is(err, boom) {
		t.Errorf("Scan err = %v, want %v", err, boom)
	}
	if _, found, err := GetAs[string](s, "k"); found || !errors.Is(err, boom) {
		t.Errorf("GetAs = found:%v err:%v, want false, %v", found, err, boom)
	}
}

func TestOpenFailsWhenPostgresUnreachable(t *testing.T) {
	t.Setenv("PERSISTENCE", "")
	t.Setenv("DATABASE_URL", "postgres://127.0.0.1:1/miniblue?sslmode=disable&connect_timeout=1")

	if _, err := Open(); err == nil {
		t.Fatal("Open succeeded with unreachable DATABASE_URL, want error")
	}
}

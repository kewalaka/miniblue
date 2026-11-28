package arm

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/moabukar/miniblue/internal/store"
)

func backends() map[string]func() *store.Store {
	return map[string]func() *store.Store{
		"memory": store.New,
	}
}

func mustID(t *testing.T, path string) ID {
	t.Helper()
	id, err := ParseID(path)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func doc(id ID) map[string]any {
	return map[string]any{"id": id.String(), "name": id.Name(), "properties": map[string]any{"n": 1}}
}

// Put is a test helper: an unconditional upsert built on Update.
func (s *Store) Put(id ID, d map[string]any) (created bool) {
	_, created, err := s.Update(id, func(map[string]any) (map[string]any, error) { return d, nil })
	if err != nil {
		panic(err)
	}
	return created
}

func ids(docs []map[string]any) []string {
	out := []string{}
	for _, d := range docs {
		out = append(out, d["id"].(string))
	}
	return out
}

func TestStorePutGetCaseInsensitive(t *testing.T) {
	for name, mk := range backends() {
		t.Run(name, func(t *testing.T) {
			s := NewStore(mk())
			id := mustID(t, "/subscriptions/a1/providers/Microsoft.Authorization/policyDefinitions/MyPolicy")
			if !s.Put(id, doc(id)) {
				t.Fatal("first Put should report created")
			}
			if s.Put(id, doc(id)) {
				t.Fatal("second Put should report update")
			}
			mangled := mustID(t, "/SUBSCRIPTIONS/A1/PROVIDERS/MICROSOFT.AUTHORIZATION/POLICYDEFINITIONS/MYPOLICY")
			got, ok, err := s.Get(mangled)
			if err != nil || !ok {
				t.Fatalf("mangled Get missed: ok=%v err=%v", ok, err)
			}
			if got["id"] != id.String() || got["name"] != "MyPolicy" {
				t.Errorf("original casing lost: %v", got)
			}
			if _, ok, err := s.Get(mustID(t, "/subscriptions/a1/providers/Microsoft.Authorization/policyDefinitions/other")); err != nil || ok {
				t.Errorf("unexpected hit: ok=%v err=%v", ok, err)
			}
		})
	}
}

func TestStoreCopiesValues(t *testing.T) {
	s := NewStore(store.New())
	id := mustID(t, "/subscriptions/a1/providers/Microsoft.Authorization/policyDefinitions/p")
	in := doc(id)
	s.Put(id, in)
	in["properties"].(map[string]any)["n"] = 99
	got, _, err := s.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	got["properties"].(map[string]any)["n"] = 42
	again, _, err := s.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	if n := again["properties"].(map[string]any)["n"]; n != json.Number("1") {
		t.Errorf("store state mutated through caller copies: n=%v", n)
	}
}

func TestStoreKeepsNumbersExact(t *testing.T) {
	id := mustID(t, "/subscriptions/a1/providers/Microsoft.Authorization/policyDefinitions/p")
	in, err := decodeObject(strings.NewReader(
		`{"properties":{"big":9007199254740993,"min":-9223372036854775808,"dec":1.10,"exp":1e400}}`))
	if err != nil {
		t.Fatal(err)
	}
	check := func(t *testing.T, s *Store) {
		t.Helper()
		got, ok, err := s.Get(id)
		if err != nil || !ok {
			t.Fatalf("missing: ok=%v err=%v", ok, err)
		}
		props := got["properties"].(map[string]any)
		for k, want := range map[string]json.Number{
			"big": "9007199254740993", "min": "-9223372036854775808", "dec": "1.10", "exp": "1e400",
		} {
			if props[k] != want {
				t.Errorf("%s = %v, want %v", k, props[k], want)
			}
		}
	}
	for name, mk := range backends() {
		t.Run(name, func(t *testing.T) {
			s := NewStore(mk())
			s.Put(id, in)
			check(t, s)
		})
	}
	t.Run("file reload", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "state.json")
		fb, err := store.NewFileBackend(path)
		if err != nil {
			t.Fatal(err)
		}
		NewStore(store.NewWithBackend(fb)).Put(id, in)
		if err := fb.Save(); err != nil {
			t.Fatal(err)
		}
		reloaded, err := store.NewFileBackend(path)
		if err != nil {
			t.Fatal(err)
		}
		check(t, NewStore(store.NewWithBackend(reloaded)))
	})
}

func TestStoreList(t *testing.T) {
	for name, mk := range backends() {
		t.Run(name, func(t *testing.T) {
			s := NewStore(mk())
			for _, p := range []string{
				"/subscriptions/a1/providers/Microsoft.Authorization/policyDefinitions/zeta",
				"/subscriptions/a1/providers/Microsoft.Authorization/policyDefinitions/Alpha",
				"/subscriptions/b2/providers/Microsoft.Authorization/policyDefinitions/other",
				"/subscriptions/a1/providers/Microsoft.Authorization/policySetDefinitions/set",
			} {
				id := mustID(t, p)
				s.Put(id, doc(id))
			}
			list, err := s.List(mustID(t, "/SUBSCRIPTIONS/A1/providers/microsoft.authorization/POLICYDEFINITIONS"))
			if err != nil {
				t.Fatal(err)
			}
			want := []string{
				"/subscriptions/a1/providers/Microsoft.Authorization/policyDefinitions/Alpha",
				"/subscriptions/a1/providers/Microsoft.Authorization/policyDefinitions/zeta",
			}
			if got := ids(list); !reflect.DeepEqual(got, want) {
				t.Errorf("List = %v, want %v", got, want)
			}
			empty, err := s.List(mustID(t, "/subscriptions/zz/providers/Microsoft.Authorization/policyDefinitions"))
			if err != nil {
				t.Fatal(err)
			}
			if len(empty) != 0 {
				t.Errorf("empty scope should list nothing, got %v", empty)
			}
		})
	}
}

func TestStoreReaderUnder(t *testing.T) {
	s := NewStore(store.New())
	for _, p := range []string{
		"/subscriptions/a1/resourceGroups/rg1/providers/Microsoft.Test/widgets/w1",
		"/subscriptions/a1/resourceGroups/rg2/providers/Microsoft.Test/widgets/w2",
		"/subscriptions/a1/resourceGroups/rg1/providers/Microsoft.Test/widgets/w1/parts/p1",
		"/subscriptions/b2/resourceGroups/rg1/providers/Microsoft.Test/widgets/w3",
	} {
		id := mustID(t, p)
		s.Put(id, doc(id))
	}

	subWidgets, err := s.Under(mustID(t, "/subscriptions/a1"), "Microsoft.Test/widgets")
	if err != nil {
		t.Fatal(err)
	}
	wantSub := []string{
		"/subscriptions/a1/resourceGroups/rg1/providers/Microsoft.Test/widgets/w1",
		"/subscriptions/a1/resourceGroups/rg2/providers/Microsoft.Test/widgets/w2",
	}
	if got := ids(subWidgets); !reflect.DeepEqual(got, wantSub) {
		t.Errorf("Under(sub) = %v, want %v", got, wantSub)
	}

	rgWidgets, err := s.Under(mustID(t, "/subscriptions/a1/resourceGroups/rg1"), "Microsoft.Test/widgets")
	if err != nil {
		t.Fatal(err)
	}
	wantRG := []string{"/subscriptions/a1/resourceGroups/rg1/providers/Microsoft.Test/widgets/w1"}
	if got := ids(rgWidgets); !reflect.DeepEqual(got, wantRG) {
		t.Errorf("Under(rg1) = %v, want %v", got, wantRG)
	}
}

func TestStoreListSkipsStaleIndexEntries(t *testing.T) {
	backend := store.New()
	s := NewStore(backend)
	a := mustID(t, "/subscriptions/a1/providers/Microsoft.Authorization/policyDefinitions/a")
	b := mustID(t, "/subscriptions/a1/providers/Microsoft.Authorization/policyDefinitions/b")
	s.Put(a, doc(a))
	s.Put(b, doc(b))
	backend.Delete(resKey(a))
	list, err := s.List(mustID(t, "/subscriptions/a1/providers/Microsoft.Authorization/policyDefinitions"))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := ids(list), []string{b.String()}; !reflect.DeepEqual(got, want) {
		t.Errorf("List = %v, want %v", got, want)
	}
}

func TestStoreDelete(t *testing.T) {
	backend := store.New()
	s := NewStore(backend)
	id := mustID(t, "/subscriptions/a1/providers/Microsoft.Authorization/policyDefinitions/p")
	s.Put(id, doc(id))
	existed, err := s.Delete(mustID(t, "/SUBSCRIPTIONS/A1/providers/microsoft.authorization/policydefinitions/P"))
	if err != nil || !existed {
		t.Fatalf("Delete should report existed: existed=%v err=%v", existed, err)
	}
	existed, err = s.Delete(id)
	if err != nil || existed {
		t.Errorf("second Delete should report absent: existed=%v err=%v", existed, err)
	}
	if keys := backend.List(); len(keys) != 0 {
		t.Errorf("index/resource keys left behind: %v", keys)
	}
}

func TestStoreDeleteTree(t *testing.T) {
	for name, mk := range backends() {
		t.Run(name, func(t *testing.T) {
			backend := mk()
			s := NewStore(backend)
			const rg = "/subscriptions/a1/resourceGroups/rg1/providers/Microsoft.Network"
			put := func(p string) ID {
				id := mustID(t, p)
				s.Put(id, doc(id))
				return id
			}
			vnet := put(rg + "/virtualNetworks/vnet")
			put(rg + "/virtualNetworks/vnet/subnets/s1")
			put(rg + "/virtualNetworks/vnet/subnets/s1/providers/Microsoft.Authorization/locks/l1") // extension on a child
			put(rg + "/virtualNetworks/vnet/providers/Microsoft.Authorization/locks/l2")            // extension on the parent
			keep := put(rg + "/virtualNetworks/vnet2")                                              // shares a name prefix
			keepChild := put(rg + "/virtualNetworks/vnet2/subnets/s1")

			existed, err := s.DeleteTree(mustID(t, rg+"/VIRTUALNETWORKS/VNET"))
			if err != nil || !existed {
				t.Fatalf("DeleteTree should report existed: existed=%v err=%v", existed, err)
			}
			if _, ok, err := s.Get(vnet); err != nil || ok {
				t.Errorf("vnet survived: ok=%v err=%v", ok, err)
			}
			if got, err := s.List(mustID(t, rg+"/virtualNetworks/vnet/subnets")); err != nil || len(got) != 0 {
				t.Errorf("subnets survived: %v err=%v", ids(got), err)
			}
			if _, ok, err := s.Get(keep); err != nil || !ok {
				t.Errorf("vnet2 was deleted: ok=%v err=%v", ok, err)
			}
			if _, ok, err := s.Get(keepChild); err != nil || !ok {
				t.Errorf("vnet2 child was deleted: ok=%v err=%v", ok, err)
			}
			list, err := s.List(mustID(t, rg+"/virtualNetworks"))
			if err != nil {
				t.Fatal(err)
			}
			if got := ids(list); !reflect.DeepEqual(got, []string{keep.String()}) {
				t.Errorf("List(virtualNetworks) = %v", got)
			}
			// Only vnet2, its child, and the two index records remain.
			if n := len(backend.List()); n != 4 {
				t.Errorf("backend keys = %v, want 4", backend.List())
			}
		})
	}
}

func TestStoreUpdate(t *testing.T) {
	s := NewStore(store.New())
	id := mustID(t, "/subscriptions/a1/providers/Microsoft.Authorization/policyDefinitions/p")
	coll := mustID(t, "/subscriptions/a1/providers/Microsoft.Authorization/policyDefinitions")

	var saw map[string]any
	got, created, err := s.Update(id, func(cur map[string]any) (map[string]any, error) {
		saw = cur
		return doc(id), nil
	})
	if err != nil || !created || saw != nil || got == nil {
		t.Fatalf("create: doc=%v created=%v saw=%v err=%v", got, created, saw, err)
	}

	got, created, err = s.Update(id, func(cur map[string]any) (map[string]any, error) {
		saw = cur
		cur["extra"] = true
		return cur, nil
	})
	if err != nil || created || saw == nil || got["extra"] != true {
		t.Fatalf("update: doc=%v created=%v saw=%v err=%v", got, created, saw, err)
	}
	if stored, _, err := s.Get(id); err != nil || stored["extra"] != true {
		t.Errorf("update not stored: %v err=%v", stored, err)
	}

	// Returning nil stores nothing, so an update-if-present cannot create.
	missing := mustID(t, "/subscriptions/a1/providers/Microsoft.Authorization/policyDefinitions/missing")
	got, created, err = s.Update(missing, func(map[string]any) (map[string]any, error) { return nil, nil })
	if got != nil || created || err != nil {
		t.Fatalf("nil result: doc=%v created=%v err=%v", got, created, err)
	}
	if _, ok, err := s.Get(missing); err != nil || ok {
		t.Errorf("nil result created the resource: ok=%v err=%v", ok, err)
	}

	// A document that cannot be encoded is an error and leaves no trace.
	_, _, err = s.Update(missing, func(map[string]any) (map[string]any, error) {
		return map[string]any{"bad": make(chan int)}, nil
	})
	if err == nil {
		t.Fatal("unencodable document accepted")
	}
	if _, ok, err := s.Get(missing); err != nil || ok {
		t.Errorf("failed Update created the resource: ok=%v err=%v", ok, err)
	}
	list, err := s.List(coll)
	if err != nil {
		t.Fatal(err)
	}
	if got := ids(list); !reflect.DeepEqual(got, []string{id.String()}) {
		t.Errorf("List = %v, want only %s", got, id)
	}
}

func TestStoreUpdateExcludesDelete(t *testing.T) {
	s := NewStore(store.New())
	id := mustID(t, "/subscriptions/a1/providers/Microsoft.Authorization/policyDefinitions/p")
	s.Put(id, doc(id))

	entered, release, updated := make(chan struct{}), make(chan struct{}), make(chan struct{})
	go func() {
		defer close(updated)
		_, _, _ = s.Update(id, func(cur map[string]any) (map[string]any, error) {
			close(entered)
			<-release
			cur["extra"] = true
			return cur, nil
		})
	}()
	<-entered

	deleted := make(chan bool, 1)
	go func() {
		ok, _ := s.Delete(id)
		deleted <- ok
	}()
	select {
	case <-deleted:
		t.Fatal("Delete completed while an Update was in flight")
	case <-time.After(50 * time.Millisecond):
	}

	close(release)
	<-updated
	if !<-deleted {
		t.Error("Delete should have removed the updated resource")
	}
	if _, ok, err := s.Get(id); err != nil || ok {
		t.Errorf("resource survived Delete: ok=%v err=%v", ok, err)
	}
}

func TestStoreDeleteTreeOfUnstoredParent(t *testing.T) {
	backend := store.New()
	s := NewStore(backend)
	child := mustID(t, "/subscriptions/a1/resourceGroups/rg1/providers/Microsoft.Network/virtualNetworks/v")
	s.Put(child, doc(child))
	existed, err := s.DeleteTree(mustID(t, "/subscriptions/a1/resourceGroups/rg1"))
	if err != nil || existed {
		t.Errorf("resource group is not stored, existed should be false: existed=%v err=%v", existed, err)
	}
	if keys := backend.List(); len(keys) != 0 {
		t.Errorf("descendants survived: %v", keys)
	}
}

// failingBackend injects errors into a MemoryBackend for error-propagation tests.
type failingBackend struct {
	*store.MemoryBackend
	getErr, setErr, deleteErr, scanErr, deletePrefixErr error
}

func (f *failingBackend) Get(key string) ([]byte, bool, error) {
	if f.getErr != nil {
		return nil, false, f.getErr
	}
	return f.MemoryBackend.Get(key)
}

func (f *failingBackend) Set(key string, v []byte) error {
	if f.setErr != nil {
		return f.setErr
	}
	return f.MemoryBackend.Set(key, v)
}

func (f *failingBackend) Delete(key string) (bool, error) {
	if f.deleteErr != nil {
		return false, f.deleteErr
	}
	return f.MemoryBackend.Delete(key)
}

func (f *failingBackend) Scan(prefix string) ([]store.KV, error) {
	if f.scanErr != nil {
		return nil, f.scanErr
	}
	return f.MemoryBackend.Scan(prefix)
}

func (f *failingBackend) DeleteByPrefix(prefix string) (int, error) {
	if f.deletePrefixErr != nil {
		return 0, f.deletePrefixErr
	}
	return f.MemoryBackend.DeleteByPrefix(prefix)
}

func TestStorePropagatesBackendErrors(t *testing.T) {
	boom := errors.New("backend unavailable")
	id := mustID(t, "/subscriptions/a1/providers/Microsoft.Test/widgets/w1")
	sub := mustID(t, "/subscriptions/a1")

	fb := &failingBackend{MemoryBackend: store.NewMemoryBackend(), getErr: boom}
	s := NewStore(store.NewWithBackend(fb))
	if _, _, err := s.Get(id); !errors.Is(err, boom) {
		t.Errorf("Get err = %v, want %v", err, boom)
	}
	if _, err := s.Children(sub, "Microsoft.Test/widgets"); !errors.Is(err, boom) {
		t.Errorf("Children err = %v, want %v", err, boom)
	}
	if _, _, err := s.Update(id, func(cur map[string]any) (map[string]any, error) { return doc(id), nil }); !errors.Is(err, boom) {
		t.Errorf("Update(read) err = %v, want %v", err, boom)
	}

	fb = &failingBackend{MemoryBackend: store.NewMemoryBackend(), scanErr: boom}
	s = NewStore(store.NewWithBackend(fb))
	if _, err := s.Under(sub, "Microsoft.Test/widgets"); !errors.Is(err, boom) {
		t.Errorf("Under err = %v, want %v", err, boom)
	}

	fb = &failingBackend{MemoryBackend: store.NewMemoryBackend(), setErr: boom}
	s = NewStore(store.NewWithBackend(fb))
	if _, _, err := s.Update(id, func(cur map[string]any) (map[string]any, error) { return doc(id), nil }); !errors.Is(err, boom) {
		t.Errorf("Update(write) err = %v, want %v", err, boom)
	}

	fb = &failingBackend{MemoryBackend: store.NewMemoryBackend(), deleteErr: boom}
	s = NewStore(store.NewWithBackend(fb))
	if _, err := s.Delete(id); !errors.Is(err, boom) {
		t.Errorf("Delete err = %v, want %v", err, boom)
	}
	if _, err := s.DeleteTree(id); !errors.Is(err, boom) {
		t.Errorf("DeleteTree err = %v, want %v", err, boom)
	}
}

package arm

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/moabukar/miniblue/internal/store"
)

// jsonBackend mimics the Postgres backend: values are JSON round-tripped, so
// maps come back as map[string]interface{} with float64 numbers.
type jsonBackend struct{ *store.MemoryBackend }

func (b jsonBackend) Set(key string, value interface{}) error {
	raw, _ := json.Marshal(value)
	var v interface{}
	_ = json.Unmarshal(raw, &v)
	return b.MemoryBackend.Set(key, v)
}

func backends() map[string]func() *store.Store {
	return map[string]func() *store.Store{
		"memory": store.New,
		"json":   func() *store.Store { return store.NewWithBackend(jsonBackend{store.NewMemoryBackend()}) },
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

func doc(id ID) map[string]interface{} {
	return map[string]interface{}{"id": id.String(), "name": id.Name(), "properties": map[string]interface{}{"n": 1}}
}

func ids(docs []map[string]interface{}) []string {
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
			got, ok := s.Get(mangled)
			if !ok {
				t.Fatal("mangled Get missed")
			}
			if got["id"] != id.String() || got["name"] != "MyPolicy" {
				t.Errorf("original casing lost: %v", got)
			}
			if _, ok := s.Get(mustID(t, "/subscriptions/a1/providers/Microsoft.Authorization/policyDefinitions/other")); ok {
				t.Error("unexpected hit")
			}
		})
	}
}

func TestStoreCopiesValues(t *testing.T) {
	s := NewStore(store.New())
	id := mustID(t, "/subscriptions/a1/providers/Microsoft.Authorization/policyDefinitions/p")
	in := doc(id)
	s.Put(id, in)
	in["properties"].(map[string]interface{})["n"] = 99
	got, _ := s.Get(id)
	got["properties"].(map[string]interface{})["n"] = 42
	again, _ := s.Get(id)
	if n := again["properties"].(map[string]interface{})["n"]; n != 1 {
		t.Errorf("store state mutated through caller copies: n=%v", n)
	}
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
			list := s.List(mustID(t, "/SUBSCRIPTIONS/A1/providers/microsoft.authorization/POLICYDEFINITIONS"))
			want := []string{
				"/subscriptions/a1/providers/Microsoft.Authorization/policyDefinitions/Alpha",
				"/subscriptions/a1/providers/Microsoft.Authorization/policyDefinitions/zeta",
			}
			if got := ids(list); !reflect.DeepEqual(got, want) {
				t.Errorf("List = %v, want %v", got, want)
			}
			if got := s.List(mustID(t, "/subscriptions/zz/providers/Microsoft.Authorization/policyDefinitions")); len(got) != 0 {
				t.Errorf("empty scope should list nothing, got %v", got)
			}
		})
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
	got := ids(s.List(mustID(t, "/subscriptions/a1/providers/Microsoft.Authorization/policyDefinitions")))
	if want := []string{b.String()}; !reflect.DeepEqual(got, want) {
		t.Errorf("List = %v, want %v", got, want)
	}
}

func TestStoreDelete(t *testing.T) {
	backend := store.New()
	s := NewStore(backend)
	id := mustID(t, "/subscriptions/a1/providers/Microsoft.Authorization/policyDefinitions/p")
	s.Put(id, doc(id))
	if !s.Delete(mustID(t, "/SUBSCRIPTIONS/A1/providers/microsoft.authorization/policydefinitions/P")) {
		t.Fatal("Delete should report existed")
	}
	if s.Delete(id) {
		t.Error("second Delete should report absent")
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

			if !s.DeleteTree(mustID(t, rg+"/VIRTUALNETWORKS/VNET")) {
				t.Fatal("DeleteTree should report existed")
			}
			if _, ok := s.Get(vnet); ok {
				t.Error("vnet survived")
			}
			if got := s.List(mustID(t, rg+"/virtualNetworks/vnet/subnets")); len(got) != 0 {
				t.Errorf("subnets survived: %v", ids(got))
			}
			if _, ok := s.Get(keep); !ok {
				t.Error("vnet2 was deleted")
			}
			if _, ok := s.Get(keepChild); !ok {
				t.Error("vnet2 child was deleted")
			}
			if got := ids(s.List(mustID(t, rg+"/virtualNetworks"))); !reflect.DeepEqual(got, []string{keep.String()}) {
				t.Errorf("List(virtualNetworks) = %v", got)
			}
			// Only vnet2, its child, and the two index records remain.
			if n := len(backend.List()); n != 4 {
				t.Errorf("backend keys = %v, want 4", backend.List())
			}
		})
	}
}

func TestStoreDeleteTreeOfUnstoredParent(t *testing.T) {
	backend := store.New()
	s := NewStore(backend)
	child := mustID(t, "/subscriptions/a1/resourceGroups/rg1/providers/Microsoft.Network/virtualNetworks/v")
	s.Put(child, doc(child))
	if s.DeleteTree(mustID(t, "/subscriptions/a1/resourceGroups/rg1")) {
		t.Error("resource group is not stored, existed should be false")
	}
	if keys := backend.List(); len(keys) != 0 {
		t.Errorf("descendants survived: %v", keys)
	}
}

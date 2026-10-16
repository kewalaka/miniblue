package arm

import (
	"sort"
	"strings"
	"sync"

	"github.com/moabukar/miniblue/internal/store"
)

// Store keeps ARM resources in a store.Backend under keys derived from the
// lowercased canonical ID, so lookups are case-insensitive while the stored
// document keeps the casing it was created with.
//
// Layout:
//
//	arm:r:<lower id>                   -> resource document
//	arm:i:<lower parent>|<lower type>  -> sorted list of child resource keys
//
// The child index makes listing O(children) instead of a ListByPrefix scan of
// the whole backend (O(n), see upstream issue #95). Only DeleteTree scans by
// prefix, and only for types that declare children.
type Store struct {
	mu sync.Mutex // serialises index read-modify-write
	s  *store.Store
}

// NewStore returns a Store over s.
func NewStore(s *store.Store) *Store { return &Store{s: s} }

func resKey(id ID) string { return "arm:r:" + id.Key() }

func idxKey(id ID) string {
	return "arm:i:" + strings.ToLower(id.Parent().String()) + "|" + strings.ToLower(id.TypeName())
}

// Get returns a copy of the document stored for id.
func (s *Store) Get(id ID) (map[string]interface{}, bool) {
	v, ok := s.s.Get(resKey(id))
	if !ok {
		return nil, false
	}
	doc, _ := deepCopy(v).(map[string]interface{})
	return doc, doc != nil
}

// Put stores a copy of doc under id and reports whether it was newly created.
func (s *Store) Put(id ID, doc map[string]interface{}) (created bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := resKey(id)
	created = !s.s.Exists(key)
	s.s.Set(key, deepCopy(doc))
	if created {
		s.indexAdd(idxKey(id), key)
	}
	return created
}

// Delete removes the resource and reports whether it existed.
func (s *Store) Delete(id ID) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := resKey(id)
	if !s.s.Delete(key) {
		return false
	}
	s.indexRemove(idxKey(id), key)
	return true
}

// DeleteTree removes the resource (if present) and everything beneath it:
// child resources and extension resources, with their indexes. It scans the
// backend by prefix, so use it only for deletes that can have descendants.
func (s *Store) DeleteTree(id ID) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := id.Key()
	existed := s.s.Delete("arm:r:" + k)
	if existed {
		s.indexRemove(idxKey(id), "arm:r:"+k)
	}
	s.s.DeleteByPrefix("arm:r:" + k + "/")
	s.s.DeleteByPrefix("arm:i:" + k + "/")
	s.s.DeleteByPrefix("arm:i:" + k + "|")
	return existed
}

// List returns copies of the resources in a collection, ordered by ID.
func (s *Store) List(collection ID) []map[string]interface{} {
	out := []map[string]interface{}{}
	for _, key := range s.index(idxKey(collection)) {
		if v, ok := s.s.Get(key); ok {
			if doc, _ := deepCopy(v).(map[string]interface{}); doc != nil {
				out = append(out, doc)
			}
		}
	}
	return out
}

func (s *Store) index(key string) []string {
	v, ok := s.s.Get(key)
	if !ok {
		return nil
	}
	items, _ := v.([]interface{})
	keys := make([]string, 0, len(items))
	for _, it := range items {
		if k, ok := it.(string); ok {
			keys = append(keys, k)
		}
	}
	return keys
}

func (s *Store) writeIndex(key string, keys []string) {
	if len(keys) == 0 {
		s.s.Delete(key)
		return
	}
	items := make([]interface{}, len(keys))
	for i, k := range keys {
		items[i] = k
	}
	s.s.Set(key, items)
}

func (s *Store) indexAdd(idx, key string) {
	keys := s.index(idx)
	i := sort.SearchStrings(keys, key)
	if i < len(keys) && keys[i] == key {
		return
	}
	keys = append(keys, "")
	copy(keys[i+1:], keys[i:])
	keys[i] = key
	s.writeIndex(idx, keys)
}

func (s *Store) indexRemove(idx, key string) {
	keys := s.index(idx)
	i := sort.SearchStrings(keys, key)
	if i == len(keys) || keys[i] != key {
		return
	}
	s.writeIndex(idx, append(keys[:i], keys[i+1:]...))
}

// deepCopy copies JSON-shaped values. The memory backend stores values by
// reference, so without copies callers could mutate stored state.
func deepCopy(v interface{}) interface{} {
	switch t := v.(type) {
	case map[string]interface{}:
		m := make(map[string]interface{}, len(t))
		for k, e := range t {
			m[k] = deepCopy(e)
		}
		return m
	case []interface{}:
		a := make([]interface{}, len(t))
		for i, e := range t {
			a[i] = deepCopy(e)
		}
		return a
	default:
		return v
	}
}

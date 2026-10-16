package arm

import (
	"encoding/json"
	"io"
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
//	arm:r:<lower id>                   -> resource document (JSON text)
//	arm:i:<lower parent>|<lower type>  -> sorted list of child resource keys
//
// Documents are stored as JSON text, not as maps: the file and Postgres
// backends decode stored values into interface{}, which turns every number
// into a float64 and silently changes integers beyond 2^53. A string survives
// every backend unchanged, and is decoded here with json.Number for the same
// reason. It also means callers never share state with the backend.
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

// Get returns the document stored for id. Numbers are json.Number.
func (s *Store) Get(id ID) (map[string]interface{}, bool) {
	v, ok := s.s.Get(resKey(id))
	if !ok {
		return nil, false
	}
	return decodeDoc(v)
}

// Put stores doc under id and reports whether it was newly created.
func (s *Store) Put(id ID, doc map[string]interface{}) (created bool) {
	raw, err := json.Marshal(doc)
	if err != nil {
		panic("arm: document is not JSON-encodable: " + err.Error())
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	key := resKey(id)
	created = !s.s.Exists(key)
	s.s.Set(key, string(raw))
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

// List returns the resources in a collection, ordered by ID.
func (s *Store) List(collection ID) []map[string]interface{} {
	out := []map[string]interface{}{}
	for _, key := range s.index(idxKey(collection)) {
		if v, ok := s.s.Get(key); ok {
			if doc, ok := decodeDoc(v); ok {
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

// decodeObject decodes a JSON object keeping numbers as json.Number, so
// integers beyond float64 precision round-trip unchanged.
func decodeObject(r io.Reader) (map[string]interface{}, error) {
	var m map[string]interface{}
	dec := json.NewDecoder(r)
	dec.UseNumber()
	if err := dec.Decode(&m); err != nil {
		return nil, err
	}
	return m, nil
}

func decodeDoc(v interface{}) (map[string]interface{}, bool) {
	raw, ok := v.(string)
	if !ok {
		return nil, false
	}
	doc, err := decodeObject(strings.NewReader(raw))
	return doc, err == nil && doc != nil
}

package arm

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"

	"github.com/moabukar/miniblue/internal/store"
)

// Reader provides lock-free reads against the ARM store. Backend errors are
// returned, never swallowed.
type Reader interface {
	Get(id ID) (map[string]any, bool, error)
	Children(parent ID, typeName string) ([]map[string]any, error)
	Under(scope ID, typeName string) ([]map[string]any, error)
}

// storeError marks backend and decode errors from Store so Engine.Do never
// downgrades a store failure returned from a Validate hook to a 400.
type storeError struct{ err error }

func (e *storeError) Error() string { return e.err.Error() }
func (e *storeError) Unwrap() error { return e.err }

func wrapStoreErr(err error) error {
	if err == nil {
		return nil
	}
	var se *storeError
	if errors.As(err, &se) {
		return err
	}
	return &storeError{err: err}
}

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
	mu sync.Mutex // serialises all writes, including each index read-modify-write
	s  *store.Store
}

// NewStore returns a Store over s.
func NewStore(s *store.Store) *Store { return &Store{s: s} }

func resKey(id ID) string { return "arm:r:" + id.Key() }

func idxKey(id ID) string {
	return "arm:i:" + strings.ToLower(id.Parent().String()) + "|" + strings.ToLower(id.TypeName())
}

// Get returns the document stored for id. Numbers are json.Number.
// Backend and decode errors are returned, never reported as not found.
func (s *Store) Get(id ID) (map[string]any, bool, error) {
	raw, ok, err := s.s.Lookup(resKey(id))
	if err != nil {
		return nil, false, wrapStoreErr(err)
	}
	if !ok {
		return nil, false, nil
	}
	doc, err := decodeStoredDoc(raw)
	if err != nil {
		return nil, false, wrapStoreErr(err)
	}
	return doc, true, nil
}

// Children returns the direct child resources of typeName under parent,
// ordered by ID.
func (s *Store) Children(parent ID, typeName string) ([]map[string]any, error) {
	if !strings.Contains(typeName, "/") && !parent.IsTenant() {
		typeName = parent.TypeName() + "/" + typeName
	}
	idx := "arm:i:" + strings.ToLower(parent.String()) + "|" + strings.ToLower(typeName)
	keys, err := s.index(idx)
	if err != nil {
		return nil, wrapStoreErr(err)
	}
	out := []map[string]any{}
	for _, key := range keys {
		raw, ok, err := s.s.Lookup(key)
		if err != nil {
			return nil, wrapStoreErr(err)
		}
		if !ok {
			continue
		}
		doc, err := decodeStoredDoc(raw)
		if err != nil {
			return nil, wrapStoreErr(err)
		}
		out = append(out, doc)
	}
	return out, nil
}

// Under returns all resources of typeName anywhere under scope (e.g.
// subscription- or resource-group-wide), ordered by ID.
func (s *Store) Under(scope ID, typeName string) ([]map[string]any, error) {
	prefix := "arm:r:/"
	if !scope.IsTenant() {
		prefix = "arm:r:" + scope.Key() + "/"
	}
	kvs, err := s.s.Scan(prefix)
	if err != nil {
		return nil, wrapStoreErr(err)
	}
	out := []map[string]any{}
	for _, kv := range kvs {
		id, err := ParseID(strings.TrimPrefix(kv.Key, "arm:r:"))
		if err != nil || !strings.EqualFold(id.TypeName(), typeName) {
			continue
		}
		doc, err := decodeStoredDoc(kv.Value)
		if err != nil {
			return nil, wrapStoreErr(err)
		}
		out = append(out, doc)
	}
	return out, nil
}

// Update atomically replaces the document for id. fn receives the current
// document, or nil if there is none, and returns the document to store; nil
// stores nothing. It runs under the store write lock, so it may perform
// lock-free reads via Reader methods, but must not call write methods on Store.
// Update, Delete and DeleteTree are mutually exclusive, which makes
// create-if-absent, update-if-present and delete race-free within this
// process. Update returns the stored document and whether it was created.
func (s *Store) Update(id ID, fn func(cur map[string]any) (map[string]any, error)) (doc map[string]any, created bool, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := resKey(id)
	cur, exists, err := s.Get(id)
	if err != nil {
		return nil, false, err
	}
	next, err := fn(cur)
	if err != nil {
		return nil, false, err
	}
	if next == nil {
		return nil, false, nil
	}
	raw, err := json.Marshal(next)
	if err != nil {
		return nil, false, wrapStoreErr(err)
	}
	if err := s.s.Set(key, string(raw)); err != nil {
		return nil, false, wrapStoreErr(err)
	}
	if !exists {
		if err := s.indexAdd(idxKey(id), key); err != nil {
			return nil, false, wrapStoreErr(err)
		}
	}
	return next, !exists, nil
}

// Delete removes the resource and reports whether it existed.
func (s *Store) Delete(id ID) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := resKey(id)
	existed, err := s.s.Remove(key)
	if err != nil {
		return false, wrapStoreErr(err)
	}
	if !existed {
		return false, nil
	}
	if err := s.indexRemove(idxKey(id), key); err != nil {
		return true, wrapStoreErr(err)
	}
	return true, nil
}

// DeleteTree removes the resource (if present) and everything beneath it:
// child resources and extension resources, with their indexes. It scans the
// backend by prefix, so use it only for deletes that can have descendants.
func (s *Store) DeleteTree(id ID) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := id.Key()
	existed, err := s.s.Remove("arm:r:" + k)
	if err != nil {
		return false, wrapStoreErr(err)
	}
	if existed {
		if err := s.indexRemove(idxKey(id), "arm:r:"+k); err != nil {
			return true, wrapStoreErr(err)
		}
	}
	if _, err := s.s.RemoveByPrefix("arm:r:" + k + "/"); err != nil {
		return existed, wrapStoreErr(err)
	}
	if _, err := s.s.RemoveByPrefix("arm:i:" + k + "/"); err != nil {
		return existed, wrapStoreErr(err)
	}
	if _, err := s.s.RemoveByPrefix("arm:i:" + k + "|"); err != nil {
		return existed, wrapStoreErr(err)
	}
	return existed, nil
}

// List returns the resources in a collection, ordered by ID.
func (s *Store) List(collection ID) ([]map[string]any, error) {
	return s.Children(collection.Parent(), collection.TypeName())
}

func (s *Store) index(key string) ([]string, error) {
	raw, ok, err := s.s.Lookup(key)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}
	var keys []string
	if err := json.Unmarshal(raw, &keys); err != nil {
		return nil, fmt.Errorf("arm: decode index %q: %w", key, err)
	}
	return keys, nil
}

func (s *Store) writeIndex(key string, keys []string) error {
	if len(keys) == 0 {
		_, err := s.s.Remove(key)
		return err
	}
	return s.s.Set(key, keys)
}

func (s *Store) indexAdd(idx, key string) error {
	keys, err := s.index(idx)
	if err != nil {
		return err
	}
	i := sort.SearchStrings(keys, key)
	if i < len(keys) && keys[i] == key {
		return nil
	}
	keys = append(keys, "")
	copy(keys[i+1:], keys[i:])
	keys[i] = key
	return s.writeIndex(idx, keys)
}

func (s *Store) indexRemove(idx, key string) error {
	keys, err := s.index(idx)
	if err != nil {
		return err
	}
	i := sort.SearchStrings(keys, key)
	if i == len(keys) || keys[i] != key {
		return nil
	}
	return s.writeIndex(idx, append(keys[:i], keys[i+1:]...))
}

// decodeObject decodes a JSON object keeping numbers as json.Number, so
// integers beyond float64 precision round-trip unchanged.
func decodeObject(r io.Reader) (map[string]any, error) {
	var m map[string]any
	dec := json.NewDecoder(r)
	dec.UseNumber()
	if err := dec.Decode(&m); err != nil {
		return nil, err
	}
	return m, nil
}

func decodeStoredDoc(raw []byte) (map[string]any, error) {
	var docJSON string
	if err := json.Unmarshal(raw, &docJSON); err != nil {
		return nil, fmt.Errorf("arm: decode stored envelope: %w", err)
	}
	doc, err := decodeObject(strings.NewReader(docJSON))
	if err != nil {
		return nil, fmt.Errorf("arm: decode stored doc: %w", err)
	}
	if doc == nil {
		return nil, errors.New("arm: stored doc is null")
	}
	return doc, nil
}

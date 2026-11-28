package arm

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
//	arm:r:<lower id> -> resource document (JSON text)
//
// Documents are stored as JSON text, not as maps: the file and Postgres
// backends decode stored values into interface{}, which turns every number
// into a float64 and silently changes integers beyond 2^53. A string survives
// every backend unchanged, and is decoded here with json.Number for the same
// reason. It also means callers never share state with the backend.
//
// Lists are served via prefix Scan over arm:r: with a depth filter. Leftover
// arm:i: keys from earlier versions are ignored.
type Store struct {
	mu sync.Mutex // serialises all writes
	s  *store.Store
}

// NewStore returns a Store over s.
func NewStore(s *store.Store) *Store { return &Store{s: s} }

func resKey(id ID) string { return "arm:r:" + id.Key() }

func collectionPrefix(parent ID, typeName string) string {
	lowerType := strings.ToLower(typeName)
	if parent.IsTenant() {
		if lowerType == strings.ToLower(nsResources+"/"+typeSubscriptions) {
			return "arm:r:/" + strings.ToLower(typeSubscriptions) + "/"
		}
		return "arm:r:/providers/" + lowerType + "/"
	}
	base := "arm:r:" + parent.Key()
	if parent.is(nsResources, typeSubscriptions) && lowerType == strings.ToLower(nsResources+"/"+typeResourceGroups) {
		return base + "/" + strings.ToLower(typeResourceGroups) + "/"
	}
	if !strings.Contains(lowerType, "/") {
		return base + "/" + lowerType + "/"
	}
	parentType := strings.ToLower(parent.TypeName()) + "/"
	if childSeg, ok := strings.CutPrefix(lowerType, parentType); ok && !strings.Contains(childSeg, "/") {
		return base + "/" + childSeg + "/"
	}
	return base + "/providers/" + lowerType + "/"
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
	prefix := collectionPrefix(parent, typeName)
	kvs, err := s.s.Scan(prefix)
	if err != nil {
		return nil, wrapStoreErr(err)
	}
	out := []map[string]any{}
	for _, kv := range kvs {
		rest := strings.TrimPrefix(kv.Key, prefix)
		if rest == "" || strings.Contains(rest, "/") {
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
// Update, DeleteTree and DeleteScope are mutually exclusive, which makes
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
	return next, !exists, nil
}

// DeleteTree removes the resource (if present) and everything beneath it:
// child resources and extension resources.
func (s *Store) DeleteTree(id ID) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := id.Key()
	existed, err := s.s.Remove("arm:r:" + k)
	if err != nil {
		return false, wrapStoreErr(err)
	}
	if _, err := s.s.RemoveByPrefix("arm:r:" + k + "/"); err != nil {
		return existed, wrapStoreErr(err)
	}
	return existed, nil
}

// DeleteScope removes every engine resource stored beneath scope (for example,
// all resources in a resource group). The scope itself is not removed.
func (s *Store) DeleteScope(scope ID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	prefix := "arm:r:/"
	if !scope.IsTenant() {
		prefix = "arm:r:" + scope.Key() + "/"
	}
	_, err := s.s.RemoveByPrefix(prefix)
	return wrapStoreErr(err)
}

// List returns the resources in a collection, ordered by ID.
func (s *Store) List(collection ID) ([]map[string]any, error) {
	return s.Children(collection.Parent(), collection.TypeName())
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

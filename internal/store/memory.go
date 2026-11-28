package store

import (
	"cmp"
	"encoding/json"
	"log"
	"slices"
	"strings"
	"sync"
	"time"
)

// MemoryBackend is an in-memory byte-KV implementation of Backend.
type MemoryBackend struct {
	mu   sync.RWMutex
	data map[string][]byte
}

// NewMemoryBackend creates a new in-memory backend.
func NewMemoryBackend() *MemoryBackend {
	return &MemoryBackend{data: make(map[string][]byte)}
}

func (m *MemoryBackend) Get(key string) ([]byte, bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	v, ok := m.data[key]
	if !ok {
		return nil, false, nil
	}
	return slices.Clone(v), true, nil
}

func (m *MemoryBackend) Set(key string, v []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data[key] = slices.Clone(v)
	return nil
}

func (m *MemoryBackend) SetNX(key string, v []byte) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.data[key]; exists {
		return false, nil
	}
	m.data[key] = slices.Clone(v)
	return true, nil
}

func (m *MemoryBackend) Delete(key string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.data[key]; !ok {
		return false, nil
	}
	delete(m.data, key)
	return true, nil
}

func (m *MemoryBackend) Scan(prefix string) ([]KV, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	results := make([]KV, 0)
	for k, v := range m.data {
		if strings.HasPrefix(k, prefix) {
			results = append(results, KV{Key: k, Value: slices.Clone(v)})
		}
	}
	slices.SortFunc(results, func(a, b KV) int { return cmp.Compare(a.Key, b.Key) })
	return results, nil
}

func (m *MemoryBackend) DeleteByPrefix(prefix string) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	count := 0
	for k := range m.data {
		if strings.HasPrefix(k, prefix) {
			delete(m.data, k)
			count++
		}
	}
	return count, nil
}

// Reset drops all state except the schema stamp, which describes this miniblue
// version rather than the data. Doing it under the lock means the stamp is
// never absent, even to a concurrent Reset or Snapshot.
func (m *MemoryBackend) Reset() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	stamp, stamped := m.data[schemaKey]
	m.data = make(map[string][]byte)
	if stamped {
		m.data[schemaKey] = stamp
	}
	return nil
}

func (m *MemoryBackend) Snapshot() map[string][]byte {
	m.mu.RLock()
	defer m.mu.RUnlock()
	snapshot := make(map[string][]byte, len(m.data))
	for k, v := range m.data {
		snapshot[k] = slices.Clone(v)
	}
	return snapshot
}

// Store wraps a Backend and owns JSON encoding/decoding for all handlers.
// Every backend stores raw JSON bytes, so memory, file, and Postgres behave
// identically.
type Store struct {
	backend Backend
}

// New creates a Store backed by an in-memory backend.
func New() *Store {
	return &Store{backend: NewMemoryBackend()}
}

// NewWithBackend creates a Store backed by the given Backend.
func NewWithBackend(b Backend) *Store {
	return &Store{backend: b}
}

// Lookup reads the raw JSON bytes for key, propagating backend errors.
func (s *Store) Lookup(key string) ([]byte, bool, error) {
	return s.backend.Get(key)
}

// Scan returns all key-value pairs whose key starts with prefix, sorted by key,
// propagating backend errors.
func (s *Store) Scan(prefix string) ([]KV, error) {
	return s.backend.Scan(prefix)
}

// Remove deletes key and reports whether it existed, propagating backend errors.
func (s *Store) Remove(key string) (bool, error) {
	return s.backend.Delete(key)
}

// RemoveByPrefix deletes all keys with prefix and returns the count,
// propagating backend errors.
func (s *Store) RemoveByPrefix(prefix string) (int, error) {
	return s.backend.DeleteByPrefix(prefix)
}

// Set marshals value to JSON and stores it under key. Existing callers that
// ignore the return value still compile; new and migrated code should check it.
func (s *Store) Set(key string, value interface{}) error {
	data, err := json.Marshal(value)
	if err != nil {
		log.Printf("store: marshal %q: %v", key, err)
		return err
	}
	if err := s.backend.Set(key, data); err != nil {
		log.Printf("store: set %q: %v", key, err)
		return err
	}
	return nil
}

// SetNX atomically marshals and stores value only if key does not exist.
func (s *Store) SetNX(key string, value interface{}) (bool, error) {
	data, err := json.Marshal(value)
	if err != nil {
		log.Printf("store: marshal %q: %v", key, err)
		return false, err
	}
	ok, err := s.backend.SetNX(key, data)
	if err != nil {
		log.Printf("store: setnx %q: %v", key, err)
		return false, err
	}
	return ok, nil
}

// SetIfNotExists atomically sets key only if it does not exist.
func (s *Store) SetIfNotExists(key string, value interface{}) bool {
	ok, err := s.SetNX(key, value)
	if err != nil {
		return false
	}
	return ok
}

func (s *Store) Exists(key string) bool {
	_, ok, err := s.backend.Get(key)
	if err != nil {
		log.Printf("store: exists %q: %v", key, err)
		return false
	}
	return ok
}

func (s *Store) Get(key string) (interface{}, bool) {
	raw, ok, err := s.backend.Get(key)
	if err != nil {
		log.Printf("store: get %q: %v", key, err)
		return nil, false
	}
	if !ok {
		return nil, false
	}
	var v interface{}
	if err := json.Unmarshal(raw, &v); err != nil {
		log.Printf("store: unmarshal %q: %v", key, err)
		return nil, false
	}
	return v, true
}

func (s *Store) Delete(key string) bool {
	ok, err := s.backend.Delete(key)
	if err != nil {
		log.Printf("store: delete %q: %v", key, err)
		return false
	}
	return ok
}

func (s *Store) List() []string {
	return s.ListKeysByPrefix("")
}

func (s *Store) ListByPrefix(prefix string) []interface{} {
	kvs, err := s.backend.Scan(prefix)
	if err != nil {
		log.Printf("store: scan %q: %v", prefix, err)
		return []interface{}{}
	}
	results := make([]interface{}, 0, len(kvs))
	for _, kv := range kvs {
		var v interface{}
		if err := json.Unmarshal(kv.Value, &v); err != nil {
			log.Printf("store: unmarshal %q: %v", kv.Key, err)
			continue
		}
		results = append(results, v)
	}
	return results
}

func (s *Store) ListKeysByPrefix(prefix string) []string {
	kvs, err := s.backend.Scan(prefix)
	if err != nil {
		log.Printf("store: scan %q: %v", prefix, err)
		return []string{}
	}
	keys := make([]string, 0, len(kvs))
	for _, kv := range kvs {
		keys = append(keys, kv.Key)
	}
	return keys
}

func (s *Store) CountByPrefix(prefix string) int {
	kvs, err := s.backend.Scan(prefix)
	if err != nil {
		log.Printf("store: count %q: %v", prefix, err)
		return 0
	}
	return len(kvs)
}

func (s *Store) DeleteByPrefix(prefix string) int {
	n, err := s.backend.DeleteByPrefix(prefix)
	if err != nil {
		log.Printf("store: deleteByPrefix %q: %v", prefix, err)
		return 0
	}
	return n
}

// Reset wipes all state except the schema stamp, which each backend keeps.
func (s *Store) Reset() {
	if err := s.backend.Reset(); err != nil {
		log.Printf("store: reset: %v", err)
	}
}

// Save persists state to disk if the backend supports it (e.g. FileBackend).
// Returns nil if the backend does not support persistence.
func (s *Store) Save() error {
	if fb, ok := s.backend.(*FileBackend); ok {
		return fb.Save()
	}
	return nil
}

// StartAutoSave starts periodic auto-save if the backend is a FileBackend.
// Returns a no-op stop function for other backends.
func (s *Store) StartAutoSave(interval time.Duration) func() {
	if fb, ok := s.backend.(*FileBackend); ok {
		return fb.StartAutoSave(interval)
	}
	return func() {}
}

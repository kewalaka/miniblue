package store

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
)

// Open creates the Store selected by the environment and verifies its schema
// stamp. A non-nil error means startup must stop: the state needs a reset,
// a state file cannot be loaded, or Postgres cannot be reached.
// Priority: PERSISTENCE=1 -> FileBackend, DATABASE_URL -> PostgreSQL,
// otherwise in-memory.
func Open() (*Store, error) {
	b, location, err := newBackend()
	if err != nil {
		return nil, err
	}
	s := NewWithBackend(b)
	if err := CheckSchema(s, location); err != nil {
		return nil, err
	}
	return s, nil
}

// newBackend also returns a description of where the state lives. It never
// includes DATABASE_URL, which may carry credentials. Failing to load a state
// file or connect to Postgres returns an error instead of falling back to
// memory, avoiding silent data loss.
func newBackend() (Backend, string, error) {
	if os.Getenv("PERSISTENCE") == "1" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, "", fmt.Errorf("resolve home directory for file persistence: %w", err)
		}
		path := filepath.Join(home, ".miniblue", "state.json")
		log.Printf("Using file persistence: %s", path)
		fb, err := NewFileBackend(path)
		if err != nil {
			return nil, path, err
		}
		return fb, path, nil
	}
	if url := os.Getenv("DATABASE_URL"); url != "" {
		pg, err := NewPostgresBackend(url)
		if err != nil {
			return nil, "Postgres (DATABASE_URL)", fmt.Errorf("connect to Postgres: %w", err)
		}
		return pg, "Postgres (DATABASE_URL)", nil
	}
	return NewMemoryBackend(), "memory", nil
}

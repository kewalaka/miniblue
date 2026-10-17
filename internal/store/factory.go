package store

import (
	"log"
	"os"
	"path/filepath"
)

// Open creates the Store selected by the environment and verifies its schema
// stamp. A non-nil error means startup must stop: the state needs a reset,
// or a state file that cannot be loaded has to be fixed or deleted.
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
// includes DATABASE_URL, which may carry credentials. A state file that
// cannot be read or parsed is an error: starting empty would let autosave
// overwrite it.
func newBackend() (Backend, string, error) {
	if os.Getenv("PERSISTENCE") == "1" {
		home, err := os.UserHomeDir()
		if err != nil {
			log.Printf("Failed to get home dir: %v, falling back to in-memory", err)
			return NewMemoryBackend(), "memory", nil
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
			log.Printf("Failed to connect to Postgres: %v, falling back to in-memory", err)
			return NewMemoryBackend(), "memory", nil
		}
		return pg, "Postgres (DATABASE_URL)", nil
	}
	return NewMemoryBackend(), "memory", nil
}

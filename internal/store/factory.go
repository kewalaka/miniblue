package store

import (
	"log"
	"os"
	"path/filepath"
)

// NewBackend creates the appropriate backend based on environment variables.
// Priority: PERSISTENCE=1 -> FileBackend, DATABASE_URL -> PostgreSQL, otherwise in-memory.
func NewBackend() Backend {
	b, _ := newBackend()
	return b
}

// Open creates the Store selected by the environment and verifies its schema
// stamp. A non-nil error means startup must stop: the state needs a reset.
func Open() (*Store, error) {
	b, location := newBackend()
	s := NewWithBackend(b)
	if err := CheckSchema(s, location); err != nil {
		return nil, err
	}
	return s, nil
}

// newBackend also returns a description of where the state lives. It never
// includes DATABASE_URL, which may carry credentials.
func newBackend() (Backend, string) {
	if os.Getenv("PERSISTENCE") == "1" {
		home, err := os.UserHomeDir()
		if err != nil {
			log.Printf("Failed to get home dir: %v, falling back to in-memory", err)
			return NewMemoryBackend(), "memory"
		}
		path := filepath.Join(home, ".miniblue", "state.json")
		log.Printf("Using file persistence: %s", path)
		return NewFileBackend(path), path
	}
	if url := os.Getenv("DATABASE_URL"); url != "" {
		pg, err := NewPostgresBackend(url)
		if err != nil {
			log.Printf("Failed to connect to Postgres: %v, falling back to in-memory", err)
			return NewMemoryBackend(), "memory"
		}
		return pg, "Postgres (DATABASE_URL)"
	}
	return NewMemoryBackend(), "memory"
}

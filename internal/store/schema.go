package store

import (
	"encoding/json"
	"fmt"
)

// SchemaVersion identifies the key and value shape of persisted state. Bump it
// when the shape of an existing persisted type changes, and add a CHANGELOG
// line: "state reset required: <why>".
const SchemaVersion = 1

const schemaKey = "meta:schema"

// unstampedVersion is the schema of state written before stamps existed.
const unstampedVersion = 1

// schemaReader is implemented by backends whose reads can fail (Postgres).
// Their Get and List report a failure as a miss, which here would pass for an
// absent stamp and let the check overwrite it.
type schemaReader interface {
	// readSchema returns the stored stamp, whether one exists, and whether the
	// store holds any state. A read or decode failure is an error, never a miss.
	readSchema() (stamp interface{}, stamped, hasState bool, err error)
}

func readSchema(b Backend) (stamp interface{}, stamped, hasState bool, err error) {
	if r, ok := b.(schemaReader); ok {
		return r.readSchema()
	}
	stamp, stamped = b.Get(schemaKey)
	return stamp, stamped, len(b.List()) > 0, nil
}

// CheckSchema verifies the schema stamp in s, writing it for an empty store.
// It must run once at startup, before anything reads or writes state. A stamp
// that differs from SchemaVersion, or one that cannot be read, is an error and
// the store is left untouched. location names where the state lives, for the
// error message.
func CheckSchema(s *Store, location string) error {
	stamp, stamped, hasState, err := readSchema(s.backend)
	if err != nil {
		return fmt.Errorf("read schema stamp from %s: %w", location, err)
	}
	// State without a stamp predates stamps; it is only assumed to be version 1
	// once the stamp is confirmed absent.
	if !stamped && hasState {
		stamp, stamped = unstampedVersion, true
	}

	if stamped {
		v, ok := stampVersion(stamp)
		switch {
		case !ok:
			return resetError(location, fmt.Sprintf("has an unreadable schema stamp %v", stamp))
		case v != SchemaVersion:
			return resetError(location, fmt.Sprintf("is schema %d", v))
		}
	}
	return s.backend.Set(schemaKey, SchemaVersion)
}

func resetError(location, found string) error {
	return fmt.Errorf("state in %s %s; this miniblue needs %d: delete the file (or TRUNCATE miniblue_store on Postgres) and restart",
		location, found, SchemaVersion)
}

// stampVersion reads a stamp as stored: an int in memory, a float64 once it
// has been through JSON.
func stampVersion(v interface{}) (int, bool) {
	b, err := json.Marshal(v)
	if err != nil {
		return 0, false
	}
	var n int
	return n, json.Unmarshal(b, &n) == nil
}

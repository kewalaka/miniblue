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

// CheckSchema verifies the schema stamp in s, writing it for an empty store.
// It must run once at startup, before anything reads or writes state. A stamp
// that differs from SchemaVersion is an error and the store is left untouched.
// location names where the state lives, for the error message.
func CheckSchema(s *Store, location string) error {
	stamp, stamped := s.backend.Get(schemaKey)
	if !stamped && len(s.backend.List()) > 0 {
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

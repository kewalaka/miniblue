package store

import (
	"encoding/json"
	"fmt"
)

// Decode converts a value returned by Store.Get or ListByPrefix into T.
// Memory hands back the Go value that was stored, while file-after-restart and
// Postgres hand back decoded JSON (maps and float64s), so handlers must not
// assert a struct type directly. A value that already is a T is returned as is
// (sharing any maps it holds); anything else takes a JSON round trip.
func Decode[T any](v any) (T, error) {
	if t, ok := v.(T); ok {
		return t, nil
	}
	var out T
	b, err := json.Marshal(v)
	if err != nil {
		return out, fmt.Errorf("store: encode %T: %w", v, err)
	}
	if err := json.Unmarshal(b, &out); err != nil {
		return out, fmt.Errorf("store: decode into %T: %w", out, err)
	}
	return out, nil
}

// GetAs reads key and decodes it into T. found is true whenever the key
// exists, so a non-nil error with found set means the value is present but
// cannot be decoded as T.
func GetAs[T any](s *Store, key string) (t T, found bool, err error) {
	v, ok := s.Get(key)
	if !ok {
		return t, false, nil
	}
	t, err = Decode[T](v)
	return t, true, err
}

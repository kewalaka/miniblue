package store

// KV is a single key-value pair returned by Backend.Scan and Store.Scan.
type KV struct {
	Key   string
	Value []byte // JSON
}

// Backend is the byte-KV storage interface. Store owns JSON encoding and
// decoding; every backend stores raw JSON bytes.
type Backend interface {
	Get(key string) ([]byte, bool, error)
	Set(key string, v []byte) error
	SetNX(key string, v []byte) (bool, error) // atomic; Postgres: ON CONFLICT DO NOTHING
	Delete(key string) (bool, error)
	Scan(prefix string) ([]KV, error) // sorted by key
	DeleteByPrefix(prefix string) (int, error)
	Reset() error
}

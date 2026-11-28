package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"strings"

	_ "github.com/lib/pq"
)

// PostgresBackend implements Backend using a PostgreSQL database.
type PostgresBackend struct {
	db *sql.DB
}

var likeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

// likePrefix returns a LIKE pattern matching keys that start with prefix,
// with the wildcards and the escape character in prefix taken literally.
func likePrefix(prefix string) string {
	return likeEscaper.Replace(prefix) + "%"
}

// NewPostgresBackend connects to Postgres and auto-creates the storage table.
func NewPostgresBackend(databaseURL string) (*PostgresBackend, error) {
	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, err
	}

	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS miniblue_store (
		key TEXT PRIMARY KEY,
		value JSONB,
		created_at TIMESTAMPTZ DEFAULT NOW()
	)`)
	if err != nil {
		db.Close()
		return nil, err
	}

	// The primary key btree cannot serve LIKE 'x%' under non-C collations.
	_, err = db.Exec(`CREATE INDEX IF NOT EXISTS miniblue_store_key_prefix
		ON miniblue_store (key text_pattern_ops)`)
	if err != nil {
		db.Close()
		return nil, err
	}

	log.Println("Connected to PostgreSQL backend")
	return &PostgresBackend{db: db}, nil
}

func (p *PostgresBackend) Get(key string) ([]byte, bool, error) {
	var data []byte
	err := p.db.QueryRow(`SELECT value FROM miniblue_store WHERE key = $1`, key).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return data, true, nil
}

func (p *PostgresBackend) Set(key string, v []byte) error {
	_, err := p.db.Exec(
		`INSERT INTO miniblue_store (key, value) VALUES ($1, $2)
		 ON CONFLICT (key) DO UPDATE SET value = $2`,
		key, v,
	)
	return err
}

func (p *PostgresBackend) SetNX(key string, v []byte) (bool, error) {
	result, err := p.db.Exec(
		`INSERT INTO miniblue_store (key, value) VALUES ($1, $2)
		 ON CONFLICT (key) DO NOTHING`,
		key, v,
	)
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	return n > 0, err
}

func (p *PostgresBackend) Delete(key string) (bool, error) {
	result, err := p.db.Exec(`DELETE FROM miniblue_store WHERE key = $1`, key)
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	return n > 0, err
}

func (p *PostgresBackend) Scan(prefix string) ([]KV, error) {
	rows, err := p.db.Query(
		`SELECT key, value FROM miniblue_store WHERE key LIKE $1 ORDER BY key COLLATE "C"`,
		likePrefix(prefix),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	results := make([]KV, 0)
	for rows.Next() {
		var kv KV
		if err := rows.Scan(&kv.Key, &kv.Value); err != nil {
			return nil, err
		}
		results = append(results, kv)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return results, nil
}

func (p *PostgresBackend) DeleteByPrefix(prefix string) (int, error) {
	result, err := p.db.Exec(
		`DELETE FROM miniblue_store WHERE key LIKE $1`,
		likePrefix(prefix),
	)
	if err != nil {
		return 0, err
	}
	n, err := result.RowsAffected()
	return int(n), err
}

// Reset drops all state except the schema stamp. It is one statement, so the
// stamp is never absent.
func (p *PostgresBackend) Reset() error {
	_, err := p.db.Exec(`DELETE FROM miniblue_store WHERE key <> $1`, schemaKey)
	return err
}

// readSchema implements schemaReader, reporting query and decode failures
// instead of folding them into "absent" as Get and List do.
func (p *PostgresBackend) readSchema() (stamp interface{}, stamped, hasState bool, err error) {
	var data []byte
	err = p.db.QueryRow(`SELECT value FROM miniblue_store WHERE key = $1`, schemaKey).Scan(&data)
	switch {
	case err == nil:
		if err := json.Unmarshal(data, &stamp); err != nil {
			return nil, false, false, err
		}
		return stamp, true, true, nil
	case !errors.Is(err, sql.ErrNoRows):
		return nil, false, false, err
	}
	err = p.db.QueryRow(`SELECT EXISTS(SELECT 1 FROM miniblue_store)`).Scan(&hasState)
	return nil, false, hasState, err
}

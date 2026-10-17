package store

import (
	"fmt"
	"os"
	"testing"
	"time"
)

func TestLikePrefix(t *testing.T) {
	tests := map[string]string{
		"":         "%",
		"plain":    "plain%",
		"a_b":      `a\_b%`,
		"100%":     `100\%%`,
		`c\d`:      `c\\d%`,
		`_%\`:      `\_\%\\%`,
		"sub/rg/x": "sub/rg/x%",
	}
	for prefix, want := range tests {
		if got := likePrefix(prefix); got != want {
			t.Errorf("likePrefix(%q) = %q, want %q", prefix, got, want)
		}
	}
}

// testPostgres connects to the database named by MINIBLUE_TEST_POSTGRES_URL
// and skips when it is not set. DATABASE_URL is deliberately not used, so a
// developer's own database is never touched.
func testPostgres(t *testing.T) *PostgresBackend {
	t.Helper()
	url := os.Getenv("MINIBLUE_TEST_POSTGRES_URL")
	if url == "" {
		t.Skip("MINIBLUE_TEST_POSTGRES_URL not set")
	}
	pg, err := NewPostgresBackend(url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { pg.db.Close() })
	return pg
}

func TestPostgresPrefixIndex(t *testing.T) {
	pg := testPostgres(t)

	var n int
	err := pg.db.QueryRow(
		`SELECT COUNT(*) FROM pg_indexes
		 WHERE tablename = 'miniblue_store' AND indexname = 'miniblue_store_key_prefix'`,
	).Scan(&n)
	if err != nil || n != 1 {
		t.Fatalf("prefix index: count = %d, err = %v", n, err)
	}
}

func TestPostgresPrefixEscaping(t *testing.T) {
	pg := testPostgres(t)

	// A unique root keeps the test clear of other data in a shared database.
	root := fmt.Sprintf("test-like-%d/", time.Now().UnixNano())
	keys := []string{
		root + "a_b/1", root + "axb/1",
		root + "100%/1", root + "1000/1",
		root + `c\d/1`, root + "cd/1",
	}
	t.Cleanup(func() {
		for _, k := range keys {
			pg.Delete(k)
		}
	})
	for _, k := range keys {
		if err := pg.Set(k, map[string]string{"key": k}); err != nil {
			t.Fatalf("Set(%q): %v", k, err)
		}
	}

	for _, name := range []string{"a_b", "100%", `c\d`} {
		prefix := root + name
		if n := pg.CountByPrefix(prefix); n != 1 {
			t.Errorf("CountByPrefix(%q) = %d, want 1", prefix, n)
		}
		if got := pg.ListByPrefix(prefix); len(got) != 1 {
			t.Errorf("ListByPrefix(%q) returned %d values, want 1", prefix, len(got))
		}
	}

	for _, name := range []string{"a_b", "100%", `c\d`} {
		prefix := root + name
		if n := pg.DeleteByPrefix(prefix); n != 1 {
			t.Errorf("DeleteByPrefix(%q) = %d, want 1", prefix, n)
		}
	}
	if n := pg.CountByPrefix(root); n != 3 {
		t.Errorf("%d keys left under the root, want the 3 look-alikes", n)
	}
}

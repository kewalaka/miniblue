package dbpostgres

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/moabukar/miniblue/internal/store"
	"github.com/moabukar/miniblue/internal/store/storetest"
)

func do(s *store.Store, method, path string) *httptest.ResponseRecorder {
	r := chi.NewRouter()
	NewHandler(s).Register(r)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(method, path, strings.NewReader("{}")))
	return w
}

// After a restart the store returns decoded maps, not Database structs, so the
// real databases must still be found and dropped when the server is deleted.
func TestDeleteServerDropsDatabasesAfterRestart(t *testing.T) {
	s, restart := storetest.NewFile(t)
	const server = "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.DBforPostgreSQL/flexibleServers/srv1"
	for _, p := range []string{server, server + "/databases/db1"} {
		if w := do(s, http.MethodPut, p); w.Code != http.StatusCreated {
			t.Fatalf("PUT %s: %d %s", p, w.Code, w.Body)
		}
	}

	s = restart()
	t.Setenv("POSTGRES_URL", "postgres://admin@localhost:5432/postgres")
	var stmts []string
	orig := execDDL
	execDDL = func(stmt string) error { stmts = append(stmts, stmt); return nil }
	t.Cleanup(func() { execDDL = orig })

	if w := do(s, http.MethodDelete, server); w.Code != http.StatusAccepted {
		t.Fatalf("delete: %d %s", w.Code, w.Body)
	}
	if want := `DROP DATABASE IF EXISTS "db1"`; len(stmts) != 1 || stmts[0] != want {
		t.Fatalf("ddl = %q, want [%s]", stmts, want)
	}
}

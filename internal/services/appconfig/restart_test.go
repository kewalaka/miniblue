package appconfig

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/moabukar/miniblue/internal/store"
	"github.com/moabukar/miniblue/internal/store/storetest"
)

func do(s *store.Store, method, path, body string) *httptest.ResponseRecorder {
	r := chi.NewRouter()
	NewHandler(s).Register(r)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(method, path, strings.NewReader(body)))
	return w
}

// After a restart the store returns decoded maps, not KeyValue structs.
func TestGetKeyValueAfterRestart(t *testing.T) {
	s, restart := storetest.NewFile(t)
	const path = "/appconfig/store1/kv/my-key"
	if w := do(s, http.MethodPut, path, `{"value":"v1"}`); w.Code != http.StatusOK {
		t.Fatalf("set: %d %s", w.Code, w.Body)
	}

	s = restart()
	w := do(s, http.MethodGet, path, "")
	if w.Code != http.StatusOK {
		t.Fatalf("get: %d %s", w.Code, w.Body)
	}
	if got := w.Header().Get("ETag"); got != "etag-my-key" {
		t.Errorf("ETag = %q, want etag-my-key", got)
	}
	if !strings.Contains(w.Body.String(), `"value":"v1"`) {
		t.Errorf("body = %s", w.Body)
	}
}

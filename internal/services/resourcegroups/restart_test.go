package resourcegroups

import (
	"encoding/json"
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

// After a restart the store returns decoded maps, not ResourceGroup structs.
func TestUpdateAfterRestart(t *testing.T) {
	s, restart := storetest.NewFile(t)
	const path = "/subscriptions/sub1/resourcegroups/rg1"
	if w := do(s, http.MethodPut, path, `{"location":"eastus","tags":{"a":"1"}}`); w.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", w.Code, w.Body)
	}

	s = restart()
	w := do(s, http.MethodPatch, path, `{"tags":{"b":"2"}}`)
	if w.Code != http.StatusOK {
		t.Fatalf("patch: %d %s", w.Code, w.Body)
	}
	var rg ResourceGroup
	if err := json.Unmarshal(w.Body.Bytes(), &rg); err != nil {
		t.Fatal(err)
	}
	if rg.Location != "eastus" || rg.Tags["b"] != "2" || rg.Properties.ProvisioningState != "Succeeded" {
		t.Fatalf("unexpected patch result: %+v", rg)
	}

	s = restart()
	if w := do(s, http.MethodGet, path, ""); !strings.Contains(w.Body.String(), `"b":"2"`) {
		t.Fatalf("patch not persisted: %s", w.Body)
	}
}

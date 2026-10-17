package acr

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

// After a restart the store returns decoded maps, not Registry structs.
func TestCheckNameAvailabilityAfterRestart(t *testing.T) {
	s, restart := storetest.NewFile(t)
	const reg = "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.ContainerRegistry/registries/taken"
	if w := do(s, http.MethodPut, reg, `{"location":"eastus"}`); w.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", w.Code, w.Body)
	}

	s = restart()
	const check = "/subscriptions/sub1/providers/Microsoft.ContainerRegistry/checkNameAvailability"
	for name, want := range map[string]bool{"taken": false, "free": true} {
		w := do(s, http.MethodPost, check, `{"name":"`+name+`","type":"Microsoft.ContainerRegistry/registries"}`)
		var got struct {
			NameAvailable bool `json:"nameAvailable"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil || w.Code != http.StatusOK {
			t.Fatalf("%s: %d %s", name, w.Code, w.Body)
		}
		if got.NameAvailable != want {
			t.Errorf("%s: nameAvailable = %v, want %v", name, got.NameAvailable, want)
		}
	}
}

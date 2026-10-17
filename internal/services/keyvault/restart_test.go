package keyvault

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

// After a restart the store returns decoded maps, not Secret structs.
func TestSecretsAfterRestart(t *testing.T) {
	s, restart := storetest.NewFile(t)
	const secrets = "/keyvault/v1/secrets"
	if w := do(s, http.MethodPut, secrets+"/s1", `{"value":"hunter2","tags":{"k":"v"}}`); w.Code != http.StatusOK {
		t.Fatalf("set: %d %s", w.Code, w.Body)
	}

	s = restart()
	t.Run("list", func(t *testing.T) {
		w := do(s, http.MethodGet, secrets, "")
		if w.Code != http.StatusOK {
			t.Fatalf("list: %d %s", w.Code, w.Body)
		}
		var got struct {
			Value []map[string]interface{} `json:"value"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		if len(got.Value) != 1 || !strings.Contains(got.Value[0]["id"].(string), "/secrets/s1/") {
			t.Fatalf("unexpected list: %s", w.Body)
		}
		if strings.Contains(w.Body.String(), "hunter2") {
			t.Fatalf("list leaked the secret value: %s", w.Body)
		}
	})

	t.Run("delete", func(t *testing.T) {
		w := do(s, http.MethodDelete, secrets+"/s1", "")
		if w.Code != http.StatusOK {
			t.Fatalf("delete: %d %s", w.Code, w.Body)
		}
		var got struct {
			Value string            `json:"value"`
			Tags  map[string]string `json:"tags"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		if got.Value != "hunter2" || got.Tags["k"] != "v" {
			t.Fatalf("deleted secret lost its data: %s", w.Body)
		}
	})
}

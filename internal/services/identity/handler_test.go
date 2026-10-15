package identity

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/moabukar/miniblue/internal/store"
)

func newTestServer(t *testing.T) http.Handler {
	t.Helper()
	s := store.New()
	s.Set("rg:sub1:rg1", map[string]interface{}{"name": "rg1"})
	r := chi.NewRouter()
	NewHandler(s).Register(r)
	return r
}

func do(t *testing.T, h http.Handler, method, path string, body interface{}) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

func decodeMap(t *testing.T, rr *httptest.ResponseRecorder) map[string]interface{} {
	t.Helper()
	var got map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return got
}

// Store.Get hands out the stored map and handlers encode it without a lock,
// so PATCH must publish a new record rather than mutate the one readers hold.
func TestUpdateIdentityDoesNotMutatePublishedRecord(t *testing.T) {
	s := store.New()
	s.Set("rg:sub1:rg1", map[string]interface{}{"name": "rg1"})
	r := chi.NewRouter()
	NewHandler(s).Register(r)
	base := "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.ManagedIdentity/userAssignedIdentities/id1"

	do(t, r, "PUT", base, map[string]interface{}{"location": "eastus", "tags": map[string]interface{}{"env": "test"}})
	v, _ := s.Get(identityKey("sub1", "rg1", "id1"))
	published := v.(map[string]interface{})

	patch := do(t, r, "PATCH", base, map[string]interface{}{"tags": map[string]interface{}{"env": "patched"}})
	if patch.Code != http.StatusOK {
		t.Fatalf("PATCH: want 200, got %d: %s", patch.Code, patch.Body.String())
	}

	if tags := published["tags"].(map[string]interface{}); tags["env"] != "test" {
		t.Fatalf("PATCH mutated the previously published record in place: tags=%#v", tags)
	}
	got := decodeMap(t, do(t, r, "GET", base, nil))
	if tags := got["tags"].(map[string]interface{}); tags["env"] != "patched" {
		t.Fatalf("tags not patched: %#v", tags)
	}
}

func TestUserAssignedIdentityLifecycleAndPatchTags(t *testing.T) {
	h := newTestServer(t)
	base := "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.ManagedIdentity/userAssignedIdentities/id1"

	create := do(t, h, "PUT", base, map[string]interface{}{
		"location": "westeurope",
		"tags": map[string]interface{}{
			"env": "test",
		},
	})
	if create.Code != http.StatusCreated {
		t.Fatalf("PUT: want 201, got %d: %s", create.Code, create.Body.String())
	}
	created := decodeMap(t, create)
	if created["id"] != base || created["name"] != "id1" || created["type"] != "Microsoft.ManagedIdentity/userAssignedIdentities" {
		t.Fatalf("unexpected identity response: %#v", created)
	}
	if created["location"] != "westeurope" {
		t.Fatalf("location not preserved: %#v", created)
	}
	props := created["properties"].(map[string]interface{})
	for _, key := range []string{"principalId", "clientId", "tenantId"} {
		if props[key] == "" {
			t.Fatalf("expected %s in properties: %#v", key, props)
		}
	}

	get := do(t, h, "GET", base, nil)
	if get.Code != http.StatusOK {
		t.Fatalf("GET: want 200, got %d", get.Code)
	}
	got := decodeMap(t, get)
	gotProps := got["properties"].(map[string]interface{})
	if gotProps["principalId"] != props["principalId"] || gotProps["clientId"] != props["clientId"] {
		t.Fatalf("expected deterministic ids to be stable: create=%#v get=%#v", props, gotProps)
	}

	list := do(t, h, "GET", "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.ManagedIdentity/userAssignedIdentities", nil)
	if list.Code != http.StatusOK {
		t.Fatalf("LIST: want 200, got %d", list.Code)
	}
	listBody := decodeMap(t, list)
	if values := listBody["value"].([]interface{}); len(values) != 1 {
		t.Fatalf("expected one listed identity, got %#v", listBody)
	}

	patch := do(t, h, "PATCH", base, map[string]interface{}{
		"tags": map[string]interface{}{
			"env": "patched",
		},
	})
	if patch.Code != http.StatusOK {
		t.Fatalf("PATCH: want 200, got %d: %s", patch.Code, patch.Body.String())
	}
	patched := decodeMap(t, patch)
	tags := patched["tags"].(map[string]interface{})
	if tags["env"] != "patched" {
		t.Fatalf("tags not patched: %#v", tags)
	}
	patchedProps := patched["properties"].(map[string]interface{})
	if patchedProps["principalId"] != props["principalId"] || patchedProps["clientId"] != props["clientId"] {
		t.Fatalf("expected patch to preserve ids: before=%#v after=%#v", props, patchedProps)
	}

	del := do(t, h, "DELETE", base, nil)
	if del.Code != http.StatusNoContent {
		t.Fatalf("DELETE: want 204, got %d", del.Code)
	}
	missing := do(t, h, "GET", base, nil)
	if missing.Code != http.StatusNotFound {
		t.Fatalf("GET after delete: want 404, got %d", missing.Code)
	}
}

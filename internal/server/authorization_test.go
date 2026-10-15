package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A blob whose name merely contains the RBAC provider segment is data-plane
// content, not an authorization request.
func TestBlobNameContainingAuthorizationSegmentIsNotRBAC(t *testing.T) {
	h := New().Handler()
	do := func(method, path, body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(method, path, strings.NewReader(body)))
		return w
	}
	blobPath := "/blob/acct1/c1/providers/Microsoft.Authorization/roleDefinitions"

	if w := do(http.MethodPut, "/blob/acct1/c1?restype=container", ""); w.Code != http.StatusCreated {
		t.Fatalf("create container: %d %s", w.Code, w.Body.String())
	}
	if w := do(http.MethodPut, blobPath, "blob-contents"); w.Code != http.StatusCreated {
		t.Fatalf("upload blob: %d %s", w.Code, w.Body.String())
	}
	w := do(http.MethodGet, blobPath, "")
	if w.Code != http.StatusOK || w.Body.String() != "blob-contents" {
		t.Fatalf("download blob: %d %q", w.Code, w.Body.String())
	}
}

func TestStorageScopeRoleDefinitionsWithPreviewAPIVersion(t *testing.T) {
	srv := New()
	path := "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Storage/storageAccounts/acct1/providers/Microsoft.Authorization/roleDefinitions?api-version=2022-05-01-preview"
	req := httptest.NewRequest(http.MethodGet, path, nil)
	w := httptest.NewRecorder()

	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected role definitions at storage scope to return 200, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "Storage Blob Data Contributor") {
		t.Fatalf("expected storage roles in response, got: %s", w.Body.String())
	}
}

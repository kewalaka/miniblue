package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

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

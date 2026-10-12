package storageaccounts_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/moabukar/miniblue/internal/server"
)

// TestDeleteStorageAccountIsSynchronous verifies the Delete Storage Account
// operation behaves like the real Azure Storage RP: 200 OK when the account
// existed, 204 No Content when it did not. A bare 202 Accepted with no body or
// poll URL makes the azurerm provider / Go SDK fail with
// "unexpected status 202 received with no body".
func TestDeleteStorageAccountIsSynchronous(t *testing.T) {
	ts := httptest.NewServer(server.New().Handler())
	defer ts.Close()

	base := ts.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Storage/storageAccounts/acct1"
	putJSON(t, base, map[string]interface{}{"location": "eastus", "kind": "StorageV2"})

	// First delete: account exists, expect 200 OK.
	if status := doDelete(t, base); status != http.StatusOK {
		t.Fatalf("DELETE existing account status = %d, want %d", status, http.StatusOK)
	}

	// Account is gone now.
	getJSON(t, base, http.StatusNotFound)

	// Second delete: account no longer exists, expect 204 No Content.
	if status := doDelete(t, base); status != http.StatusNoContent {
		t.Fatalf("DELETE missing account status = %d, want %d", status, http.StatusNoContent)
	}
}

func doDelete(t *testing.T, url string) int {
	t.Helper()
	req, err := http.NewRequest(http.MethodDelete, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	return resp.StatusCode
}

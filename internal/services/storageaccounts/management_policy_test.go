package storageaccounts_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/moabukar/miniblue/internal/server"
)

func TestManagementPolicyPutGetDeleteEchoesPolicy(t *testing.T) {
	ts := httptest.NewServer(server.New().Handler())
	defer ts.Close()

	base := ts.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Storage/storageAccounts/acct1"
	putJSON(t, base, map[string]interface{}{"location": "eastus", "kind": "StorageV2"})

	policy := map[string]interface{}{
		"properties": map[string]interface{}{
			"policy": map[string]interface{}{
				"enabled": true,
				"rules": []interface{}{
					map[string]interface{}{
						"name":    "delete-old-blobs",
						"enabled": true,
						"type":    "Lifecycle",
						"definition": map[string]interface{}{
							"actions": map[string]interface{}{
								"baseBlob": map[string]interface{}{
									"delete": map[string]interface{}{
										"daysAfterModificationGreaterThan": float64(7),
									},
								},
							},
							"filters": map[string]interface{}{
								"blobTypes":   []interface{}{"blockBlob"},
								"prefixMatch": []interface{}{"demo/"},
							},
						},
					},
				},
			},
		},
	}

	policyURL := base + "/managementPolicies/default"
	created := putJSON(t, policyURL, policy)
	assertManagementPolicy(t, created, policy)

	got := getJSON(t, policyURL, http.StatusOK)
	assertManagementPolicy(t, got, policy)

	req, err := http.NewRequest(http.MethodDelete, policyURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("DELETE status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	getJSON(t, policyURL, http.StatusNotFound)
}

func putJSON(t *testing.T, url string, body map[string]interface{}) map[string]interface{} {
	t.Helper()
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPut, url, bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PUT %s status = %d, want %d", url, resp.StatusCode, http.StatusOK)
	}
	var out map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out
}

func getJSON(t *testing.T, url string, wantStatus int) map[string]interface{} {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != wantStatus {
		t.Fatalf("GET %s status = %d, want %d", url, resp.StatusCode, wantStatus)
	}
	var out map[string]interface{}
	if wantStatus == http.StatusOK {
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			t.Fatal(err)
		}
	}
	return out
}

func assertManagementPolicy(t *testing.T, got, want map[string]interface{}) {
	t.Helper()
	if got["id"] != "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Storage/storageAccounts/acct1/managementPolicies/default" {
		t.Fatalf("unexpected id: %v", got["id"])
	}
	if got["name"] != "default" {
		t.Fatalf("unexpected name: %v", got["name"])
	}
	if got["type"] != "Microsoft.Storage/storageAccounts/managementPolicies" {
		t.Fatalf("unexpected type: %v", got["type"])
	}
	if !reflect.DeepEqual(got["properties"], want["properties"]) {
		t.Fatalf("properties = %#v, want %#v", got["properties"], want["properties"])
	}
}

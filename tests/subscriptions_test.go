package tests

import (
	"testing"
)

func TestSubscriptionsAndTenants(t *testing.T) {
	ts := setupServer()
	defer ts.Close()

	resp := doRequest(t, "GET", ts.URL+"/subscriptions?api-version=2022-12-01", "")
	defer resp.Body.Close()
	expectStatus(t, resp, 200)

	m := decodeJSON(t, resp)
	subs := m["value"].([]interface{})
	if len(subs) == 0 {
		t.Fatal("expected at least 1 subscription")
	}

	resp = doRequest(t, "GET", ts.URL+"/tenants?api-version=2022-12-01", "")
	m = decodeJSON(t, resp)
	tenants := m["value"].([]interface{})
	if len(tenants) == 0 {
		t.Fatal("expected at least 1 tenant")
	}
}

func TestKeyVaultProviderMetadataIncludesVaults(t *testing.T) {
	ts := setupServer()
	defer ts.Close()

	const sub = "00000000-0000-0000-0000-000000000000"
	resp := doRequest(t, "GET", ts.URL+"/subscriptions/"+sub+"/providers/Microsoft.KeyVault?api-version=2022-12-01", "")
	defer resp.Body.Close()
	expectStatus(t, resp, 200)

	provider := decodeJSON(t, resp)
	for _, resourceType := range provider["resourceTypes"].([]interface{}) {
		rt := resourceType.(map[string]interface{})
		if rt["resourceType"] != "vaults" {
			continue
		}
		for _, apiVersion := range rt["apiVersions"].([]interface{}) {
			if apiVersion == "2023-02-01" {
				return
			}
		}
		t.Fatalf("expected Key Vault vaults to include api-version 2023-02-01, got %v", rt["apiVersions"])
	}
	t.Fatalf("expected Microsoft.KeyVault provider metadata to include vaults, got %v", provider["resourceTypes"])
}

func TestDocumentDBProviderMetadataIncludesCosmosResourceTypes(t *testing.T) {
	ts := setupServer()
	defer ts.Close()

	const sub = "00000000-0000-0000-0000-000000000000"
	resp := doRequest(t, "GET", ts.URL+"/subscriptions/"+sub+"/providers/Microsoft.DocumentDB?api-version=2022-12-01", "")
	defer resp.Body.Close()
	expectStatus(t, resp, 200)

	provider := decodeJSON(t, resp)
	want := map[string]bool{
		"databaseAccounts":              false,
		"databaseAccounts/tables":       false,
		"databaseAccounts/sqlDatabases": false,
	}
	for _, resourceType := range provider["resourceTypes"].([]interface{}) {
		rt := resourceType.(map[string]interface{})
		name, _ := rt["resourceType"].(string)
		if _, ok := want[name]; ok {
			want[name] = true
		}
	}
	for name, found := range want {
		if !found {
			t.Fatalf("expected Microsoft.DocumentDB provider metadata to include %s, got %v", name, provider["resourceTypes"])
		}
	}
}

func TestManagedIdentityToken(t *testing.T) {
	ts := setupServer()
	defer ts.Close()

	resp := doRequest(t, "GET", ts.URL+"/metadata/identity/oauth2/token?resource=https://management.azure.com/", "")
	defer resp.Body.Close()
	expectStatus(t, resp, 200)

	m := decodeJSON(t, resp)
	if m["token_type"] != "Bearer" {
		t.Fatalf("expected token_type=Bearer, got %v", m["token_type"])
	}
	if m["access_token"] == nil || m["access_token"] == "" {
		t.Fatal("expected non-empty access_token")
	}
}

func TestMetadataEndpoints(t *testing.T) {
	ts := setupServer()
	defer ts.Close()

	resp := doRequest(t, "GET", ts.URL+"/metadata/endpoints", "")
	defer resp.Body.Close()
	expectStatus(t, resp, 200)
}

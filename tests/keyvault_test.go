package tests

import (
	"strings"
	"testing"
)

func TestKeyVaultCRUD(t *testing.T) {
	ts := setupServer()
	defer ts.Close()
	base := ts.URL + "/keyvault/myvault/secrets"

	// Set secret
	resp := doRequest(t, "PUT", base+"/db-pass", `{"value":"supersecret"}`)
	defer resp.Body.Close()
	expectStatus(t, resp, 200)

	m := decodeJSON(t, resp)
	if m["value"] != "supersecret" {
		t.Fatalf("expected value=supersecret, got %v", m["value"])
	}

	// Get
	resp = doRequest(t, "GET", base+"/db-pass", "")
	m = decodeJSON(t, resp)
	if m["value"] != "supersecret" {
		t.Fatalf("get: expected value=supersecret, got %v", m["value"])
	}

	// List
	resp = doRequest(t, "GET", base, "")
	list := decodeJSON(t, resp)
	if list["value"] == nil {
		t.Fatal("expected value array in list response")
	}

	// Delete
	resp = doRequest(t, "DELETE", base+"/db-pass", "")
	resp.Body.Close()

	// Should 404 now
	resp = doRequest(t, "GET", base+"/db-pass", "")
	defer resp.Body.Close()
	expectStatus(t, resp, 404)
}

func TestKeyVaultDataPlaneVaultAzureNetHostCRUD(t *testing.T) {
	ts := setupServer()
	defer ts.Close()
	host := "myvault.vault.azure.net"
	base := ts.URL + "/secrets/db-pass"

	resp := doAppConfigHostRequest(t, "PUT", base+"?api-version=7.4", host, `{"value":"supersecret","contentType":"text/plain","tags":{"env":"test"}}`)
	defer resp.Body.Close()
	expectStatus(t, resp, 200)

	m := decodeJSON(t, resp)
	if m["value"] != "supersecret" || m["contentType"] != "text/plain" {
		t.Fatalf("expected secret value/contentType round trip, got %v", m)
	}
	id, _ := m["id"].(string)
	if !strings.HasPrefix(id, "https://myvault.vault.azure.net/secrets/db-pass/") {
		t.Fatalf("expected versioned vault.azure.net id, got %q", id)
	}
	version := id[strings.LastIndex(id, "/")+1:]

	respTrailingSlash := doAppConfigHostRequest(t, "GET", base+"/?api-version=7.4", host, "")
	defer respTrailingSlash.Body.Close()
	expectStatus(t, respTrailingSlash, 200)
	m = decodeJSON(t, respTrailingSlash)
	if m["value"] != "supersecret" {
		t.Fatalf("expected trailing-slash get value=supersecret, got %v", m["value"])
	}

	resp2 := doAppConfigHostRequest(t, "GET", base+"/"+version+"?api-version=7.4", host, "")
	defer resp2.Body.Close()
	expectStatus(t, resp2, 200)
	m = decodeJSON(t, resp2)
	if m["value"] != "supersecret" {
		t.Fatalf("expected versioned get value=supersecret, got %v", m["value"])
	}

	resp3 := doAppConfigHostRequest(t, "GET", ts.URL+"/secrets?api-version=7.4", host, "")
	defer resp3.Body.Close()
	expectStatus(t, resp3, 200)
	list := decodeJSON(t, resp3)
	items := list["value"].([]interface{})
	if len(items) != 1 {
		t.Fatalf("expected one listed secret, got %d", len(items))
	}
	item := items[0].(map[string]interface{})
	if _, ok := item["value"]; ok {
		t.Fatalf("list response should redact secret value, got %v", item)
	}

	resp4 := doAppConfigHostRequest(t, "DELETE", base+"?api-version=7.4", host, "")
	defer resp4.Body.Close()
	expectStatus(t, resp4, 200)

	resp5 := doAppConfigHostRequest(t, "GET", base+"?api-version=7.4", host, "")
	defer resp5.Body.Close()
	expectStatus(t, resp5, 404)

	resp6 := doAppConfigHostRequest(t, "GET", ts.URL+"/deletedsecrets/db-pass?api-version=7.4", host, "")
	defer resp6.Body.Close()
	expectStatus(t, resp6, 200)
}

func TestKeyVaultARMVaultCRUDAndAccessPolicies(t *testing.T) {
	ts := setupServer()
	defer ts.Close()

	const sub = "00000000-0000-0000-0000-000000000000"
	base := ts.URL + "/subscriptions/" + sub + "/resourceGroups/rg1/providers/Microsoft.KeyVault/vaults/kvtest?api-version=2023-07-01"

	resp := doRequest(t, "PUT", base, `{
		"location":"eastus",
		"tags":{"env":"test"},
		"properties":{
			"tenantId":"00000000-0000-0000-0000-000000000000",
			"sku":{"family":"A","name":"standard"},
			"enableRbacAuthorization":true
		}
	}`)
	defer resp.Body.Close()
	expectStatus(t, resp, 201)
	if resp.Header.Get("Azure-AsyncOperation") == "" {
		t.Fatal("expected Azure-AsyncOperation header")
	}
	vault := decodeJSON(t, resp)
	if vault["id"] != "/subscriptions/"+sub+"/resourceGroups/rg1/providers/Microsoft.KeyVault/vaults/kvtest" {
		t.Fatalf("unexpected id: %v", vault["id"])
	}
	if _, ok := vault["systemData"].(map[string]interface{}); !ok {
		t.Fatalf("expected systemData in create response, got %v", vault["systemData"])
	}
	props := vault["properties"].(map[string]interface{})
	if props["vaultUri"] != "https://kvtest.vault.azure.net/" || props["provisioningState"] != "Succeeded" {
		t.Fatalf("unexpected properties: %v", props)
	}

	resp = doRequest(t, "GET", base, "")
	defer resp.Body.Close()
	expectStatus(t, resp, 200)
	vault = decodeJSON(t, resp)
	if _, ok := vault["systemData"].(map[string]interface{}); !ok {
		t.Fatalf("expected systemData in get response, got %v", vault["systemData"])
	}

	resp = doRequest(t, "PATCH", base, `{"tags":{"env":"prod"},"properties":{"enabledForDeployment":true}}`)
	defer resp.Body.Close()
	expectStatus(t, resp, 200)
	vault = decodeJSON(t, resp)
	if vault["tags"].(map[string]interface{})["env"] != "prod" {
		t.Fatalf("patch did not update tags: %v", vault["tags"])
	}

	policyBody := `{"properties":{"accessPolicies":[{"tenantId":"00000000-0000-0000-0000-000000000000","objectId":"11111111-1111-1111-1111-111111111111","permissions":{"secrets":["get","list"]}}]}}`
	resp = doRequest(t, "PUT", strings.Replace(base, "?api-version=2023-07-01", "/accessPolicies/add?api-version=2023-07-01", 1), policyBody)
	defer resp.Body.Close()
	expectStatus(t, resp, 200)
	vault = decodeJSON(t, resp)
	props = vault["properties"].(map[string]interface{})
	if len(props["accessPolicies"].([]interface{})) != 1 {
		t.Fatalf("expected one access policy, got %v", props["accessPolicies"])
	}

	resp = doRequest(t, "GET", ts.URL+"/subscriptions/"+sub+"/resourceGroups/rg1/providers/Microsoft.KeyVault/vaults?api-version=2023-07-01", "")
	defer resp.Body.Close()
	expectStatus(t, resp, 200)
	list := decodeJSON(t, resp)
	if len(list["value"].([]interface{})) != 1 {
		t.Fatalf("expected one RG vault, got %v", list)
	}

	resp = doRequest(t, "GET", ts.URL+"/subscriptions/"+sub+"/providers/Microsoft.KeyVault/vaults?api-version=2023-07-01", "")
	defer resp.Body.Close()
	expectStatus(t, resp, 200)
	list = decodeJSON(t, resp)
	if len(list["value"].([]interface{})) != 1 {
		t.Fatalf("expected one subscription vault, got %v", list)
	}

	resp = doRequest(t, "POST", ts.URL+"/subscriptions/"+sub+"/providers/Microsoft.KeyVault/checkNameAvailability?api-version=2023-07-01", `{"name":"kvtest","type":"Microsoft.KeyVault/vaults"}`)
	defer resp.Body.Close()
	expectStatus(t, resp, 200)
	availability := decodeJSON(t, resp)
	if availability["nameAvailable"] != false || availability["reason"] != "AlreadyExists" {
		t.Fatalf("expected unavailable name, got %v", availability)
	}

	resp = doRequest(t, "POST", ts.URL+"/subscriptions/"+sub+"/providers/Microsoft.KeyVault/checkNameAvailability?api-version=2023-07-01", `{"name":"ab","type":"Microsoft.KeyVault/vaults"}`)
	defer resp.Body.Close()
	expectStatus(t, resp, 200)
	availability = decodeJSON(t, resp)
	if availability["nameAvailable"] != false || availability["reason"] != "AccountNameInvalid" {
		t.Fatalf("expected invalid name reason, got %v", availability)
	}

	resp = doRequest(t, "DELETE", base, "")
	defer resp.Body.Close()
	expectStatus(t, resp, 202)

	resp = doRequest(t, "GET", base, "")
	defer resp.Body.Close()
	expectStatus(t, resp, 404)

	resp = doRequest(t, "GET", ts.URL+"/subscriptions/"+sub+"/providers/Microsoft.KeyVault/locations/eastus/deletedVaults/kvtest?api-version=2023-07-01", "")
	defer resp.Body.Close()
	expectStatus(t, resp, 200)
}

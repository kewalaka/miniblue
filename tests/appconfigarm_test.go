package tests

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const appConfigSub = "sub1"
const appConfigRG = "rg1"
const appConfigStore = "myappconfig"

func appConfigARMBase(ts *httptest.Server) string {
	return ts.URL + "/subscriptions/" + appConfigSub + "/resourceGroups/" + appConfigRG + "/providers/Microsoft.AppConfiguration/configurationStores"
}

func doAppConfigHostRequest(t *testing.T, method, url, host, body string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	if body != "" {
		req = mustRequestWithBody(t, method, url, body)
	}
	req.Host = host
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func mustRequestWithBody(t *testing.T, method, url, body string) *http.Request {
	t.Helper()
	req, err := http.NewRequest(method, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	return req
}

func TestAppConfigARMStoreCRUD(t *testing.T) {
	ts := setupServer()
	defer ts.Close()
	base := appConfigARMBase(ts) + "/" + appConfigStore

	// Create store
	resp := doRequest(t, "PUT", base, `{"location":"eastus","sku":{"name":"Standard"}}`)
	defer resp.Body.Close()
	expectStatus(t, resp, 201)

	m := decodeJSON(t, resp)
	if m["name"] != appConfigStore {
		t.Fatalf("expected name=%s, got %v", appConfigStore, m["name"])
	}
	if m["type"] != "Microsoft.AppConfiguration/configurationStores" {
		t.Fatalf("expected correct type, got %v", m["type"])
	}
	props := m["properties"].(map[string]interface{})
	if props["provisioningState"] != "Succeeded" {
		t.Fatalf("expected provisioningState=Succeeded, got %v", props["provisioningState"])
	}
	if props["endpoint"] == nil {
		t.Fatal("expected endpoint in properties")
	}

	// Get store
	resp2 := doRequest(t, "GET", base, "")
	defer resp2.Body.Close()
	expectStatus(t, resp2, 200)

	// Update store (idempotent)
	resp3 := doRequest(t, "PUT", base, `{"location":"eastus"}`)
	defer resp3.Body.Close()
	expectStatus(t, resp3, 200)
}

func TestAppConfigARMStoreOmitsEmptyEncryption(t *testing.T) {
	ts := setupServer()
	defer ts.Close()
	base := appConfigARMBase(ts) + "/" + appConfigStore

	resp := doRequest(t, "PUT", base, `{"location":"eastus","properties":{"encryption":{}}}`)
	resp.Body.Close()
	expectStatus(t, resp, 201)

	resp = doRequest(t, "GET", base, "")
	defer resp.Body.Close()
	expectStatus(t, resp, 200)

	m := decodeJSON(t, resp)
	props := m["properties"].(map[string]interface{})
	if _, ok := props["encryption"]; ok {
		t.Fatalf("did not expect empty encryption in properties, got %v", props["encryption"])
	}
}

func TestAppConfigARMStoreOmitsEmptyKeyVaultEncryption(t *testing.T) {
	ts := setupServer()
	defer ts.Close()
	base := appConfigARMBase(ts) + "/" + appConfigStore

	resp := doRequest(t, "PUT", base, `{"location":"eastus","properties":{"encryption":{"keyVaultProperties":{}}}}`)
	resp.Body.Close()
	expectStatus(t, resp, 201)

	resp = doRequest(t, "GET", base, "")
	defer resp.Body.Close()
	expectStatus(t, resp, 200)

	m := decodeJSON(t, resp)
	props := m["properties"].(map[string]interface{})
	if _, ok := props["encryption"]; ok {
		t.Fatalf("did not expect empty keyVaultProperties encryption in properties, got %v", props["encryption"])
	}
}

func TestAppConfigARMStoreList(t *testing.T) {
	ts := setupServer()
	defer ts.Close()
	base := appConfigARMBase(ts)

	doRequest(t, "PUT", base+"/store-a", `{}`).Body.Close()
	doRequest(t, "PUT", base+"/store-b", `{}`).Body.Close()

	resp := doRequest(t, "GET", base, "")
	defer resp.Body.Close()
	expectStatus(t, resp, 200)

	m := decodeJSON(t, resp)
	items := m["value"].([]interface{})
	if len(items) < 2 {
		t.Fatalf("expected at least 2 stores, got %d", len(items))
	}
}

func TestAppConfigARMStoreListKeys(t *testing.T) {
	ts := setupServer()
	defer ts.Close()
	base := appConfigARMBase(ts) + "/" + appConfigStore

	doRequest(t, "PUT", base, `{"location":"eastus","sku":{"name":"free"}}`).Body.Close()

	resp := doRequest(t, "POST", base+"/listKeys?api-version=2024-05-01", "")
	defer resp.Body.Close()
	expectStatus(t, resp, 200)

	m := decodeJSON(t, resp)
	items := m["value"].([]interface{})
	if len(items) != 2 {
		t.Fatalf("expected two keys, got %d", len(items))
	}
	primary := items[0].(map[string]interface{})
	if primary["id"] != "primary" || primary["name"] != "Primary" || primary["value"] != "miniblue-primary-key" {
		t.Fatalf("expected primary placeholder key, got %v", primary)
	}
	if !strings.Contains(primary["connectionString"].(string), "Endpoint=https://"+appConfigStore+".azconfig.io") {
		t.Fatalf("expected endpoint in connection string, got %v", primary["connectionString"])
	}
}

func TestAppConfigARMStoreListReplicas(t *testing.T) {
	ts := setupServer()
	defer ts.Close()
	base := appConfigARMBase(ts) + "/" + appConfigStore

	doRequest(t, "PUT", base, `{"location":"eastus","sku":{"name":"free"}}`).Body.Close()

	resp := doRequest(t, "GET", base+"/replicas?api-version=2024-05-01", "")
	defer resp.Body.Close()
	expectStatus(t, resp, 200)

	m := decodeJSON(t, resp)
	items := m["value"].([]interface{})
	if len(items) != 0 {
		t.Fatalf("expected no replicas, got %d", len(items))
	}
}

func TestAppConfigARMStoreNotFound(t *testing.T) {
	ts := setupServer()
	defer ts.Close()
	resp := doRequest(t, "GET", appConfigARMBase(ts)+"/nonexistent", "")
	defer resp.Body.Close()
	expectStatus(t, resp, 404)
}

func TestAppConfigARMStoreDelete(t *testing.T) {
	ts := setupServer()
	defer ts.Close()
	base := appConfigARMBase(ts) + "/store-del"

	doRequest(t, "PUT", base, `{}`).Body.Close()

	resp := doRequest(t, "DELETE", base, "")
	defer resp.Body.Close()
	expectStatus(t, resp, 202)

	resp2 := doRequest(t, "GET", base, "")
	defer resp2.Body.Close()
	expectStatus(t, resp2, 404)
}

func TestAppConfigARMResponseHasID(t *testing.T) {
	ts := setupServer()
	defer ts.Close()
	base := appConfigARMBase(ts) + "/mystore"

	resp := doRequest(t, "PUT", base, `{}`)
	defer resp.Body.Close()
	m := decodeJSON(t, resp)

	expectedID := "/subscriptions/" + appConfigSub + "/resourceGroups/" + appConfigRG + "/providers/Microsoft.AppConfiguration/configurationStores/mystore"
	if m["id"] != expectedID {
		t.Fatalf("expected id=%s, got %v", expectedID, m["id"])
	}
}

func TestAppConfigARMEndpointFormat(t *testing.T) {
	ts := setupServer()
	defer ts.Close()
	storeName := "teststoreendpt"
	base := appConfigARMBase(ts) + "/" + storeName

	resp := doRequest(t, "PUT", base, `{}`)
	defer resp.Body.Close()
	m := decodeJSON(t, resp)
	props := m["properties"].(map[string]interface{})

	expectedEndpoint := "https://" + storeName + ".azconfig.io"
	if props["endpoint"] != expectedEndpoint {
		t.Fatalf("expected endpoint=%s, got %v", expectedEndpoint, props["endpoint"])
	}
}

func TestAppConfigARMDoesNotBreakDataPlane(t *testing.T) {
	ts := setupServer()
	defer ts.Close()
	base := ts.URL + "/appconfig/mystore/kv"

	// Data-plane operations should still work
	resp := doRequest(t, "PUT", base+"/mykey", `{"value":"myvalue"}`)
	defer resp.Body.Close()
	m := decodeJSON(t, resp)
	if m["value"] != "myvalue" {
		t.Fatalf("expected value=myvalue, got %v", m["value"])
	}
}

func TestAppConfigDataPlaneAzConfigHostCRUD(t *testing.T) {
	ts := setupServer()
	defer ts.Close()
	host := appConfigStore + ".azconfig.io"
	base := ts.URL + "/kv/mykey"

	resp := doAppConfigHostRequest(t, "PUT", base, host, `{"value":"myvalue"}`)
	defer resp.Body.Close()
	expectStatus(t, resp, 200)
	m := decodeJSON(t, resp)
	if m["key"] != "mykey" || m["value"] != "myvalue" {
		t.Fatalf("expected key/value round trip, got %v", m)
	}

	resp2 := doAppConfigHostRequest(t, "GET", base, host, "")
	defer resp2.Body.Close()
	expectStatus(t, resp2, 200)
	m = decodeJSON(t, resp2)
	if m["value"] != "myvalue" {
		t.Fatalf("expected value=myvalue, got %v", m["value"])
	}

	resp3 := doAppConfigHostRequest(t, "DELETE", base, host, "")
	defer resp3.Body.Close()
	expectStatus(t, resp3, 204)

	resp4 := doAppConfigHostRequest(t, "GET", base, host, "")
	defer resp4.Body.Close()
	expectStatus(t, resp4, 404)
}

func TestAppConfigDataPlaneAzConfigHostLabelsAndSlashKeys(t *testing.T) {
	ts := setupServer()
	defer ts.Close()
	host := appConfigStore + ".azconfig.io"
	base := ts.URL + "/kv/folder/key"

	resp := doAppConfigHostRequest(t, "PUT", base+"?label=prod", host, `{"value":"prodvalue"}`)
	defer resp.Body.Close()
	expectStatus(t, resp, 200)

	resp2 := doAppConfigHostRequest(t, "PUT", base+"?label=dev", host, `{"value":"devvalue"}`)
	defer resp2.Body.Close()
	expectStatus(t, resp2, 200)

	resp3 := doAppConfigHostRequest(t, "GET", base+"?label=prod", host, "")
	defer resp3.Body.Close()
	expectStatus(t, resp3, 200)
	m := decodeJSON(t, resp3)
	if m["key"] != "folder/key" || m["label"] != "prod" || m["value"] != "prodvalue" {
		t.Fatalf("expected labeled slash key, got %v", m)
	}
}

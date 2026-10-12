package tests

import (
	"strings"
	"testing"
)

// TestARMCaseInsensitive_ResourceGroup covers the exact reproduction from the
// bug report: PUT and GET on /resourceGroups/<name> (camelCase) must return
// the same status as the lowercase path.
func TestARMCaseInsensitive_ResourceGroup(t *testing.T) {
	ts := setupServer()
	defer ts.Close()
	sub := "00000000-0000-0000-0000-000000000000"

	// PUT with canonical camelCase
	url := ts.URL + "/subscriptions/" + sub + "/resourceGroups/test-rg-camel?api-version=2023-07-01"
	resp := doRequest(t, "PUT", url, `{"location":"eastus"}`)
	defer resp.Body.Close()
	expectStatus(t, resp, 201)

	body := decodeJSON(t, resp)
	// Response id must mirror the case the client sent: resourceGroups (camelCase).
	if id, _ := body["id"].(string); !strings.Contains(id, "/resourceGroups/test-rg-camel") {
		t.Fatalf("response id should mirror client's camelCase casing, got %q", id)
	}

	// GET on the same camelCase URL must return 200 with the same casing.
	resp = doRequest(t, "GET", url, "")
	defer resp.Body.Close()
	expectStatus(t, resp, 200)
	body = decodeJSON(t, resp)
	if id, _ := body["id"].(string); !strings.Contains(id, "/resourceGroups/test-rg-camel") {
		t.Fatalf("GET response id casing wrong: %q", id)
	}
}

// TestARMCaseInsensitive_LowercaseStillWorks ensures the legacy lowercase
// path continues to function and that responses for it use lowercase
// `resourcegroups`.
func TestARMCaseInsensitive_LowercaseStillWorks(t *testing.T) {
	ts := setupServer()
	defer ts.Close()
	sub := "00000000-0000-0000-0000-000000000000"

	url := ts.URL + "/subscriptions/" + sub + "/resourcegroups/test-rg-lower?api-version=2023-07-01"
	resp := doRequest(t, "PUT", url, `{"location":"eastus"}`)
	defer resp.Body.Close()
	expectStatus(t, resp, 201)

	body := decodeJSON(t, resp)
	if id, _ := body["id"].(string); !strings.Contains(id, "/resourcegroups/test-rg-lower") {
		t.Fatalf("response id should mirror client's lowercase casing, got %q", id)
	}
}

// TestARMCaseInsensitive_ProvidersList ensures the providers list endpoint
// works regardless of casing.
func TestARMCaseInsensitive_ProvidersList(t *testing.T) {
	ts := setupServer()
	defer ts.Close()
	sub := "00000000-0000-0000-0000-000000000000"

	for _, p := range []string{"providers", "Providers", "PROVIDERS"} {
		url := ts.URL + "/subscriptions/" + sub + "/" + p + "?api-version=2022-09-01"
		resp := doRequest(t, "GET", url, "")
		resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Fatalf("GET %s: expected 200, got %d", url, resp.StatusCode)
		}
	}
}

// TestARMCaseInsensitive_VirtualNetwork covers the full nested-provider path
// pulled from the acceptance criteria.
func TestARMCaseInsensitive_VirtualNetwork(t *testing.T) {
	ts := setupServer()
	defer ts.Close()
	sub := "00000000-0000-0000-0000-000000000000"

	rgURL := ts.URL + "/subscriptions/" + sub + "/resourceGroups/rg1?api-version=2023-07-01"
	resp := doRequest(t, "PUT", rgURL, `{"location":"eastus"}`)
	resp.Body.Close()
	expectStatus(t, resp, 201)

	vnetURL := ts.URL + "/subscriptions/" + sub +
		"/resourceGroups/rg1/providers/Microsoft.Network/virtualNetworks/vnet1?api-version=2023-09-01"
	resp = doRequest(t, "PUT", vnetURL,
		`{"location":"eastus","properties":{"addressSpace":{"addressPrefixes":["10.0.0.0/16"]}}}`)
	defer resp.Body.Close()
	expectStatus(t, resp, 201)

	body := decodeJSON(t, resp)
	id, _ := body["id"].(string)
	// All four literal segments must echo client's casing.
	for _, want := range []string{
		"/resourceGroups/rg1",
		"/providers/Microsoft.Network/",
		"/virtualNetworks/vnet1",
	} {
		if !strings.Contains(id, want) {
			t.Fatalf("response id %q should contain %q", id, want)
		}
	}
}

// TestARMCaseInsensitive_DnsZone — also from the acceptance criteria.
func TestARMCaseInsensitive_DnsZone(t *testing.T) {
	ts := setupServer()
	defer ts.Close()
	sub := "00000000-0000-0000-0000-000000000000"

	rgURL := ts.URL + "/subscriptions/" + sub + "/resourceGroups/rg2?api-version=2023-07-01"
	resp := doRequest(t, "PUT", rgURL, `{"location":"eastus"}`)
	resp.Body.Close()
	expectStatus(t, resp, 201)

	dnsURL := ts.URL + "/subscriptions/" + sub +
		"/resourceGroups/rg2/providers/Microsoft.Network/dnsZones/example.local?api-version=2018-05-01"
	resp = doRequest(t, "PUT", dnsURL, `{"location":"global"}`)
	defer resp.Body.Close()
	expectStatus(t, resp, 201)

	body := decodeJSON(t, resp)
	id, _ := body["id"].(string)
	if !strings.Contains(id, "/dnsZones/example.local") {
		t.Fatalf("response id should mirror /dnsZones/ casing, got %q", id)
	}
}

// TestARMCaseInsensitive_MixedRandomCase exercises a deliberately wonky
// casing mix to prove segment-level case-insensitivity is fully generic.
func TestARMCaseInsensitive_MixedRandomCase(t *testing.T) {
	ts := setupServer()
	defer ts.Close()
	sub := "00000000-0000-0000-0000-000000000000"

	rgURL := ts.URL + "/subscriptions/" + sub + "/RESOURCEGROUPS/rg3?api-version=2023-07-01"
	resp := doRequest(t, "PUT", rgURL, `{"location":"eastus"}`)
	defer resp.Body.Close()
	expectStatus(t, resp, 201)

	body := decodeJSON(t, resp)
	id, _ := body["id"].(string)
	if !strings.Contains(id, "/RESOURCEGROUPS/rg3") {
		t.Fatalf("client used uppercase RESOURCEGROUPS, response should echo it: %q", id)
	}
	// Resource name must be preserved (not lowercased).
	if name, _ := body["name"].(string); name != "rg3" {
		t.Fatalf("resource name should be preserved as-sent, got %q", name)
	}
}

// TestARMNotFound_JSONEnvelope verifies that unmatched ARM routes return the
// standard `{"error":{"code":"InvalidResourceType",...}}` body that
// terraform's azurerm provider relies on.
func TestARMNotFound_JSONEnvelope(t *testing.T) {
	ts := setupServer()
	defer ts.Close()
	sub := "00000000-0000-0000-0000-000000000000"

	url := ts.URL + "/subscriptions/" + sub +
		"/resourceGroups/rg/providers/Microsoft.Bogus/widgets/x?api-version=2024-01-01"
	resp := doRequest(t, "GET", url, "")
	defer resp.Body.Close()
	expectStatus(t, resp, 404)

	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "json") {
		t.Fatalf("expected JSON content-type, got %q", ct)
	}
	e := decodeError(t, resp)
	if e.Error.Code != "InvalidResourceType" {
		t.Fatalf("expected code=InvalidResourceType, got %q", e.Error.Code)
	}
	if e.Error.Message == "" {
		t.Fatal("expected non-empty error message")
	}
}

// The case-insensitive middleware must not break routes that predate it:
// resourcegroups (lowercase registration), deployments (registers both
// casings and dispatches back into the router) and the Docker-backed vm.

func TestARMCaseInsensitive_UpstreamResourceGroups(t *testing.T) {
	ts := setupServer()
	defer ts.Close()
	base := ts.URL + "/subscriptions/sub1/"

	for _, seg := range []string{"resourcegroups", "resourceGroups", "RESOURCEGROUPS"} {
		resp := doRequest(t, "PUT", base+seg+"/rg-"+seg+"?api-version=2023-07-01", `{"location":"eastus"}`)
		expectStatus(t, resp, 201)
		resp.Body.Close()
		resp = doRequest(t, "GET", base+seg+"/rg-"+seg+"?api-version=2023-07-01", "")
		expectStatus(t, resp, 200)
		resp.Body.Close()
	}

	resp := doRequest(t, "GET", ts.URL+"/SUBSCRIPTIONS/sub1/RESOURCEGROUPS?api-version=2023-07-01", "")
	expectStatus(t, resp, 200)
	resp.Body.Close()

	resp = doRequest(t, "DELETE", base+"RESOURCEGROUPS/rg-resourcegroups?api-version=2023-07-01", "")
	if resp.StatusCode != 200 && resp.StatusCode != 202 && resp.StatusCode != 204 {
		t.Fatalf("DELETE status %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestARMCaseInsensitive_UpstreamVM(t *testing.T) {
	ts := setupVMServer(t)
	defer ts.Close()
	createRG(t, ts, "sub1", "rg1")

	url := ts.URL + "/subscriptions/sub1/RESOURCEGROUPS/rg1/providers/MICROSOFT.COMPUTE/VIRTUALMACHINES/web"
	resp := doRequest(t, "PUT", url+vmAV, `{"location":"eastus","properties":{"miniblue":{"image":"ubuntu:24.04"}}}`)
	expectStatus(t, resp, 201)
	m := decodeJSON(t, resp)
	resp.Body.Close()
	if id, _ := m["id"].(string); !strings.Contains(id, "/RESOURCEGROUPS/rg1/providers/MICROSOFT.COMPUTE/VIRTUALMACHINES/web") {
		t.Fatalf("id should mirror client casing, got %q", id)
	}

	resp = doRequest(t, "GET", vmURL(ts, "sub1", "rg1", "web")+vmAV, "")
	expectStatus(t, resp, 200)
	resp.Body.Close()

	resp = doRequest(t, "POST", url+"/POWEROFF"+vmAV, "")
	expectStatus(t, resp, 200)
	resp.Body.Close()

	resp = doRequest(t, "DELETE", url+vmAV, "")
	expectStatus(t, resp, 204)
	resp.Body.Close()
}

func TestARMCaseInsensitive_DeploymentDispatch(t *testing.T) {
	ts := setupServer()
	defer ts.Close()
	base := ts.URL + "/subscriptions/sub1/"

	resp := doRequest(t, "PUT", base+"resourceGroups/rg1?api-version=2023-07-01", `{"location":"eastus"}`)
	expectStatus(t, resp, 201)
	resp.Body.Close()

	// The template types are lowercased; dispatch must normalize them.
	tpl := `{"properties":{"mode":"Incremental","template":{"resources":[
		{"type":"microsoft.network/virtualnetworks","apiVersion":"2023-09-01","name":"vn1","location":"eastus",
		 "properties":{"addressSpace":{"addressPrefixes":["10.0.0.0/16"]}}}]}}}`
	resp = doRequest(t, "PUT", base+"RESOURCEGROUPS/rg1/providers/Microsoft.Resources/deployments/d1?api-version=2021-04-01", tpl)
	expectStatus(t, resp, 201)
	m := decodeJSON(t, resp)
	resp.Body.Close()
	props, _ := m["properties"].(map[string]interface{})
	if props["provisioningState"] != "Succeeded" {
		t.Fatalf("deployment state=%v error=%v", props["provisioningState"], props["error"])
	}

	resp = doRequest(t, "GET", base+"resourceGroups/rg1/providers/Microsoft.Network/virtualNetworks/vn1?api-version=2023-09-01", "")
	expectStatus(t, resp, 200)
	resp.Body.Close()
}


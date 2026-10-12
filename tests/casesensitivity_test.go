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

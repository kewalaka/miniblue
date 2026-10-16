package tests

import (
	"net/http"
	"strings"
	"testing"
)

const (
	pdSub  = "/subscriptions/a1b2c3d4-0000-4000-8000-0000000000aa"
	pdBase = pdSub + "/providers/Microsoft.Authorization/policyDefinitions"
	pdAPI  = "?api-version=2023-04-01"
	pdBody = `{"properties":{"policyType":"Custom","mode":"All","displayName":"Deny locations",` +
		`"policyRule":{"if":{"not":{"field":"location","in":["westeurope"]}},"then":{"effect":"deny"}}}}`
)

func TestPolicyDefinitionLifecycle(t *testing.T) {
	ts := setupServer()
	defer ts.Close()
	path := ts.URL + pdBase + "/Deny-Locations"

	expectStatus(t, doRequest(t, "GET", path+pdAPI, ""), http.StatusNotFound)

	resp := doRequest(t, "PUT", path+pdAPI, pdBody)
	expectStatus(t, resp, http.StatusCreated)
	created := decodeJSON(t, resp)
	if created["id"] != pdBase+"/Deny-Locations" || created["name"] != "Deny-Locations" ||
		created["type"] != "Microsoft.Authorization/policyDefinitions" {
		t.Fatalf("created = %v", created)
	}

	expectStatus(t, doRequest(t, "PUT", path+pdAPI, pdBody), http.StatusOK)

	mangled := ts.URL + strings.ToUpper(pdBase) + "/deny-LOCATIONS"
	resp = doRequest(t, "GET", mangled+pdAPI, "")
	expectStatus(t, resp, http.StatusOK)
	got := decodeJSON(t, resp)
	if got["id"] != created["id"] || got["name"] != "Deny-Locations" {
		t.Fatalf("mangled GET id/name = %v/%v, want %v/Deny-Locations", got["id"], got["name"], created["id"])
	}
	rule := got["properties"].(map[string]interface{})["policyRule"].(map[string]interface{})
	if rule["then"].(map[string]interface{})["effect"] != "deny" {
		t.Fatalf("policyRule not preserved: %v", rule)
	}

	resp = doRequest(t, "GET", ts.URL+strings.ToLower(pdBase)+pdAPI, "")
	expectStatus(t, resp, http.StatusOK)
	if list := decodeJSON(t, resp)["value"].([]interface{}); len(list) != 1 {
		t.Fatalf("list = %v, want one item", list)
	}

	expectStatus(t, doRequest(t, "DELETE", mangled+pdAPI, ""), http.StatusOK)
	expectStatus(t, doRequest(t, "DELETE", path+pdAPI, ""), http.StatusNoContent)
	resp = doRequest(t, "GET", path+pdAPI, "")
	expectStatus(t, resp, http.StatusNotFound)
	if code := decodeError(t, resp).Error.Code; code != "ResourceNotFound" {
		t.Fatalf("error code = %q", code)
	}
}

func TestPolicyDefinitionRequestValidation(t *testing.T) {
	ts := setupServer()
	defer ts.Close()

	expectStatus(t, doRequest(t, "PUT", ts.URL+pdBase+"/x?api-version=2019-01-01", pdBody), http.StatusBadRequest)
	expectStatus(t, doRequest(t, "PUT", ts.URL+pdBase+"/x"+pdAPI, `{"name":"y"}`), http.StatusBadRequest)
	expectStatus(t, doRequest(t, "POST", ts.URL+pdBase+"/x"+pdAPI, pdBody), http.StatusMethodNotAllowed)
}

// Only subscription scope is registered; other scopes fall through to the
// existing handlers (here the authorization middleware, which 404s them).
func TestPolicyDefinitionOnlyAtSubscriptionScope(t *testing.T) {
	ts := setupServer()
	defer ts.Close()

	for _, p := range []string{
		pdSub + "/resourceGroups/rg/providers/Microsoft.Authorization/policyDefinitions/x",
		"/providers/Microsoft.Management/managementGroups/mg/providers/Microsoft.Authorization/policyDefinitions/x",
	} {
		expectStatus(t, doRequest(t, "PUT", ts.URL+p+pdAPI, pdBody), http.StatusNotFound)
	}
}

func TestPolicyDefinitionCoexistsWithRoleDefinitions(t *testing.T) {
	ts := setupServer()
	defer ts.Close()

	expectStatus(t, doRequest(t, "PUT", ts.URL+pdBase+"/x"+pdAPI, pdBody), http.StatusCreated)
	resp := doRequest(t, "GET", ts.URL+pdSub+"/providers/Microsoft.Authorization/roleDefinitions?api-version=2022-04-01", "")
	expectStatus(t, resp, http.StatusOK)
}

func TestPolicyDefinitionServiceFilterAndReset(t *testing.T) {
	ts := setupServer()
	path := ts.URL + pdBase + "/x" + pdAPI
	expectStatus(t, doRequest(t, "PUT", path, pdBody), http.StatusCreated)
	expectStatus(t, doRequest(t, "POST", ts.URL+"/_miniblue/reset", ""), http.StatusOK)
	expectStatus(t, doRequest(t, "GET", path, ""), http.StatusNotFound)
	ts.Close()

	t.Setenv("SERVICES", "resourcegroups")
	ts = setupServer()
	defer ts.Close()
	expectStatus(t, doRequest(t, "PUT", ts.URL+pdBase+"/x"+pdAPI, pdBody), http.StatusNotFound)
}

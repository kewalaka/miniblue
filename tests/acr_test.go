package tests

import (
	"testing"
)

func TestACRRegistry(t *testing.T) {
	ts := setupServer()
	defer ts.Close()
	base := ts.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.ContainerRegistry/registries"
	av := "?api-version=2023-07-01"

	resp := doRequest(t, "PUT", base+"/myreg"+av,
		`{"location":"eastus","sku":{"name":"Premium","tier":"Premium"}}`)
	defer resp.Body.Close()
	expectStatus(t, resp, 201)

	m := decodeJSON(t, resp)
	props := m["properties"].(map[string]interface{})
	if props["loginServer"] != "myreg.azurecr.io" {
		t.Fatalf("expected loginServer=myreg.azurecr.io, got %v", props["loginServer"])
	}
	sku := m["sku"].(map[string]interface{})
	if sku["name"] != "Premium" {
		t.Fatalf("expected sku=Premium, got %v", sku["name"])
	}
}

// azurerm reads these back; omitting them shows as plan drift.
func TestACRRegistryARMPolicyAndRoleAssignmentMode(t *testing.T) {
	ts := setupServer()
	defer ts.Close()
	base := ts.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.ContainerRegistry/registries"
	av := "?api-version=2023-07-01"

	armStatus := func(m map[string]interface{}) interface{} {
		policies := m["properties"].(map[string]interface{})["policies"].(map[string]interface{})
		return policies["azureADAuthenticationAsArmPolicy"].(map[string]interface{})["status"]
	}
	mode := func(m map[string]interface{}) interface{} {
		return m["properties"].(map[string]interface{})["roleAssignmentMode"]
	}

	resp := doRequest(t, "PUT", base+"/defaults"+av, `{"location":"eastus"}`)
	expectStatus(t, resp, 201)
	m := decodeJSON(t, resp)
	resp.Body.Close()
	if armStatus(m) != "enabled" || mode(m) != "LegacyRegistryPermissions" {
		t.Fatalf("unexpected defaults: %v", m["properties"])
	}

	resp = doRequest(t, "PUT", base+"/custom"+av,
		`{"location":"eastus","properties":{"roleAssignmentMode":"AbacRepositoryPermissions","policies":{"azureADAuthenticationAsArmPolicy":{"status":"disabled"}}}}`)
	expectStatus(t, resp, 201)
	m = decodeJSON(t, resp)
	resp.Body.Close()
	if armStatus(m) != "disabled" || mode(m) != "AbacRepositoryPermissions" {
		t.Fatalf("expected requested values echoed, got %v", m["properties"])
	}
}

package tests

import (
	"testing"
)

func TestVNetSubnetLifecycle(t *testing.T) {
	ts := setupServer()
	defer ts.Close()
	base := ts.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Network/virtualNetworks"
	av := "?api-version=2023-09-01"

	// Create VNet
	resp := doRequest(t, "PUT", base+"/vnet1"+av,
		`{"location":"eastus","properties":{"addressSpace":{"addressPrefixes":["10.0.0.0/16"]}}}`)
	resp.Body.Close()
	expectStatus(t, resp, 201)

	// Create Subnet
	resp = doRequest(t, "PUT", base+"/vnet1/subnets/web"+av,
		`{"properties":{"addressPrefix":"10.0.1.0/24"}}`)
	defer resp.Body.Close()
	expectStatus(t, resp, 201)

	m := decodeJSON(t, resp)
	if m["name"] != "web" {
		t.Fatalf("expected subnet name=web, got %v", m["name"])
	}
	props := m["properties"].(map[string]interface{})
	if props["provisioningState"] != "Succeeded" {
		t.Fatalf("expected provisioningState=Succeeded")
	}

	// List subnets
	resp = doRequest(t, "GET", base+"/vnet1/subnets"+av, "")
	list := decodeJSON(t, resp)
	subnets := list["value"].([]interface{})
	if len(subnets) != 1 {
		t.Fatalf("expected 1 subnet, got %d", len(subnets))
	}

	// Delete VNet cascades subnets
	doRequest(t, "DELETE", base+"/vnet1"+av, "").Body.Close()
	resp = doRequest(t, "GET", base+"/vnet1/subnets/web"+av, "")
	defer resp.Body.Close()
	expectStatus(t, resp, 404)
}

func TestVNetInlineSubnetsPersist(t *testing.T) {
	ts := setupServer()
	defer ts.Close()
	base := ts.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Network/virtualNetworks"
	av := "?api-version=2023-09-01"

	resp := doRequest(t, "PUT", base+"/inline-vnet"+av,
		`{"location":"eastus","properties":{"addressSpace":{"addressPrefixes":["10.60.0.0/16"]},"subnets":[{"name":"app","properties":{"addressPrefix":"10.60.1.0/24","addressPrefixes":["10.60.1.0/24"]}},{"name":"data","properties":{"addressPrefix":"10.60.2.0/24","addressPrefixes":["10.60.2.0/24"]}}]}}`)
	expectStatus(t, resp, 201)
	m := decodeJSON(t, resp)
	resp.Body.Close()
	props := m["properties"].(map[string]interface{})
	subnets := props["subnets"].([]interface{})
	if len(subnets) != 2 {
		t.Fatalf("expected PUT response to include 2 inline subnets, got %d", len(subnets))
	}

	resp = doRequest(t, "GET", base+"/inline-vnet/subnets"+av, "")
	expectStatus(t, resp, 200)
	list := decodeJSON(t, resp)
	resp.Body.Close()
	subnets = list["value"].([]interface{})
	if len(subnets) != 2 {
		t.Fatalf("expected subnet list to include 2 inline subnets, got %d", len(subnets))
	}

	resp = doRequest(t, "GET", base+"/inline-vnet/subnets/app"+av, "")
	expectStatus(t, resp, 200)
	m = decodeJSON(t, resp)
	resp.Body.Close()
	if got := m["name"]; got != "app" {
		t.Fatalf("expected subnet GET name=app, got %v", got)
	}

	resp = doRequest(t, "GET", base+"/inline-vnet"+av, "")
	expectStatus(t, resp, 200)
	m = decodeJSON(t, resp)
	resp.Body.Close()
	props = m["properties"].(map[string]interface{})
	subnets = props["subnets"].([]interface{})
	if len(subnets) != 2 {
		t.Fatalf("expected VNet GET to include 2 inline subnets, got %d", len(subnets))
	}

	names := map[string]bool{}
	for _, item := range subnets {
		subnet := item.(map[string]interface{})
		names[subnet["name"].(string)] = true
	}
	for _, name := range []string{"app", "data"} {
		if !names[name] {
			t.Fatalf("expected subnet %q in VNet GET response, got %v", name, names)
		}
	}
}

func TestVNetPrivateEndpointVNetPoliciesRoundTrip(t *testing.T) {
	ts := setupServer()
	defer ts.Close()
	base := ts.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Network/virtualNetworks"
	av := "?api-version=2023-09-01"

	// Omitted on PUT -> omitted on GET (don't fabricate a value the caller never set).
	resp := doRequest(t, "PUT", base+"/vnet1"+av,
		`{"location":"eastus","properties":{"addressSpace":{"addressPrefixes":["10.0.0.0/16"]}}}`)
	resp.Body.Close()
	expectStatus(t, resp, 201)

	resp = doRequest(t, "GET", base+"/vnet1"+av, "")
	expectStatus(t, resp, 200)
	m := decodeJSON(t, resp)
	resp.Body.Close()
	props := m["properties"].(map[string]interface{})
	if _, present := props["privateEndpointVNetPolicies"]; present {
		t.Fatalf("expected privateEndpointVNetPolicies absent when not set on PUT, got %v", props["privateEndpointVNetPolicies"])
	}

	// Explicit "Basic" must round-trip through PUT and GET.
	resp = doRequest(t, "PUT", base+"/vnet2"+av,
		`{"location":"eastus","properties":{"addressSpace":{"addressPrefixes":["10.1.0.0/16"]},"privateEndpointVNetPolicies":"Basic"}}`)
	expectStatus(t, resp, 201)
	m = decodeJSON(t, resp)
	resp.Body.Close()
	props = m["properties"].(map[string]interface{})
	if got := props["privateEndpointVNetPolicies"]; got != "Basic" {
		t.Fatalf("expected PUT response privateEndpointVNetPolicies=Basic, got %v", got)
	}

	resp = doRequest(t, "GET", base+"/vnet2"+av, "")
	expectStatus(t, resp, 200)
	m = decodeJSON(t, resp)
	resp.Body.Close()
	props = m["properties"].(map[string]interface{})
	if got := props["privateEndpointVNetPolicies"]; got != "Basic" {
		t.Fatalf("expected GET response privateEndpointVNetPolicies=Basic, got %v", got)
	}
}

func TestSubnetOnNonexistentVNet(t *testing.T) {
	ts := setupServer()
	defer ts.Close()
	base := ts.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Network/virtualNetworks"
	av := "?api-version=2023-09-01"

	resp := doRequest(t, "PUT", base+"/nope/subnets/web"+av,
		`{"properties":{"addressPrefix":"10.0.1.0/24"}}`)
	defer resp.Body.Close()
	expectStatus(t, resp, 404)
}

func TestSubnetNetworkSecurityGroupAssociation(t *testing.T) {
	ts := setupServer()
	defer ts.Close()
	vnetBase := ts.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Network/virtualNetworks"
	nsgBase := ts.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Network/networkSecurityGroups"
	av := "?api-version=2023-09-01"

	// Create NSG
	resp := doRequest(t, "PUT", nsgBase+"/nsg1"+av, `{"location":"eastus"}`)
	resp.Body.Close()
	expectStatus(t, resp, 201)

	// Create VNet
	resp = doRequest(t, "PUT", vnetBase+"/vnet1"+av,
		`{"location":"eastus","properties":{"addressSpace":{"addressPrefixes":["10.0.0.0/16"]}}}`)
	resp.Body.Close()
	expectStatus(t, resp, 201)

	// Create Subnet WITHOUT NSG
	resp = doRequest(t, "PUT", vnetBase+"/vnet1/subnets/web"+av,
		`{"properties":{"addressPrefix":"10.0.1.0/24"}}`)
	expectStatus(t, resp, 201)
	m := decodeJSON(t, resp)
	resp.Body.Close()
	props := m["properties"].(map[string]interface{})
	if _, ok := props["networkSecurityGroup"]; ok {
		t.Fatalf("expected no networkSecurityGroup initially")
	}

	// Update Subnet WITH NSG association
	nsgID := "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Network/networkSecurityGroups/nsg1"
	resp = doRequest(t, "PUT", vnetBase+"/vnet1/subnets/web"+av,
		`{"properties":{"addressPrefix":"10.0.1.0/24","networkSecurityGroup":{"id":"`+nsgID+`"}}}`)
	expectStatus(t, resp, 200)
	m = decodeJSON(t, resp)
	resp.Body.Close()
	props = m["properties"].(map[string]interface{})
	nsg, ok := props["networkSecurityGroup"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected networkSecurityGroup in response")
	}
	if nsg["id"] != nsgID {
		t.Fatalf("expected nsg id=%s, got %v", nsgID, nsg["id"])
	}

	// Get subnet and verify NSG is persisted
	resp = doRequest(t, "GET", vnetBase+"/vnet1/subnets/web"+av, "")
	expectStatus(t, resp, 200)
	m = decodeJSON(t, resp)
	resp.Body.Close()
	props = m["properties"].(map[string]interface{})
	nsg, ok = props["networkSecurityGroup"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected networkSecurityGroup in GET response")
	}
	if nsg["id"] != nsgID {
		t.Fatalf("expected persisted nsg id=%s, got %v", nsgID, nsg["id"])
	}

	// Remove NSG association (update with empty networkSecurityGroup)
	resp = doRequest(t, "PUT", vnetBase+"/vnet1/subnets/web"+av,
		`{"properties":{"addressPrefix":"10.0.1.0/24"}}`)
	expectStatus(t, resp, 200)
	m = decodeJSON(t, resp)
	resp.Body.Close()
	props = m["properties"].(map[string]interface{})
	if _, ok := props["networkSecurityGroup"]; ok {
		t.Fatalf("expected no networkSecurityGroup after removal")
	}

	// Cleanup
	doRequest(t, "DELETE", vnetBase+"/vnet1"+av, "").Body.Close()
	doRequest(t, "DELETE", nsgBase+"/nsg1"+av, "").Body.Close()
}

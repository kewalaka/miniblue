package tests

import (
	"fmt"
	"testing"
)

func TestNSGLifecycle(t *testing.T) {
	ts := setupServer()
	defer ts.Close()
	base := ts.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Network/networkSecurityGroups"
	av := "?api-version=2023-09-01"

	// Create NSG
	resp := doRequest(t, "PUT", base+"/nsg1"+av, `{"location":"eastus"}`)
	expectStatus(t, resp, 201)
	m := decodeJSON(t, resp)
	resp.Body.Close()
	if m["name"] != "nsg1" {
		t.Fatalf("expected name=nsg1, got %v", m["name"])
	}
	if m["type"] != "Microsoft.Network/networkSecurityGroups" {
		t.Fatalf("expected correct type, got %v", m["type"])
	}
	props := m["properties"].(map[string]interface{})
	if props["provisioningState"] != "Succeeded" {
		t.Fatalf("expected provisioningState=Succeeded")
	}
	defaultRules := props["defaultSecurityRules"].([]interface{})
	if len(defaultRules) != 6 {
		t.Fatalf("expected 6 default security rules, got %d", len(defaultRules))
	}
	secRules := props["securityRules"].([]interface{})
	if len(secRules) != 0 {
		t.Fatalf("expected 0 security rules on fresh NSG, got %d", len(secRules))
	}

	// Get NSG
	resp = doRequest(t, "GET", base+"/nsg1"+av, "")
	expectStatus(t, resp, 200)
	got := decodeJSON(t, resp)
	resp.Body.Close()
	if got["name"] != "nsg1" {
		t.Fatalf("GET: expected name=nsg1, got %v", got["name"])
	}

	// Create security rule
	resp = doRequest(t, "PUT", base+"/nsg1/securityRules/allow-http"+av,
		`{"properties":{"priority":100,"direction":"Inbound","access":"Allow","protocol":"Tcp","sourcePortRange":"*","destinationPortRange":"80","sourceAddressPrefix":"*","destinationAddressPrefix":"*"}}`)
	expectStatus(t, resp, 201)
	rule := decodeJSON(t, resp)
	resp.Body.Close()
	if rule["name"] != "allow-http" {
		t.Fatalf("expected rule name=allow-http, got %v", rule["name"])
	}
	if rule["type"] != "Microsoft.Network/networkSecurityGroups/securityRules" {
		t.Fatalf("expected correct rule type, got %v", rule["type"])
	}
	ruleProps := rule["properties"].(map[string]interface{})
	if ruleProps["priority"] != float64(100) {
		t.Fatalf("expected priority=100, got %v", ruleProps["priority"])
	}
	if ruleProps["direction"] != "Inbound" {
		t.Fatalf("expected direction=Inbound, got %v", ruleProps["direction"])
	}
	if ruleProps["access"] != "Allow" {
		t.Fatalf("expected access=Allow, got %v", ruleProps["access"])
	}
	if ruleProps["protocol"] != "Tcp" {
		t.Fatalf("expected protocol=Tcp, got %v", ruleProps["protocol"])
	}

	// Update the rule (PUT again)
	resp = doRequest(t, "PUT", base+"/nsg1/securityRules/allow-http"+av,
		`{"properties":{"priority":150,"direction":"Inbound","access":"Allow","protocol":"Tcp","sourcePortRange":"*","destinationPortRange":"8080","sourceAddressPrefix":"10.0.0.0/8","destinationAddressPrefix":"*"}}`)
	expectStatus(t, resp, 200)
	updatedRule := decodeJSON(t, resp)
	resp.Body.Close()
	updatedRuleProps := updatedRule["properties"].(map[string]interface{})
	if updatedRuleProps["priority"] != float64(150) {
		t.Fatalf("expected updated priority=150, got %v", updatedRuleProps["priority"])
	}
	if updatedRuleProps["destinationPortRange"] != "8080" {
		t.Fatalf("expected updated port=8080, got %v", updatedRuleProps["destinationPortRange"])
	}

	// List security rules
	resp = doRequest(t, "GET", base+"/nsg1/securityRules"+av, "")
	expectStatus(t, resp, 200)
	list := decodeJSON(t, resp)
	resp.Body.Close()
	rules := list["value"].([]interface{})
	if len(rules) != 1 {
		t.Fatalf("expected 1 security rule, got %d", len(rules))
	}

	// Get NSG again - should show security rules populated
	resp = doRequest(t, "GET", base+"/nsg1"+av, "")
	expectStatus(t, resp, 200)
	nsgWithRules := decodeJSON(t, resp)
	resp.Body.Close()
	nsgProps := nsgWithRules["properties"].(map[string]interface{})
	nsgSecRules := nsgProps["securityRules"].([]interface{})
	if len(nsgSecRules) != 1 {
		t.Fatalf("expected NSG to show 1 security rule, got %d", len(nsgSecRules))
	}

	// Delete security rule
	resp = doRequest(t, "DELETE", base+"/nsg1/securityRules/allow-http"+av, "")
	expectStatus(t, resp, 202)
	resp.Body.Close()

	// Verify rule deleted
	resp = doRequest(t, "GET", base+"/nsg1/securityRules/allow-http"+av, "")
	expectStatus(t, resp, 404)
	resp.Body.Close()

	// Delete NSG
	resp = doRequest(t, "DELETE", base+"/nsg1"+av, "")
	expectStatus(t, resp, 202)
	resp.Body.Close()

	// Get deleted NSG
	resp = doRequest(t, "GET", base+"/nsg1"+av, "")
	expectStatus(t, resp, 404)
	resp.Body.Close()
}

func TestNSGCascadeDeleteRules(t *testing.T) {
	ts := setupServer()
	defer ts.Close()
	base := ts.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Network/networkSecurityGroups"
	av := "?api-version=2023-09-01"

	// Create NSG with multiple rules
	doRequest(t, "PUT", base+"/nsg2"+av, `{"location":"eastus"}`).Body.Close()
	doRequest(t, "PUT", base+"/nsg2/securityRules/rule1"+av,
		`{"properties":{"priority":100,"direction":"Inbound","access":"Allow","protocol":"Tcp","sourcePortRange":"*","destinationPortRange":"80","sourceAddressPrefix":"*","destinationAddressPrefix":"*"}}`).Body.Close()
	doRequest(t, "PUT", base+"/nsg2/securityRules/rule2"+av,
		`{"properties":{"priority":200,"direction":"Inbound","access":"Deny","protocol":"*","sourcePortRange":"*","destinationPortRange":"*","sourceAddressPrefix":"*","destinationAddressPrefix":"*"}}`).Body.Close()

	// Verify 2 rules exist
	resp := doRequest(t, "GET", base+"/nsg2/securityRules"+av, "")
	list := decodeJSON(t, resp)
	resp.Body.Close()
	rules := list["value"].([]interface{})
	if len(rules) != 2 {
		t.Fatalf("expected 2 rules before cascade delete, got %d", len(rules))
	}

	// Delete NSG should cascade delete all rules
	resp = doRequest(t, "DELETE", base+"/nsg2"+av, "")
	expectStatus(t, resp, 202)
	resp.Body.Close()

	resp = doRequest(t, "GET", base+"/nsg2/securityRules/rule1"+av, "")
	expectStatus(t, resp, 404)
	resp.Body.Close()

	resp = doRequest(t, "GET", base+"/nsg2/securityRules/rule2"+av, "")
	expectStatus(t, resp, 404)
	resp.Body.Close()
}

func TestNSGRuleOnNonexistentNSG(t *testing.T) {
	ts := setupServer()
	defer ts.Close()
	base := ts.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Network/networkSecurityGroups"
	av := "?api-version=2023-09-01"

	resp := doRequest(t, "PUT", base+"/nope/securityRules/rule1"+av,
		`{"properties":{"priority":100,"direction":"Inbound","access":"Allow","protocol":"Tcp","sourcePortRange":"*","destinationPortRange":"443","sourceAddressPrefix":"*","destinationAddressPrefix":"*"}}`)
	expectStatus(t, resp, 404)
	e := decodeError(t, resp)
	resp.Body.Close()
	if e.Error.Code != "ResourceNotFound" {
		t.Fatalf("expected ResourceNotFound, got %s", e.Error.Code)
	}
}

func TestNSGNotFound(t *testing.T) {
	ts := setupServer()
	defer ts.Close()
	base := ts.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Network/networkSecurityGroups"
	av := "?api-version=2023-09-01"

	resp := doRequest(t, "GET", base+"/nonexistent"+av, "")
	expectStatus(t, resp, 404)
	resp.Body.Close()

	resp = doRequest(t, "DELETE", base+"/nonexistent"+av, "")
	expectStatus(t, resp, 404)
	resp.Body.Close()
}

func TestNSGEmptyList(t *testing.T) {
	ts := setupServer()
	defer ts.Close()
	base := ts.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Network/networkSecurityGroups"
	av := "?api-version=2023-09-01"

	resp := doRequest(t, "GET", base+av, "")
	expectStatus(t, resp, 200)
	list := decodeJSON(t, resp)
	resp.Body.Close()
	items := list["value"].([]interface{})
	if len(items) != 0 {
		t.Fatalf("expected 0 NSGs, got %d", len(items))
	}
}

func TestNSGUpdateReturns200(t *testing.T) {
	ts := setupServer()
	defer ts.Close()
	base := ts.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Network/networkSecurityGroups"
	av := "?api-version=2023-09-01"

	resp := doRequest(t, "PUT", base+"/nsg-upd"+av, `{"location":"eastus"}`)
	expectStatus(t, resp, 201)
	resp.Body.Close()

	resp = doRequest(t, "PUT", base+"/nsg-upd"+av, `{"location":"westus"}`)
	expectStatus(t, resp, 200)
	m := decodeJSON(t, resp)
	resp.Body.Close()
	if m["location"] != "westus" {
		t.Fatalf("expected updated location=westus, got %v", m["location"])
	}

	doRequest(t, "DELETE", base+"/nsg-upd"+av, "").Body.Close()
}

// azurerm_network_security_group with inline security_rule blocks sends rules in the NSG PUT.
func TestNSGInlineSecurityRules(t *testing.T) {
	ts := setupServer()
	defer ts.Close()
	base := ts.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Network/networkSecurityGroups"
	av := "?api-version=2023-09-01"

	ruleNames := func(path string) []string {
		t.Helper()
		resp := doRequest(t, "GET", path, "")
		expectStatus(t, resp, 200)
		defer resp.Body.Close()
		var names []string
		for _, r := range decodeJSON(t, resp)["properties"].(map[string]interface{})["securityRules"].([]interface{}) {
			names = append(names, r.(map[string]interface{})["name"].(string))
		}
		return names
	}
	rule := func(name string, priority int) string {
		return fmt.Sprintf(`{"name":%q,"properties":{"priority":%d,"direction":"Inbound","access":"Allow","protocol":"Tcp","sourcePortRange":"*","destinationPortRange":"80","sourceAddressPrefix":"*","destinationAddressPrefix":"*"}}`, name, priority)
	}

	resp := doRequest(t, "PUT", base+"/nsg-inline"+av,
		`{"location":"eastus","properties":{"securityRules":[`+rule("a", 100)+`,`+rule("b", 110)+`]}}`)
	expectStatus(t, resp, 201)
	resp.Body.Close()
	if got := ruleNames(base + "/nsg-inline" + av); len(got) != 2 {
		t.Fatalf("expected inline rules a,b persisted, got %v", got)
	}

	resp = doRequest(t, "GET", base+"/nsg-inline/securityRules/a"+av, "")
	expectStatus(t, resp, 200)
	resp.Body.Close()

	// Omitting securityRules keeps the existing set.
	resp = doRequest(t, "PUT", base+"/nsg-inline"+av, `{"location":"eastus","tags":{"k":"v"}}`)
	expectStatus(t, resp, 200)
	resp.Body.Close()
	if got := ruleNames(base + "/nsg-inline" + av); len(got) != 2 {
		t.Fatalf("expected rules kept when securityRules omitted, got %v", got)
	}

	// A new inline set replaces the old one.
	resp = doRequest(t, "PUT", base+"/nsg-inline"+av,
		`{"location":"eastus","properties":{"securityRules":[`+rule("c", 120)+`]}}`)
	expectStatus(t, resp, 200)
	resp.Body.Close()
	if got := ruleNames(base + "/nsg-inline" + av); len(got) != 1 || got[0] != "c" {
		t.Fatalf("expected inline set replaced by c, got %v", got)
	}
	resp = doRequest(t, "GET", base+"/nsg-inline/securityRules/a"+av, "")
	expectStatus(t, resp, 404)
	resp.Body.Close()
}

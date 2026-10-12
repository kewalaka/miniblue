package tests

import (
	"net/http/httptest"
	"testing"
)

const cosmosSub = "sub1"
const cosmosRG = "rg1"
const cosmosAcct = "mycosmosacct"

func cosmosARMBase(ts *httptest.Server) string {
	return ts.URL + "/subscriptions/" + cosmosSub + "/resourceGroups/" + cosmosRG + "/providers/Microsoft.DocumentDB/databaseAccounts"
}

func TestCosmosDBARMAccountCRUD(t *testing.T) {
	ts := setupServer()
	defer ts.Close()
	base := cosmosARMBase(ts) + "/" + cosmosAcct

	// Create account
	resp := doRequest(t, "PUT", base, `{"location":"eastus","kind":"GlobalDocumentDB"}`)
	defer resp.Body.Close()
	expectStatus(t, resp, 200)

	m := decodeJSON(t, resp)
	if m["name"] != cosmosAcct {
		t.Fatalf("expected name=%s, got %v", cosmosAcct, m["name"])
	}
	if m["type"] != "Microsoft.DocumentDB/databaseAccounts" {
		t.Fatalf("expected correct type, got %v", m["type"])
	}
	props := m["properties"].(map[string]interface{})
	if props["provisioningState"] != "Succeeded" {
		t.Fatalf("expected provisioningState=Succeeded, got %v", props["provisioningState"])
	}
	if props["documentEndpoint"] == nil {
		t.Fatal("expected documentEndpoint in properties")
	}
	for _, field := range []string{"capabilities", "ipRules", "virtualNetworkRules", "cors", "networkAclBypassResourceIds"} {
		if items, ok := props[field].([]interface{}); !ok {
			t.Fatalf("expected %s to be an array, got %T", field, props[field])
		} else if len(items) != 0 {
			t.Fatalf("expected %s to default empty, got %v", field, items)
		}
	}

	// Get account
	resp2 := doRequest(t, "GET", base, "")
	defer resp2.Body.Close()
	expectStatus(t, resp2, 200)

	// Update account (idempotent)
	resp3 := doRequest(t, "PUT", base, `{"location":"eastus"}`)
	defer resp3.Body.Close()
	expectStatus(t, resp3, 200)
}

func TestCosmosDBARMAccountActions(t *testing.T) {
	ts := setupServer()
	defer ts.Close()
	base := cosmosARMBase(ts) + "/" + cosmosAcct

	resp := doRequest(t, "PUT", base+"?api-version=2023-04-15", `{"location":"eastus","kind":"GlobalDocumentDB"}`)
	resp.Body.Close()
	expectStatus(t, resp, 200)

	resp = doRequest(t, "POST", base+"/listKeys?api-version=2023-04-15", "")
	expectStatus(t, resp, 200)
	keys := decodeJSON(t, resp)
	resp.Body.Close()
	for _, field := range []string{"primaryMasterKey", "secondaryMasterKey", "primaryReadonlyMasterKey", "secondaryReadonlyMasterKey"} {
		if keys[field] == "" {
			t.Fatalf("expected %s in listKeys response", field)
		}
	}

	resp = doRequest(t, "POST", base+"/listConnectionStrings?api-version=2023-04-15", "")
	expectStatus(t, resp, 200)
	body := decodeJSON(t, resp)
	resp.Body.Close()
	strings, ok := body["connectionStrings"].([]interface{})
	if !ok || len(strings) == 0 {
		t.Fatalf("expected non-empty connectionStrings array, got %v", body["connectionStrings"])
	}
	first := strings[0].(map[string]interface{})
	if first["connectionString"] == "" || first["description"] == "" {
		t.Fatalf("expected connectionString and description, got %v", first)
	}
}

func TestCosmosDBARMAccountCapabilitiesRoundTrip(t *testing.T) {
	ts := setupServer()
	defer ts.Close()
	base := cosmosARMBase(ts) + "/" + cosmosAcct
	body := `{"location":"eastus","kind":"GlobalDocumentDB","properties":{"capabilities":[{"name":"EnableTable"}]}}`

	resp := doRequest(t, "PUT", base, body)
	defer resp.Body.Close()
	expectStatus(t, resp, 200)

	resp2 := doRequest(t, "GET", base, "")
	defer resp2.Body.Close()
	expectStatus(t, resp2, 200)

	m := decodeJSON(t, resp2)
	props := m["properties"].(map[string]interface{})
	capabilities, ok := props["capabilities"].([]interface{})
	if !ok {
		t.Fatalf("expected capabilities to be an array, got %T", props["capabilities"])
	}
	if len(capabilities) != 1 {
		t.Fatalf("expected one capability, got %v", capabilities)
	}
	capability := capabilities[0].(map[string]interface{})
	if capability["name"] != "EnableTable" {
		t.Fatalf("expected capability name EnableTable, got %v", capability["name"])
	}
}

func TestCosmosDBARMAccountPlanDriftFieldsRoundTrip(t *testing.T) {
	ts := setupServer()
	defer ts.Close()
	base := cosmosARMBase(ts) + "/" + cosmosAcct
	body := `{
		"location":"eastus",
		"kind":"GlobalDocumentDB",
		"tags":{"Name":"drift-repro-cosmos"},
		"properties":{
			"databaseAccountOfferType":"Standard",
			"consistencyPolicy":{"defaultConsistencyLevel":"Session"},
			"locations":[{"locationName":"eastus","failoverPriority":0}],
			"publicNetworkAccess":"Disabled",
			"capabilities":[{"name":"EnableTable"}]
		}
	}`

	resp := doRequest(t, "PUT", base, body)
	resp.Body.Close()
	expectStatus(t, resp, 200)

	resp = doRequest(t, "GET", base, "")
	defer resp.Body.Close()
	expectStatus(t, resp, 200)

	m := decodeJSON(t, resp)
	tags := m["tags"].(map[string]interface{})
	if tags["Name"] != "drift-repro-cosmos" {
		t.Fatalf("expected tags to round trip, got %v", tags)
	}
	props := m["properties"].(map[string]interface{})
	locations := props["locations"].([]interface{})
	location := locations[0].(map[string]interface{})
	if location["locationName"] != "eastus" || location["failoverPriority"] != float64(0) || location["isZoneRedundant"] != false {
		t.Fatalf("expected locations to round trip, got %v", locations)
	}
	if location["id"] == "" {
		t.Fatalf("expected location id, got %v", location)
	}
	policies := props["failoverPolicies"].([]interface{})
	policy := policies[0].(map[string]interface{})
	if policy["locationName"] != "eastus" || policy["failoverPriority"] != float64(0) || policy["id"] != location["id"] {
		t.Fatalf("expected failover policies to match locations, got %v", policies)
	}
	if props["publicNetworkAccess"] != "Disabled" {
		t.Fatalf("expected publicNetworkAccess Disabled, got %v", props["publicNetworkAccess"])
	}
	if _, ok := props["minimumTlsVersion"]; ok {
		t.Fatalf("did not expect minimumTlsVersion when omitted from PUT, got %v", props["minimumTlsVersion"])
	}
}

func TestCosmosDBARMAccountList(t *testing.T) {
	ts := setupServer()
	defer ts.Close()
	base := cosmosARMBase(ts)

	doRequest(t, "PUT", base+"/acct-a", `{}`).Body.Close()
	doRequest(t, "PUT", base+"/acct-b", `{}`).Body.Close()

	resp := doRequest(t, "GET", base, "")
	defer resp.Body.Close()
	expectStatus(t, resp, 200)

	m := decodeJSON(t, resp)
	items := m["value"].([]interface{})
	if len(items) < 2 {
		t.Fatalf("expected at least 2 accounts, got %d", len(items))
	}
}

func TestCosmosDBARMAccountNotFound(t *testing.T) {
	ts := setupServer()
	defer ts.Close()
	resp := doRequest(t, "GET", cosmosARMBase(ts)+"/nonexistent", "")
	defer resp.Body.Close()
	expectStatus(t, resp, 404)
}

func TestCosmosDBARMAccountDelete(t *testing.T) {
	ts := setupServer()
	defer ts.Close()
	base := cosmosARMBase(ts) + "/acct-del"

	doRequest(t, "PUT", base, `{}`).Body.Close()

	resp := doRequest(t, "DELETE", base, "")
	defer resp.Body.Close()
	expectStatus(t, resp, 202)

	resp2 := doRequest(t, "GET", base, "")
	defer resp2.Body.Close()
	expectStatus(t, resp2, 404)
}

func TestCosmosDBARMSQLDatabaseCRUD(t *testing.T) {
	ts := setupServer()
	defer ts.Close()
	acctBase := cosmosARMBase(ts) + "/" + cosmosAcct
	doRequest(t, "PUT", acctBase, `{}`).Body.Close()

	dbBase := acctBase + "/sqlDatabases/mydb"

	// Create database
	resp := doRequest(t, "PUT", dbBase, `{}`)
	defer resp.Body.Close()
	expectStatus(t, resp, 200)

	m := decodeJSON(t, resp)
	if m["name"] != "mydb" {
		t.Fatalf("expected name=mydb, got %v", m["name"])
	}
	if m["type"] != "Microsoft.DocumentDB/databaseAccounts/sqlDatabases" {
		t.Fatalf("expected correct type, got %v", m["type"])
	}

	// Get database
	resp2 := doRequest(t, "GET", dbBase, "")
	defer resp2.Body.Close()
	expectStatus(t, resp2, 200)

	// List databases
	resp3 := doRequest(t, "GET", acctBase+"/sqlDatabases", "")
	defer resp3.Body.Close()
	expectStatus(t, resp3, 200)
	ml := decodeJSON(t, resp3)
	if len(ml["value"].([]interface{})) == 0 {
		t.Fatal("expected at least 1 database in list")
	}

	// Delete database
	resp4 := doRequest(t, "DELETE", dbBase, "")
	defer resp4.Body.Close()
	expectStatus(t, resp4, 202)

	resp5 := doRequest(t, "GET", dbBase, "")
	defer resp5.Body.Close()
	expectStatus(t, resp5, 404)
}

func TestCosmosDBARMContainerCRUD(t *testing.T) {
	ts := setupServer()
	defer ts.Close()
	acctBase := cosmosARMBase(ts) + "/" + cosmosAcct
	doRequest(t, "PUT", acctBase, `{}`).Body.Close()
	doRequest(t, "PUT", acctBase+"/sqlDatabases/mydb", `{}`).Body.Close()

	cBase := acctBase + "/sqlDatabases/mydb/containers/users"

	// Create container
	resp := doRequest(t, "PUT", cBase, `{}`)
	defer resp.Body.Close()
	expectStatus(t, resp, 200)

	m := decodeJSON(t, resp)
	if m["name"] != "users" {
		t.Fatalf("expected name=users, got %v", m["name"])
	}
	if m["type"] != "Microsoft.DocumentDB/databaseAccounts/sqlDatabases/containers" {
		t.Fatalf("expected correct type, got %v", m["type"])
	}

	// Get container
	resp2 := doRequest(t, "GET", cBase, "")
	defer resp2.Body.Close()
	expectStatus(t, resp2, 200)

	// List containers
	resp3 := doRequest(t, "GET", acctBase+"/sqlDatabases/mydb/containers", "")
	defer resp3.Body.Close()
	expectStatus(t, resp3, 200)
	ml := decodeJSON(t, resp3)
	if len(ml["value"].([]interface{})) == 0 {
		t.Fatal("expected at least 1 container in list")
	}

	// Delete container
	resp4 := doRequest(t, "DELETE", cBase, "")
	defer resp4.Body.Close()
	expectStatus(t, resp4, 202)

	resp5 := doRequest(t, "GET", cBase, "")
	defer resp5.Body.Close()
	expectStatus(t, resp5, 404)
}

func TestCosmosDBARMTableCRUD(t *testing.T) {
	ts := setupServer()
	defer ts.Close()
	acctBase := cosmosARMBase(ts) + "/" + cosmosAcct
	doRequest(t, "PUT", acctBase, `{}`).Body.Close()

	tableBase := acctBase + "/tables/users"
	body := `{"properties":{"resource":{"id":"users"},"options":{"throughput":400}},"location":"eastus"}`

	resp := doRequest(t, "PUT", tableBase, body)
	defer resp.Body.Close()
	expectStatus(t, resp, 200)

	m := decodeJSON(t, resp)
	if m["name"] != "users" {
		t.Fatalf("expected name=users, got %v", m["name"])
	}
	if m["type"] != "Microsoft.DocumentDB/databaseAccounts/tables" {
		t.Fatalf("expected correct type, got %v", m["type"])
	}
	if m["location"] != "eastus" {
		t.Fatalf("expected request location to be echoed, got %v", m["location"])
	}
	props := m["properties"].(map[string]interface{})
	resource := props["resource"].(map[string]interface{})
	if resource["id"] != "users" {
		t.Fatalf("expected properties.resource.id to be echoed, got %v", resource["id"])
	}

	resp2 := doRequest(t, "GET", tableBase, "")
	defer resp2.Body.Close()
	expectStatus(t, resp2, 200)

	resp3 := doRequest(t, "GET", acctBase+"/tables", "")
	defer resp3.Body.Close()
	expectStatus(t, resp3, 200)
	ml := decodeJSON(t, resp3)
	if len(ml["value"].([]interface{})) == 0 {
		t.Fatal("expected at least 1 table in list")
	}

	resp4 := doRequest(t, "PUT", tableBase, body)
	defer resp4.Body.Close()
	expectStatus(t, resp4, 200)

	resp5 := doRequest(t, "DELETE", tableBase, "")
	defer resp5.Body.Close()
	expectStatus(t, resp5, 202)

	resp6 := doRequest(t, "GET", tableBase, "")
	defer resp6.Body.Close()
	expectStatus(t, resp6, 404)
}

func TestCosmosDBARMTableThroughputSettings(t *testing.T) {
	ts := setupServer()
	defer ts.Close()
	acctBase := cosmosARMBase(ts) + "/" + cosmosAcct
	doRequest(t, "PUT", acctBase, `{}`).Body.Close()
	doRequest(t, "PUT", acctBase+"/tables/users", `{"properties":{"resource":{"id":"users"}}}`).Body.Close()

	throughputBase := acctBase + "/tables/users/throughputSettings/default"
	body := `{"properties":{"resource":{"throughput":400},"autoscaleSettings":{"maxThroughput":4000}}}`

	resp := doRequest(t, "PUT", throughputBase, body)
	defer resp.Body.Close()
	expectStatus(t, resp, 200)

	m := decodeJSON(t, resp)
	if m["name"] != "default" {
		t.Fatalf("expected name=default, got %v", m["name"])
	}
	if m["type"] != "Microsoft.DocumentDB/databaseAccounts/tables/throughputSettings" {
		t.Fatalf("expected correct type, got %v", m["type"])
	}
	props := m["properties"].(map[string]interface{})
	resource := props["resource"].(map[string]interface{})
	if resource["throughput"] != float64(400) {
		t.Fatalf("expected throughput to be echoed, got %v", resource["throughput"])
	}
	autoscale := props["autoscaleSettings"].(map[string]interface{})
	if autoscale["maxThroughput"] != float64(4000) {
		t.Fatalf("expected maxThroughput to be echoed, got %v", autoscale["maxThroughput"])
	}

	resp2 := doRequest(t, "GET", throughputBase, "")
	defer resp2.Body.Close()
	expectStatus(t, resp2, 200)

	resp3 := doRequest(t, "PUT", throughputBase, body)
	defer resp3.Body.Close()
	expectStatus(t, resp3, 200)
}

func TestCosmosDBARMResponseHasID(t *testing.T) {
	ts := setupServer()
	defer ts.Close()
	base := cosmosARMBase(ts) + "/myacct"

	resp := doRequest(t, "PUT", base, `{}`)
	defer resp.Body.Close()
	m := decodeJSON(t, resp)

	expectedID := "/subscriptions/" + cosmosSub + "/resourceGroups/" + cosmosRG + "/providers/Microsoft.DocumentDB/databaseAccounts/myacct"
	if m["id"] != expectedID {
		t.Fatalf("expected id=%s, got %v", expectedID, m["id"])
	}
}

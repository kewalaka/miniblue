package tests

import "testing"

func TestRedisPutPersistsLocationTagsAndSku(t *testing.T) {
	ts := setupServer()
	defer ts.Close()

	url := ts.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Cache/redis/cache1?api-version=2024-03-01"
	body := `{
		"location":"westeurope",
		"tags":{"env":"test"},
		"properties":{"sku":{"name":"Premium","family":"P","capacity":2}}
	}`

	check := func(label string, cache map[string]interface{}) {
		t.Helper()
		if cache["location"] != "westeurope" {
			t.Fatalf("%s: location = %v, want westeurope", label, cache["location"])
		}
		if tags, _ := cache["tags"].(map[string]interface{}); tags["env"] != "test" {
			t.Fatalf("%s: tags = %v, want env=test", label, cache["tags"])
		}
		props := cache["properties"].(map[string]interface{})
		sku := props["sku"].(map[string]interface{})
		if sku["name"] != "Premium" || sku["family"] != "P" || sku["capacity"] != float64(2) {
			t.Fatalf("%s: sku = %v, want Premium/P/2", label, sku)
		}
	}

	resp := doRequest(t, "PUT", url, body)
	defer resp.Body.Close()
	expectStatus(t, resp, 201)
	check("create", decodeJSON(t, resp))

	resp = doRequest(t, "GET", url, "")
	defer resp.Body.Close()
	expectStatus(t, resp, 200)
	check("get", decodeJSON(t, resp))

	resp = doRequest(t, "PUT", url, body)
	defer resp.Body.Close()
	expectStatus(t, resp, 200)
	check("update", decodeJSON(t, resp))
}

func TestRedisPutWithoutBodyUsesDefaults(t *testing.T) {
	ts := setupServer()
	defer ts.Close()

	resp := doRequest(t, "PUT", ts.URL+"/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Cache/redis/cache1", "")
	defer resp.Body.Close()
	expectStatus(t, resp, 201)
	cache := decodeJSON(t, resp)
	sku := cache["properties"].(map[string]interface{})["sku"].(map[string]interface{})
	if cache["location"] != "eastus" || sku["name"] != "Basic" {
		t.Fatalf("unexpected defaults: %v", cache)
	}
}

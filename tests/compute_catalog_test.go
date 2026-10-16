package tests

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestComputeStaticCatalogEndpoints(t *testing.T) {
	ts := setupServer()
	defer ts.Close()

	base := ts.URL + "/subscriptions/sub1/providers/Microsoft.Compute"
	av := "?api-version=2023-09-01"

	resp := doRequest(t, "GET", base+"/locations/eastus/vmSizes"+av, "")
	defer resp.Body.Close()
	expectStatus(t, resp, 200)
	vmSizes := decodeJSON(t, resp)
	vmSizeItems := listValue(t, vmSizes)
	if len(vmSizeItems) == 0 {
		t.Fatal("expected vmSizes to include static values")
	}
	if itemName(t, vmSizeItems, 0) != "Standard_DS1_v2" {
		t.Fatalf("expected Standard_DS1_v2 in vmSizes, got %v", vmSizes["value"])
	}

	resp = doRequest(t, "GET", base+"/skus"+av, "")
	defer resp.Body.Close()
	expectStatus(t, resp, 200)
	skus := decodeJSON(t, resp)
	if len(listValue(t, skus)) == 0 {
		t.Fatal("expected skus to include static values")
	}

	// The VM image listings return a bare JSON array, unlike vmSizes and skus.
	resp = doRequest(t, "GET", base+"/locations/eastus/publishers"+av, "")
	defer resp.Body.Close()
	expectStatus(t, resp, 200)
	publishers := decodeJSONArray(t, resp)
	if len(publishers) == 0 {
		t.Fatal("expected publishers to include static values")
	}

	resp = doRequest(t, "GET", base+"/locations/eastus/publishers/Canonical/artifacttypes/vmimage/offers"+av, "")
	defer resp.Body.Close()
	expectStatus(t, resp, 200)
	offers := decodeJSONArray(t, resp)
	if len(offers) == 0 || itemName(t, offers, 0) != "0001-com-ubuntu-server-jammy" {
		t.Fatalf("expected Ubuntu offer, got %v", offers)
	}

	resp = doRequest(t, "GET", base+"/locations/eastus/publishers/Canonical/artifacttypes/vmimage/offers/0001-com-ubuntu-server-jammy/skus"+av, "")
	defer resp.Body.Close()
	expectStatus(t, resp, 200)
	imageSkus := decodeJSONArray(t, resp)
	if len(imageSkus) == 0 || itemName(t, imageSkus, 0) != "22_04-lts" {
		t.Fatalf("expected Ubuntu image sku, got %v", imageSkus)
	}

	resp = doRequest(t, "GET", base+"/locations/eastus/publishers/Canonical/artifacttypes/vmimage/offers/0001-com-ubuntu-server-jammy/skus/22_04-lts/versions"+av, "")
	defer resp.Body.Close()
	expectStatus(t, resp, 200)
	versions := decodeJSONArray(t, resp)
	if len(versions) == 0 {
		t.Fatal("expected versions to include static values")
	}

	resp = doRequest(t, "GET", base+"/locations/eastus/publishers/Canonical/artifacttypes/vmimage/offers/0001-com-ubuntu-server-jammy/skus/22_04-lts/versions/latest"+av, "")
	defer resp.Body.Close()
	expectStatus(t, resp, 200)
	version := decodeJSON(t, resp)
	if version["name"] != "latest" {
		t.Fatalf("expected latest image version, got %v", version["name"])
	}
	// osDiskImage and dataDiskImages sit directly under properties.
	props, _ := version["properties"].(map[string]interface{})
	osDiskImage, _ := props["osDiskImage"].(map[string]interface{})
	if osDiskImage["operatingSystem"] != "Linux" {
		t.Fatalf("expected properties.osDiskImage.operatingSystem Linux, got %v", version["properties"])
	}
	if _, ok := props["dataDiskImages"].([]interface{}); !ok {
		t.Fatalf("expected properties.dataDiskImages to be a list, got %v", version["properties"])
	}
	if _, ok := props["storageProfile"]; ok {
		t.Fatalf("image properties must not be wrapped in storageProfile, got %v", version["properties"])
	}
}

func decodeJSONArray(t *testing.T, resp *http.Response) []interface{} {
	t.Helper()
	var items []interface{}
	if err := json.NewDecoder(resp.Body).Decode(&items); err != nil {
		t.Fatalf("expected a bare JSON array: %v", err)
	}
	return items
}

func listValue(t *testing.T, body map[string]interface{}) []interface{} {
	t.Helper()
	items, ok := body["value"].([]interface{})
	if !ok {
		t.Fatalf("expected value to be a list, got %T", body["value"])
	}
	return items
}

func itemName(t *testing.T, items []interface{}, index int) string {
	t.Helper()
	if len(items) <= index {
		t.Fatalf("expected at least %d items, got %d", index+1, len(items))
	}
	item, ok := items[index].(map[string]interface{})
	if !ok {
		t.Fatalf("expected item %d to be an object, got %T", index, items[index])
	}
	name, ok := item["name"].(string)
	if !ok {
		t.Fatalf("expected item %d name to be a string, got %T", index, item["name"])
	}
	return name
}

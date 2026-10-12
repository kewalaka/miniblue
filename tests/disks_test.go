package tests

import "testing"

func TestManagedDiskLifecyclePatchAccessAndLists(t *testing.T) {
	ts := setupServer()
	defer ts.Close()

	av := "?api-version=2023-10-02"
	subBase := ts.URL + "/subscriptions/sub1"
	disks := subBase + "/resourceGroups/rg1/providers/Microsoft.Compute/disks"

	resp := doRequest(t, "PUT", disks+"/disk1"+av,
		`{"location":"eastus","sku":{"name":"Premium_LRS"},"properties":{"creationData":{"createOption":"Empty"},"diskSizeGB":64}}`)
	expectStatus(t, resp, 201)
	disk := decodeJSON(t, resp)
	resp.Body.Close()
	if disk["type"] != "Microsoft.Compute/disks" {
		t.Fatalf("expected disk type, got %v", disk["type"])
	}
	props := disk["properties"].(map[string]interface{})
	if props["provisioningState"] != "Succeeded" {
		t.Fatalf("expected provisioningState=Succeeded")
	}
	if props["diskState"] != "Unattached" {
		t.Fatalf("expected diskState=Unattached, got %v", props["diskState"])
	}
	creationData := props["creationData"].(map[string]interface{})
	if creationData["createOption"] != "Empty" {
		t.Fatalf("expected createOption=Empty, got %v", creationData["createOption"])
	}
	if props["diskSizeGB"] != float64(64) {
		t.Fatalf("expected diskSizeGB=64, got %v", props["diskSizeGB"])
	}

	vmID := "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Compute/virtualMachines/vm1"
	resp = doRequest(t, "PATCH", disks+"/disk1"+av,
		`{"tags":{"env":"test"},"properties":{"managedBy":"`+vmID+`"}}`)
	expectStatus(t, resp, 200)
	patched := decodeJSON(t, resp)
	resp.Body.Close()
	patchedProps := patched["properties"].(map[string]interface{})
	if patchedProps["diskState"] != "Attached" {
		t.Fatalf("expected diskState=Attached, got %v", patchedProps["diskState"])
	}
	if patchedProps["managedBy"] != vmID {
		t.Fatalf("expected managedBy VM id, got %v", patchedProps["managedBy"])
	}
	if tags := patched["tags"].(map[string]interface{}); tags["env"] != "test" {
		t.Fatalf("expected patched tags, got %v", tags)
	}

	resp = doRequest(t, "GET", disks+"/disk1"+av, "")
	expectStatus(t, resp, 200)
	resp.Body.Close()

	resp = doRequest(t, "POST", disks+"/disk1/beginGetAccess"+av,
		`{"access":"Read","durationInSeconds":3600}`)
	expectStatus(t, resp, 200)
	access := decodeJSON(t, resp)
	resp.Body.Close()
	if access["accessSAS"] == "" {
		t.Fatalf("expected fake SAS accessSAS")
	}

	resp = doRequest(t, "POST", disks+"/disk1/endGetAccess"+av, `{}`)
	expectStatus(t, resp, 200)
	resp.Body.Close()

	resp = doRequest(t, "PUT", subBase+"/resourceGroups/rg2/providers/Microsoft.Compute/disks/disk2"+av,
		`{"location":"westus","properties":{"creationData":{"createOption":"Copy","sourceResourceId":"/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Compute/disks/disk1"}}}`)
	expectStatus(t, resp, 201)
	resp.Body.Close()

	resp = doRequest(t, "GET", disks+av, "")
	expectStatus(t, resp, 200)
	rgList := decodeJSON(t, resp)
	resp.Body.Close()
	if items := rgList["value"].([]interface{}); len(items) != 1 {
		t.Fatalf("expected 1 disk in resource group, got %d", len(items))
	}

	resp = doRequest(t, "GET", subBase+"/providers/Microsoft.Compute/disks"+av, "")
	expectStatus(t, resp, 200)
	subList := decodeJSON(t, resp)
	resp.Body.Close()
	if items := subList["value"].([]interface{}); len(items) != 2 {
		t.Fatalf("expected 2 disks in subscription, got %d", len(items))
	}

	resp = doRequest(t, "PATCH", disks+"/disk1"+av, `{"properties":{"managedBy":""}}`)
	expectStatus(t, resp, 200)
	detached := decodeJSON(t, resp)
	resp.Body.Close()
	if detached["properties"].(map[string]interface{})["diskState"] != "Unattached" {
		t.Fatalf("expected detached diskState=Unattached")
	}

	resp = doRequest(t, "DELETE", disks+"/disk1"+av, "")
	expectStatus(t, resp, 202)
	resp.Body.Close()
	resp = doRequest(t, "GET", disks+"/disk1"+av, "")
	expectStatus(t, resp, 404)
	resp.Body.Close()
}

func TestManagedDiskUploadStateAndResourceGroupCascade(t *testing.T) {
	ts := setupServer()
	defer ts.Close()

	av := "?api-version=2023-10-02"
	subBase := ts.URL + "/subscriptions/sub1"
	rg := subBase + "/resourceGroups/rgdisk"
	disks := subBase + "/resourceGroups/rgdisk/providers/Microsoft.Compute/disks"

	resp := doRequest(t, "PUT", rg+av, `{"location":"eastus"}`)
	expectStatus(t, resp, 201)
	resp.Body.Close()

	resp = doRequest(t, "PUT", disks+"/upload"+av,
		`{"location":"eastus","properties":{"creationData":{"createOption":"Upload"},"diskSizeGB":10}}`)
	expectStatus(t, resp, 201)
	disk := decodeJSON(t, resp)
	resp.Body.Close()
	if disk["properties"].(map[string]interface{})["diskState"] != "ReadyToUpload" {
		t.Fatalf("expected upload diskState=ReadyToUpload")
	}

	resp = doRequest(t, "DELETE", rg+av, "")
	expectStatus(t, resp, 202)
	resp.Body.Close()

	resp = doRequest(t, "GET", disks+"/upload"+av, "")
	expectStatus(t, resp, 404)
	resp.Body.Close()
}

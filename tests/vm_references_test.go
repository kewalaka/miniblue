package tests

import "testing"

// The test file name must not end in an architecture suffix such as _arm:
// Go would silently exclude it from amd64/arm64 builds.

func TestVirtualMachineFromImageRejectsExistingDiskName(t *testing.T) {
	ts := setupVMServer(t)
	defer ts.Close()
	createRG(t, ts, "sub1", "rg1")

	av := "?api-version=2023-09-01"
	compute := ts.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Compute"
	vmBody := func(osDiskName string) string {
		return `{"location":"eastus","properties":{"storageProfile":{
			"imageReference":{"publisher":"Canonical","offer":"0001-com-ubuntu-server-jammy","sku":"22_04-lts","version":"latest"},
			"osDisk":{"name":"` + osDiskName + `","createOption":"FromImage"}}}}`
	}
	getDisk := func(name string) map[string]interface{} {
		t.Helper()
		resp := doRequest(t, "GET", compute+"/disks/"+name+av, "")
		expectStatus(t, resp, 200)
		defer resp.Body.Close()
		return decodeJSON(t, resp)
	}

	resp := doRequest(t, "PUT", compute+"/disks/user-disk"+av,
		`{"location":"eastus","tags":{"owner":"user"},"properties":{"creationData":{"createOption":"Empty"},"diskSizeGB":64}}`)
	expectStatus(t, resp, 200)
	resp.Body.Close()
	resp = doRequest(t, "PUT", compute+"/virtualMachines/vm-a"+av, vmBody("vm-a-os"))
	expectStatus(t, resp, 201)
	resp.Body.Close()

	// Azure rejects FromImage onto a disk that already exists, whoever created it.
	for _, diskName := range []string{"user-disk", "vm-a-os"} {
		resp = doRequest(t, "PUT", compute+"/virtualMachines/vm-b"+av, vmBody(diskName))
		expectStatus(t, resp, 409)
		if code := decodeError(t, resp).Error.Code; code != "OperationNotAllowed" {
			t.Fatalf("expected OperationNotAllowed for disk %s, got %q", diskName, code)
		}
		resp.Body.Close()
		resp = doRequest(t, "GET", compute+"/virtualMachines/vm-b"+av, "")
		expectStatus(t, resp, 404)
		resp.Body.Close()
	}

	userDisk := getDisk("user-disk")
	userProps := userDisk["properties"].(map[string]interface{})
	if userProps["diskSizeGB"] != float64(64) || userDisk["tags"].(map[string]interface{})["owner"] != "user" {
		t.Fatalf("expected user disk to be untouched, got %v", userDisk)
	}
	if userProps["diskState"] != "Unattached" {
		t.Fatalf("expected user disk to stay unattached, got %v", userProps["diskState"])
	}

	// A re-PUT of the owning VM is an update, not a conflict.
	resp = doRequest(t, "PUT", compute+"/virtualMachines/vm-a"+av, vmBody("vm-a-os"))
	expectStatus(t, resp, 200)
	resp.Body.Close()
	vmAID := "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Compute/virtualMachines/vm-a"
	if got := getDisk("vm-a-os")["properties"].(map[string]interface{})["managedBy"]; got != vmAID {
		t.Fatalf("expected vm-a-os to stay attached to vm-a, got %v", got)
	}

	resp = doRequest(t, "DELETE", compute+"/virtualMachines/vm-a"+av, "")
	expectStatus(t, resp, 204)
	resp.Body.Close()
	resp = doRequest(t, "GET", compute+"/disks/vm-a-os"+av, "")
	expectStatus(t, resp, 404)
	resp.Body.Close()
	getDisk("user-disk")
}

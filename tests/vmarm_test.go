package tests

import (
	"fmt"
	"testing"
)

// ARM-fidelity tests for the VM handler: replay of what the client sent,
// defaults Azure adds on GET, and disk/NIC back-references. They run in stub
// mode (see setupVMServer).

func TestVirtualMachineLifecycleReferencesActionsAndSensitiveFields(t *testing.T) {
	ts := setupVMServer(t)
	defer ts.Close()
	createRG(t, ts, "sub1", "rg1")

	av := "?api-version=2023-09-01"
	sub := ts.URL + "/subscriptions/sub1"
	net := sub + "/resourceGroups/rg1/providers/Microsoft.Network"
	compute := sub + "/resourceGroups/rg1/providers/Microsoft.Compute"
	nicID := "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Network/networkInterfaces/nic1"
	dataDiskID := "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Compute/disks/datadisk1"
	vmID := "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Compute/virtualMachines/vm1"

	resp := doRequest(t, "PUT", net+"/networkInterfaces/nic1"+av,
		`{"location":"eastus","properties":{"ipConfigurations":[{"name":"ipconfig1","properties":{"privateIPAddress":"10.0.0.10","privateIPAllocationMethod":"Static"}}]}}`)
	expectStatus(t, resp, 201)
	resp.Body.Close()
	resp = doRequest(t, "PUT", compute+"/disks/datadisk1"+av,
		`{"location":"eastus","properties":{"creationData":{"createOption":"Empty"},"diskSizeGB":64}}`)
	expectStatus(t, resp, 200)
	resp.Body.Close()

	vmBody := `{
		"location":"eastus",
		"tags":{"env":"test"},
		"properties":{
			"hardwareProfile":{"vmSize":"Standard_B1s"},
			"storageProfile":{
				"imageReference":{"publisher":"Canonical","offer":"0001-com-ubuntu-server-jammy","sku":"22_04-lts","version":"latest"},
				"osDisk":{"createOption":"FromImage"},
				"dataDisks":[{"lun":0,"name":"datadisk1","createOption":"Attach","managedDisk":{"id":"` + dataDiskID + `"}}]
			},
			"osProfile":{
				"computerName":"vm1",
				"adminUsername":"azureuser",
				"adminPassword":"super-secret",
				"linuxConfiguration":{"ssh":{"publicKeys":[{"path":"/home/azureuser/.ssh/authorized_keys","keyData":"ssh-rsa AAA"}]}}
			},
			"networkProfile":{"networkInterfaces":[{"id":"` + nicID + `","properties":{"primary":true}}]}
		}
	}`
	resp = doRequest(t, "PUT", compute+"/virtualMachines/vm1"+av, vmBody)
	expectStatus(t, resp, 201)
	vm := decodeJSON(t, resp)
	resp.Body.Close()
	if vm["id"] != vmID {
		t.Fatalf("expected VM id %s, got %v", vmID, vm["id"])
	}
	assertVMResponseShape(t, vm)
	autoOSDisk := vm["properties"].(map[string]interface{})["storageProfile"].(map[string]interface{})["osDisk"].(map[string]interface{})
	autoOSDiskName := autoOSDisk["name"].(string)

	resp = doRequest(t, "GET", compute+"/virtualMachines/vm1"+av, "")
	expectStatus(t, resp, 200)
	vm = decodeJSON(t, resp)
	resp.Body.Close()
	assertVMResponseShape(t, vm)

	resp = doRequest(t, "GET", net+"/networkInterfaces/nic1"+av, "")
	expectStatus(t, resp, 200)
	nic := decodeJSON(t, resp)
	resp.Body.Close()
	if got := nic["properties"].(map[string]interface{})["virtualMachine"].(map[string]interface{})["id"]; got != vmID {
		t.Fatalf("expected NIC virtualMachine reference %s, got %v", vmID, got)
	}

	resp = doRequest(t, "GET", compute+"/disks/"+autoOSDiskName+av, "")
	expectStatus(t, resp, 200)
	autoDisk := decodeJSON(t, resp)
	resp.Body.Close()
	autoDiskProps := autoDisk["properties"].(map[string]interface{})
	if autoDiskProps["osType"] != "Linux" {
		t.Fatalf("expected auto disk osType Linux, got %v", autoDiskProps["osType"])
	}
	if autoDiskProps["diskSizeGB"] != float64(30) {
		t.Fatalf("expected auto disk diskSizeGB 30, got %v", autoDiskProps["diskSizeGB"])
	}
	if autoDiskProps["creationData"].(map[string]interface{})["createOption"] != "FromImage" {
		t.Fatalf("expected auto disk createOption FromImage, got %v", autoDiskProps["creationData"])
	}
	if autoDiskProps["managedBy"] != vmID {
		t.Fatalf("expected auto disk managedBy %s, got %v", vmID, autoDiskProps["managedBy"])
	}
	if autoDiskProps["diskState"] != "Attached" {
		t.Fatalf("expected auto disk state Attached, got %v", autoDiskProps["diskState"])
	}
	if autoDisk["sku"].(map[string]interface{})["name"] != "Standard_LRS" {
		t.Fatalf("expected auto disk sku.name Standard_LRS, got %v", autoDisk["sku"])
	}

	resp = doRequest(t, "GET", sub+"/providers/Microsoft.Compute/disks"+av, "")
	expectStatus(t, resp, 200)
	subDiskList := decodeJSON(t, resp)
	resp.Body.Close()
	if !containsDiskName(subDiskList["value"].([]interface{}), autoOSDiskName) {
		t.Fatalf("expected subscription disk list to include auto os disk %s", autoOSDiskName)
	}

	for _, diskName := range []string{"datadisk1"} {
		resp = doRequest(t, "GET", compute+"/disks/"+diskName+av, "")
		expectStatus(t, resp, 200)
		disk := decodeJSON(t, resp)
		resp.Body.Close()
		props := disk["properties"].(map[string]interface{})
		if props["managedBy"] != vmID || props["diskState"] != "Attached" {
			t.Fatalf("expected %s to be attached to VM, got managedBy=%v state=%v", diskName, props["managedBy"], props["diskState"])
		}
	}

	resp = doRequest(t, "PATCH", compute+"/virtualMachines/vm1"+av,
		`{"tags":{"patched":"true"},"properties":{"hardwareProfile":{"vmSize":"Standard_B2s"}}}`)
	expectStatus(t, resp, 200)
	patched := decodeJSON(t, resp)
	resp.Body.Close()
	if patched["tags"].(map[string]interface{})["patched"] != "true" {
		t.Fatalf("expected patched tags")
	}
	if patched["properties"].(map[string]interface{})["hardwareProfile"].(map[string]interface{})["vmSize"] != "Standard_B2s" {
		t.Fatalf("expected patched vmSize")
	}
	assertVMResponseShape(t, patched)

	resp = doRequest(t, "POST", compute+"/virtualMachines/vm1/powerOff"+av, `{}`)
	expectStatus(t, resp, 200)
	resp.Body.Close()
	assertVMPowerState(t, compute+"/virtualMachines/vm1/instanceView"+av, "PowerState/stopped")
	for _, step := range []struct{ action, state string }{
		{"start", "PowerState/running"},
		{"restart", "PowerState/running"},
		{"deallocate", "PowerState/deallocated"},
		{"redeploy", "PowerState/running"},
	} {
		resp = doRequest(t, "POST", compute+"/virtualMachines/vm1/"+step.action+av, `{}`)
		expectStatus(t, resp, 200)
		resp.Body.Close()
		assertVMPowerState(t, compute+"/virtualMachines/vm1/instanceView"+av, step.state)
	}

	resp = doRequest(t, "PUT", compute+"/virtualMachines/vm1/extensions/customScript"+av,
		`{"location":"eastus","properties":{"publisher":"Microsoft.Azure.Extensions","type":"CustomScript","typeHandlerVersion":"2.1","settings":{"commandToExecute":"echo ok"},"protectedSettings":{"token":"s3cret"}}}`)
	expectStatus(t, resp, 201)
	resp.Body.Close()
	resp = doRequest(t, "GET", compute+"/virtualMachines/vm1/extensions"+av, "")
	expectStatus(t, resp, 200)
	extensions := decodeJSON(t, resp)
	resp.Body.Close()
	if len(extensions["value"].([]interface{})) != 1 {
		t.Fatalf("expected one VM extension")
	}
	if ext := extensions["value"].([]interface{})[0].(map[string]interface{}); ext["properties"].(map[string]interface{})["protectedSettings"] != nil {
		t.Fatalf("protectedSettings must never be returned, got %v", ext["properties"])
	}

	resp = doRequest(t, "GET", compute+"/virtualMachines"+av, "")
	expectStatus(t, resp, 200)
	rgList := decodeJSON(t, resp)
	resp.Body.Close()
	if len(rgList["value"].([]interface{})) != 1 {
		t.Fatalf("expected one VM in resource group")
	}
	resp = doRequest(t, "GET", sub+"/providers/Microsoft.Compute/virtualMachines"+av, "")
	expectStatus(t, resp, 200)
	subList := decodeJSON(t, resp)
	resp.Body.Close()
	if len(subList["value"].([]interface{})) != 1 {
		t.Fatalf("expected one VM in subscription")
	}

	resp = doRequest(t, "DELETE", compute+"/virtualMachines/vm1"+av, "")
	expectStatus(t, resp, 204)
	resp.Body.Close()
	resp = doRequest(t, "GET", compute+"/virtualMachines/vm1"+av, "")
	expectStatus(t, resp, 404)
	resp.Body.Close()
	resp = doRequest(t, "GET", compute+"/disks/"+autoOSDiskName+av, "")
	expectStatus(t, resp, 404)
	resp.Body.Close()
	resp = doRequest(t, "GET", net+"/networkInterfaces/nic1"+av, "")
	expectStatus(t, resp, 200)
	nic = decodeJSON(t, resp)
	resp.Body.Close()
	if _, ok := nic["properties"].(map[string]interface{})["virtualMachine"]; ok {
		t.Fatalf("expected VM delete to clear NIC virtualMachine reference")
	}
	resp = doRequest(t, "GET", compute+"/disks/datadisk1"+av, "")
	expectStatus(t, resp, 200)
	disk := decodeJSON(t, resp)
	resp.Body.Close()
	props := disk["properties"].(map[string]interface{})
	managedBy, _ := props["managedBy"].(string)
	if managedBy != "" || props["diskState"] != "Unattached" {
		t.Fatalf("expected VM delete to detach disk, got managedBy=%v state=%v", props["managedBy"], props["diskState"])
	}
}

func assertVMResponseShape(t *testing.T, vm map[string]interface{}) {
	t.Helper()
	props := vm["properties"].(map[string]interface{})
	osProfile := props["osProfile"].(map[string]interface{})
	if _, ok := osProfile["adminPassword"]; ok {
		t.Fatalf("GET-style VM response must not include adminPassword")
	}
	linuxConfig := osProfile["linuxConfiguration"].(map[string]interface{})
	ssh := linuxConfig["ssh"].(map[string]interface{})
	publicKeys, ok := ssh["publicKeys"].([]interface{})
	if !ok || len(publicKeys) != 1 {
		t.Fatalf("GET-style VM response must include ssh publicKeys, got %v", ssh["publicKeys"])
	}
	publicKey := publicKeys[0].(map[string]interface{})
	if publicKey["path"] != "/home/azureuser/.ssh/authorized_keys" || publicKey["keyData"] != "ssh-rsa AAA" {
		t.Fatalf("unexpected ssh publicKeys payload: %v", publicKey)
	}
}

func TestVirtualMachineFromImageDefaults(t *testing.T) {
	ts := setupVMServer(t)
	defer ts.Close()
	createRG(t, ts, "sub1", "rg1")

	av := "?api-version=2023-09-01"
	compute := ts.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Compute"

	vmBody := `{
		"location":"eastus",
		"properties":{
			"additionalCapabilities":{},
			"storageProfile":{
				"imageReference":{"publisher":"Canonical","offer":"0001-com-ubuntu-server-jammy","sku":"22_04-lts","version":"latest"},
				"osDisk":{"createOption":"FromImage"}
			},
			"osProfile":{
				"computerName":"vm-defaults",
				"adminUsername":"azureuser",
				"linuxConfiguration":{"ssh":{"publicKeys":[{"path":"/home/azureuser/.ssh/authorized_keys","keyData":"ssh-rsa AAA"}]}}
			}
		}
	}`
	resp := doRequest(t, "PUT", compute+"/virtualMachines/vm-defaults"+av, vmBody)
	expectStatus(t, resp, 201)
	vm := decodeJSON(t, resp)
	resp.Body.Close()
	assertVMResponseShape(t, vm)

	props := vm["properties"].(map[string]interface{})
	storageProfile := props["storageProfile"].(map[string]interface{})
	osDisk := storageProfile["osDisk"].(map[string]interface{})

	if osDisk["diskSizeGB"] != float64(30) {
		t.Fatalf("expected default Linux osDisk.diskSizeGB to be 30, got %v", osDisk["diskSizeGB"])
	}
	osDiskName, _ := osDisk["name"].(string)
	if osDiskName == "" {
		t.Fatalf("expected osDisk.name to be generated")
	}
	managedDisk := osDisk["managedDisk"].(map[string]interface{})
	expectedManagedDiskID := "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Compute/disks/" + osDiskName
	if managedDisk["id"] != expectedManagedDiskID {
		t.Fatalf("expected generated managedDisk id %s, got %v", expectedManagedDiskID, managedDisk["id"])
	}
	if _, ok := props["vmId"].(string); !ok || props["vmId"] == "" {
		t.Fatalf("expected properties.vmId to be generated, got %v", props["vmId"])
	}
	if _, present := props["additionalCapabilities"]; present {
		t.Fatalf("expected additionalCapabilities key to be absent when PUT with empty object, got %v", props["additionalCapabilities"])
	}
}

func TestVirtualMachineAdditionalCapabilitiesReplay(t *testing.T) {
	ts := setupVMServer(t)
	defer ts.Close()
	createRG(t, ts, "sub1", "rg1")

	av := "?api-version=2023-09-01"
	compute := ts.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Compute"

	baseProps := `"storageProfile":{
		"imageReference":{"publisher":"Canonical","offer":"0001-com-ubuntu-server-jammy","sku":"22_04-lts","version":"latest"},
		"osDisk":{"createOption":"FromImage"}
	},
	"osProfile":{
		"computerName":"vm-ac",
		"adminUsername":"azureuser",
		"linuxConfiguration":{"ssh":{"publicKeys":[{"path":"/home/azureuser/.ssh/authorized_keys","keyData":"ssh-rsa AAA"}]}}
	}`

	cases := []struct {
		name        string
		acPayload   string // JSON for additionalCapabilities including the leading comma, or empty string to omit
		wantPresent bool
		wantFields  map[string]interface{}
	}{
		{
			name:        "omitted",
			acPayload:   "",
			wantPresent: false,
		},
		{
			name:        "empty_object",
			acPayload:   `,"additionalCapabilities":{}`,
			wantPresent: false,
		},
		{
			name:        "only_ultraSSD_false",
			acPayload:   `,"additionalCapabilities":{"ultraSSDEnabled":false}`,
			wantPresent: true,
			wantFields:  map[string]interface{}{"ultraSSDEnabled": false},
		},
		{
			name:        "both_false",
			acPayload:   `,"additionalCapabilities":{"ultraSSDEnabled":false,"hibernationEnabled":false}`,
			wantPresent: true,
			wantFields:  map[string]interface{}{"ultraSSDEnabled": false, "hibernationEnabled": false},
		},
		{
			name:        "only_hibernation_true",
			acPayload:   `,"additionalCapabilities":{"hibernationEnabled":true}`,
			wantPresent: true,
			wantFields:  map[string]interface{}{"hibernationEnabled": true},
		},
	}

	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			vmName := fmt.Sprintf("vm-ac-%d", i)
			body := `{"location":"eastus","properties":{` + baseProps + tc.acPayload + `}}`
			resp := doRequest(t, "PUT", compute+"/virtualMachines/"+vmName+av, body)
			expectStatus(t, resp, 201)
			resp.Body.Close()

			resp = doRequest(t, "GET", compute+"/virtualMachines/"+vmName+av, "")
			expectStatus(t, resp, 200)
			vm := decodeJSON(t, resp)
			resp.Body.Close()

			props := vm["properties"].(map[string]interface{})
			ac, present := props["additionalCapabilities"]
			if present != tc.wantPresent {
				t.Fatalf("additionalCapabilities presence mismatch: want %v, got %v (value %v)", tc.wantPresent, present, ac)
			}
			if !tc.wantPresent {
				return
			}
			acMap := ac.(map[string]interface{})
			if len(acMap) != len(tc.wantFields) {
				t.Fatalf("additionalCapabilities should have %d field(s), got %v", len(tc.wantFields), acMap)
			}
			for k, v := range tc.wantFields {
				if acMap[k] != v {
					t.Fatalf("additionalCapabilities[%q] = %v, want %v", k, acMap[k], v)
				}
			}
		})
	}
}

func TestVirtualMachinePatchPreservesAdditionalCapabilities(t *testing.T) {
	ts := setupVMServer(t)
	defer ts.Close()
	createRG(t, ts, "sub1", "rg1")

	av := "?api-version=2023-09-01"
	compute := ts.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Compute"

	body := `{
		"location":"eastus",
		"properties":{
			"additionalCapabilities":{"ultraSSDEnabled":true},
			"storageProfile":{
				"imageReference":{"publisher":"Canonical","offer":"0001-com-ubuntu-server-jammy","sku":"22_04-lts","version":"latest"},
				"osDisk":{"createOption":"FromImage"}
			},
			"osProfile":{
				"computerName":"vm-ac-patch",
				"adminUsername":"azureuser",
				"linuxConfiguration":{"ssh":{"publicKeys":[{"path":"/home/azureuser/.ssh/authorized_keys","keyData":"ssh-rsa AAA"}]}}
			}
		}
	}`
	resp := doRequest(t, "PUT", compute+"/virtualMachines/vm-ac-patch"+av, body)
	expectStatus(t, resp, 201)
	resp.Body.Close()

	// PATCH a tag, do not mention additionalCapabilities: the existing
	// single-field map must be preserved with no auto-fill of the other key.
	resp = doRequest(t, "PATCH", compute+"/virtualMachines/vm-ac-patch"+av, `{"tags":{"env":"prod"}}`)
	expectStatus(t, resp, 200)
	resp.Body.Close()

	resp = doRequest(t, "GET", compute+"/virtualMachines/vm-ac-patch"+av, "")
	expectStatus(t, resp, 200)
	vm := decodeJSON(t, resp)
	resp.Body.Close()

	props := vm["properties"].(map[string]interface{})
	ac, ok := props["additionalCapabilities"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected additionalCapabilities to be present after PATCH, got %v", props["additionalCapabilities"])
	}
	if len(ac) != 1 || ac["ultraSSDEnabled"] != true {
		t.Fatalf("expected additionalCapabilities to be {ultraSSDEnabled:true} after PATCH, got %v", ac)
	}
}

func TestVirtualMachineDeleteDoesNotRemoveAttachedUserDisk(t *testing.T) {
	ts := setupVMServer(t)
	defer ts.Close()
	createRG(t, ts, "sub1", "rg1")

	av := "?api-version=2023-09-01"
	sub := ts.URL + "/subscriptions/sub1"
	net := sub + "/resourceGroups/rg1/providers/Microsoft.Network"
	compute := sub + "/resourceGroups/rg1/providers/Microsoft.Compute"
	nicID := "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Network/networkInterfaces/nic-attach"
	userOSDiskID := "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Compute/disks/user-osdisk"

	resp := doRequest(t, "PUT", net+"/networkInterfaces/nic-attach"+av,
		`{"location":"eastus","properties":{"ipConfigurations":[{"name":"ipconfig1","properties":{"privateIPAllocationMethod":"Dynamic"}}]}}`)
	expectStatus(t, resp, 201)
	resp.Body.Close()

	resp = doRequest(t, "PUT", compute+"/disks/user-osdisk"+av,
		`{"location":"eastus","properties":{"creationData":{"createOption":"Empty"},"diskSizeGB":64}}`)
	expectStatus(t, resp, 200)
	resp.Body.Close()

	resp = doRequest(t, "PUT", compute+"/virtualMachines/vm-attach"+av, `{
		"location":"eastus",
		"properties":{
			"storageProfile":{
				"osDisk":{"name":"user-osdisk","createOption":"Attach","managedDisk":{"id":"`+userOSDiskID+`"}}
			},
			"osProfile":{"computerName":"vm-attach","adminUsername":"azureuser"},
			"networkProfile":{"networkInterfaces":[{"id":"`+nicID+`"}]}
		}
	}`)
	expectStatus(t, resp, 201)
	resp.Body.Close()

	resp = doRequest(t, "DELETE", compute+"/virtualMachines/vm-attach"+av, "")
	expectStatus(t, resp, 204)
	resp.Body.Close()

	resp = doRequest(t, "GET", compute+"/disks/user-osdisk"+av, "")
	expectStatus(t, resp, 200)
	disk := decodeJSON(t, resp)
	resp.Body.Close()
	props := disk["properties"].(map[string]interface{})
	if managedBy, _ := props["managedBy"].(string); managedBy != "" {
		t.Fatalf("expected attached user disk managedBy cleared after VM delete, got %v", props["managedBy"])
	}
	if props["diskState"] != "Unattached" {
		t.Fatalf("expected attached user disk to remain and be detached, got state=%v", props["diskState"])
	}
}

func assertVMPowerState(t *testing.T, instanceViewURL, want string) {
	t.Helper()
	resp := doRequest(t, "GET", instanceViewURL, "")
	expectStatus(t, resp, 200)
	view := decodeJSON(t, resp)
	resp.Body.Close()
	statuses := view["statuses"].([]interface{})
	if got := statuses[len(statuses)-1].(map[string]interface{})["code"]; got != want {
		t.Fatalf("expected %s, got %v", want, statuses)
	}
}

func containsDiskName(items []interface{}, name string) bool {
	for _, item := range items {
		if disk, ok := item.(map[string]interface{}); ok && disk["name"] == name {
			return true
		}
	}
	return false
}

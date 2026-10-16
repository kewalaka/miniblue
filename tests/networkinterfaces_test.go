package tests

import "testing"

func TestNetworkInterfaceLifecycleAndReferences(t *testing.T) {
	ts := setupServer()
	defer ts.Close()

	av := "?api-version=2023-09-01"
	base := ts.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Network"
	subnetID := "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Network/virtualNetworks/vnet1/subnets/web"
	pipID := "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Network/publicIPAddresses/pip1"
	nsgID := "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Network/networkSecurityGroups/nsg1"
	vmID := "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Compute/virtualMachines/vm1"

	doRequest(t, "PUT", base+"/virtualNetworks/vnet1"+av,
		`{"location":"eastus","properties":{"addressSpace":{"addressPrefixes":["10.0.0.0/16"]}}}`).Body.Close()
	doRequest(t, "PUT", base+"/virtualNetworks/vnet1/subnets/web"+av,
		`{"properties":{"addressPrefix":"10.0.1.0/24"}}`).Body.Close()
	doRequest(t, "PUT", base+"/publicIPAddresses/pip1"+av, `{"location":"eastus"}`).Body.Close()
	doRequest(t, "PUT", base+"/networkSecurityGroups/nsg1"+av, `{"location":"eastus"}`).Body.Close()

	body := `{
		"location":"eastus",
		"tags":{"env":"test"},
		"properties":{
			"networkSecurityGroup":{"id":"` + nsgID + `"},
			"virtualMachine":{"id":"` + vmID + `"},
			"ipConfigurations":[{
				"name":"ipconfig1",
				"properties":{
					"privateIPAllocationMethod":"Dynamic",
					"subnet":{"id":"` + subnetID + `"},
					"publicIPAddress":{"id":"` + pipID + `"},
					"primary":true
				}
			}]
		}
	}`
	resp := doRequest(t, "PUT", base+"/networkInterfaces/nic1"+av, body)
	expectStatus(t, resp, 201)
	nic := decodeJSON(t, resp)
	resp.Body.Close()
	if nic["type"] != "Microsoft.Network/networkInterfaces" {
		t.Fatalf("expected NIC type, got %v", nic["type"])
	}
	props := nic["properties"].(map[string]interface{})
	if props["provisioningState"] != "Succeeded" {
		t.Fatalf("expected provisioningState=Succeeded")
	}
	if vm := props["virtualMachine"].(map[string]interface{}); vm["id"] != vmID {
		t.Fatalf("expected virtualMachine id=%s, got %v", vmID, vm["id"])
	}
	ipconfigs := props["ipConfigurations"].([]interface{})
	if len(ipconfigs) != 1 {
		t.Fatalf("expected 1 ipConfiguration, got %d", len(ipconfigs))
	}
	ipconfigProps := ipconfigs[0].(map[string]interface{})["properties"].(map[string]interface{})
	if ipconfigProps["privateIPAddress"] == "" {
		t.Fatalf("expected generated privateIPAddress")
	}

	resp = doRequest(t, "GET", base+"/virtualNetworks/vnet1/subnets/web"+av, "")
	expectStatus(t, resp, 200)
	subnet := decodeJSON(t, resp)
	resp.Body.Close()
	subnetConfigs := subnet["properties"].(map[string]interface{})["ipConfigurations"].([]interface{})
	if len(subnetConfigs) != 1 {
		t.Fatalf("expected subnet ipConfiguration reference, got %d", len(subnetConfigs))
	}

	resp = doRequest(t, "GET", base+"/publicIPAddresses/pip1"+av, "")
	expectStatus(t, resp, 200)
	pip := decodeJSON(t, resp)
	resp.Body.Close()
	if pip["properties"].(map[string]interface{})["ipConfiguration"] == nil {
		t.Fatalf("expected public IP ipConfiguration reference")
	}

	resp = doRequest(t, "GET", base+"/networkSecurityGroups/nsg1"+av, "")
	expectStatus(t, resp, 200)
	nsg := decodeJSON(t, resp)
	resp.Body.Close()
	nsgNics := nsg["properties"].(map[string]interface{})["networkInterfaces"].([]interface{})
	if len(nsgNics) != 1 {
		t.Fatalf("expected NSG network interface reference, got %d", len(nsgNics))
	}

	resp = doRequest(t, "DELETE", base+"/networkInterfaces/nic1"+av, "")
	expectStatus(t, resp, 202)
	resp.Body.Close()
	resp = doRequest(t, "GET", base+"/networkInterfaces/nic1"+av, "")
	expectStatus(t, resp, 404)
	resp.Body.Close()
}

func TestNetworkInterfacePatchAndSubscriptionList(t *testing.T) {
	ts := setupServer()
	defer ts.Close()

	av := "?api-version=2023-09-01"
	base := ts.URL + "/subscriptions/sub1"
	nics := base + "/resourceGroups/rg1/providers/Microsoft.Network/networkInterfaces"

	resp := doRequest(t, "PUT", nics+"/nic1"+av,
		`{"location":"eastus","properties":{"ipConfigurations":[{"name":"ipconfig1","properties":{"privateIPAddress":"10.0.0.10","privateIPAllocationMethod":"Static"}}]}}`)
	expectStatus(t, resp, 201)
	resp.Body.Close()

	resp = doRequest(t, "PATCH", nics+"/nic1"+av,
		`{"tags":{"patched":"true"},"properties":{"enableIPForwarding":true,"virtualMachine":{"id":"/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Compute/virtualMachines/vm1"}}}`)
	expectStatus(t, resp, 200)
	patched := decodeJSON(t, resp)
	resp.Body.Close()
	props := patched["properties"].(map[string]interface{})
	if props["enableIPForwarding"] != true {
		t.Fatalf("expected enableIPForwarding=true")
	}
	if tags := patched["tags"].(map[string]interface{}); tags["patched"] != "true" {
		t.Fatalf("expected patched tag, got %v", tags)
	}
	ipconfigs := props["ipConfigurations"].([]interface{})
	ipconfigProps := ipconfigs[0].(map[string]interface{})["properties"].(map[string]interface{})
	if ipconfigProps["privateIPAddress"] != "10.0.0.10" {
		t.Fatalf("expected patched NIC to preserve private IP, got %v", ipconfigProps["privateIPAddress"])
	}

	doRequest(t, "PUT", base+"/resourceGroups/rg2/providers/Microsoft.Network/networkInterfaces/nic2"+av,
		`{"location":"westus"}`).Body.Close()
	resp = doRequest(t, "GET", base+"/providers/Microsoft.Network/networkInterfaces"+av, "")
	expectStatus(t, resp, 200)
	list := decodeJSON(t, resp)
	resp.Body.Close()
	items := list["value"].([]interface{})
	if len(items) != 2 {
		t.Fatalf("expected 2 NICs across RGs, got %d", len(items))
	}
}

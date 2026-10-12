package tests

import (
	"testing"
)

func TestResourceGroupCascadeDeleteAllServices(t *testing.T) {
	ts := setupServer()
	defer ts.Close()

	av := "?api-version=2023-09-01"
	base := ts.URL + "/subscriptions/sub1"
	rg := base + "/resourceGroups/cascade-rg"
	net := rg + "/providers/Microsoft.Network"
	compute := rg + "/providers/Microsoft.Compute"
	nicID := "/subscriptions/sub1/resourceGroups/cascade-rg/providers/Microsoft.Network/networkInterfaces/nic1"
	osDiskID := "/subscriptions/sub1/resourceGroups/cascade-rg/providers/Microsoft.Compute/disks/osdisk1"

	doRequest(t, "PUT", base+"/resourcegroups/cascade-rg"+av, `{"location":"eastus"}`).Body.Close()

	doRequest(t, "PUT", net+"/virtualNetworks/vnet1"+av,
		`{"location":"eastus","properties":{"addressSpace":{"addressPrefixes":["10.0.0.0/16"]}}}`).Body.Close()
	doRequest(t, "PUT", net+"/virtualNetworks/vnet1/subnets/sub1"+av,
		`{"properties":{"addressPrefix":"10.0.1.0/24"}}`).Body.Close()
	doRequest(t, "PUT", net+"/networkSecurityGroups/nsg1"+av,
		`{"location":"eastus"}`).Body.Close()
	doRequest(t, "PUT", net+"/networkSecurityGroups/nsg1/securityRules/rule1"+av,
		`{"properties":{"priority":100,"direction":"Inbound","access":"Allow","protocol":"Tcp","sourcePortRange":"*","destinationPortRange":"80","sourceAddressPrefix":"*","destinationAddressPrefix":"*"}}`).Body.Close()
	doRequest(t, "PUT", net+"/publicIPAddresses/pip1"+av,
		`{"location":"eastus"}`).Body.Close()
	doRequest(t, "PUT", net+"/networkInterfaces/nic1"+av,
		`{"location":"eastus","properties":{"ipConfigurations":[{"name":"ipconfig1","properties":{"privateIPAllocationMethod":"Dynamic"}}]}}`).Body.Close()
	doRequest(t, "PUT", compute+"/disks/osdisk1"+av,
		`{"location":"eastus","properties":{"creationData":{"createOption":"Empty"},"diskSizeGB":32}}`).Body.Close()
	doRequest(t, "PUT", compute+"/virtualMachines/vm1"+av,
		`{"location":"eastus","properties":{"hardwareProfile":{"vmSize":"Standard_B1s"},"storageProfile":{"osDisk":{"name":"osdisk1","createOption":"Attach","managedDisk":{"id":"`+osDiskID+`"}}},"osProfile":{"computerName":"vm1","adminUsername":"azureuser"},"networkProfile":{"networkInterfaces":[{"id":"`+nicID+`"}]}}}`).Body.Close()
	doRequest(t, "PUT", compute+"/virtualMachines/vm1/extensions/customScript"+av,
		`{"location":"eastus","properties":{"publisher":"Microsoft.Azure.Extensions","type":"CustomScript","typeHandlerVersion":"2.1"}}`).Body.Close()
	doRequest(t, "PUT", net+"/loadBalancers/lb1"+av,
		`{"location":"eastus"}`).Body.Close()
	doRequest(t, "PUT", net+"/applicationGateways/appgw1"+av,
		`{"location":"eastus","properties":{}}`).Body.Close()

	resp := doRequest(t, "DELETE", base+"/resourcegroups/cascade-rg"+av, "")
	expectStatus(t, resp, 202)
	resp.Body.Close()

	checks := []struct {
		name string
		path string
	}{
		{"VNet", net + "/virtualNetworks/vnet1" + av},
		{"Subnet", net + "/virtualNetworks/vnet1/subnets/sub1" + av},
		{"NSG", net + "/networkSecurityGroups/nsg1" + av},
		{"NSG Rule", net + "/networkSecurityGroups/nsg1/securityRules/rule1" + av},
		{"Public IP", net + "/publicIPAddresses/pip1" + av},
		{"Network Interface", net + "/networkInterfaces/nic1" + av},
		{"Managed Disk", compute + "/disks/osdisk1" + av},
		{"Virtual Machine", compute + "/virtualMachines/vm1" + av},
		{"VM Extension", compute + "/virtualMachines/vm1/extensions/customScript" + av},
		{"Load Balancer", net + "/loadBalancers/lb1" + av},
		{"App Gateway", net + "/applicationGateways/appgw1" + av},
	}

	for _, c := range checks {
		resp := doRequest(t, "GET", c.path, "")
		if resp.StatusCode != 404 {
			t.Errorf("%s should be 404 after RG delete, got %d", c.name, resp.StatusCode)
		}
		resp.Body.Close()
	}

	listChecks := []struct {
		name string
		path string
	}{
		{"subscription NICs", base + "/providers/Microsoft.Network/networkInterfaces" + av},
		{"subscription disks", base + "/providers/Microsoft.Compute/disks" + av},
		{"subscription VMs", base + "/providers/Microsoft.Compute/virtualMachines" + av},
	}
	for _, c := range listChecks {
		resp := doRequest(t, "GET", c.path, "")
		expectStatus(t, resp, 200)
		body := decodeJSON(t, resp)
		resp.Body.Close()
		if got := len(body["value"].([]interface{})); got != 0 {
			t.Errorf("%s should be empty after RG delete, got %d", c.name, got)
		}
	}
}

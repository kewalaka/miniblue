package storageaccounts_test

import (
	"net/http"
	"reflect"
	"strings"
	"testing"
)

// seedAccountChildren writes every ARM sub-resource a storage account owns.
func seedAccountChildren(t *testing.T, base string) {
	t.Helper()
	putJSON(t, base+"/managementPolicies/default", map[string]interface{}{
		"properties": map[string]interface{}{"policy": map[string]interface{}{"rules": []interface{}{}}},
	})
	putJSON(t, base+"/blobServices/default", map[string]interface{}{
		"properties": map[string]interface{}{"isVersioningEnabled": true, "cors": corsWithOrigin("https://a.example")},
	})
	putJSON(t, base+"/blobServices/default/containers/c1", map[string]interface{}{})
}

// assertAccountChildrenGone verifies a (re)created account starts out with no
// trace of the sub-resources written by seedAccountChildren.
func assertAccountChildrenGone(t *testing.T, base string) {
	t.Helper()
	getJSON(t, base+"/managementPolicies/default", http.StatusNotFound)
	getJSON(t, base+"/blobServices/default/containers/c1", http.StatusNotFound)
	if items := getJSON(t, base+"/blobServices/default/containers", http.StatusOK)["value"].([]interface{}); len(items) != 0 {
		t.Errorf("containers = %v, want none", items)
	}
	props := properties(t, getJSON(t, base+"/blobServices/default", http.StatusOK))
	if props["isVersioningEnabled"] != false {
		t.Errorf("isVersioningEnabled = %v, want default false", props["isVersioningEnabled"])
	}
	if !reflect.DeepEqual(props["cors"], map[string]interface{}{"corsRules": []interface{}{}}) {
		t.Errorf("cors = %#v, want default empty rules", props["cors"])
	}
}

func TestDeleteStorageAccountRemovesChildren(t *testing.T) {
	serverURL := newStorageServer(t)
	base := createAccount(t, serverURL, "cleanup")
	seedAccountChildren(t, base)

	if status := doDelete(t, base); status != http.StatusOK {
		t.Fatalf("DELETE account status = %d, want %d", status, http.StatusOK)
	}
	getJSON(t, base+"/managementPolicies/default", http.StatusNotFound)

	createAccount(t, serverURL, "cleanup")
	assertAccountChildrenGone(t, base)
}

func TestDeleteResourceGroupRemovesStorageAccountChildren(t *testing.T) {
	serverURL := newStorageServer(t)
	rgURL := serverURL + "/subscriptions/sub1/resourcegroups/rg1?api-version=2021-04-01"
	req, err := http.NewRequest(http.MethodPut, rgURL, strings.NewReader(`{"location":"eastus"}`))
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	base := createAccount(t, serverURL, "rgcleanup")
	seedAccountChildren(t, base)

	if status := doDelete(t, rgURL); status != http.StatusAccepted {
		t.Fatalf("DELETE resource group status = %d, want %d", status, http.StatusAccepted)
	}

	createAccount(t, serverURL, "rgcleanup")
	assertAccountChildrenGone(t, base)
}

func TestManagementPolicyRequiresAccount(t *testing.T) {
	url := newStorageServer(t) + storageAccountsPath + "missing/managementPolicies/default"
	for _, method := range []string{http.MethodGet, http.MethodPut, http.MethodDelete} {
		if status := requestStatus(t, method, url); status != http.StatusNotFound {
			t.Errorf("%s status = %d, want %d", method, status, http.StatusNotFound)
		}
	}
}

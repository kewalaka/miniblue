package storageaccounts_test

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/moabukar/miniblue/internal/server"
)

const storageAccountsPath = "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Storage/storageAccounts/"

func newStorageServer(t *testing.T) string {
	t.Helper()
	ts := httptest.NewServer(server.New().Handler())
	t.Cleanup(ts.Close)
	return ts.URL
}

func createAccount(t *testing.T, serverURL, name string) string {
	t.Helper()
	base := serverURL + storageAccountsPath + name
	putJSON(t, base, map[string]interface{}{"location": "eastus", "kind": "StorageV2"})
	return base
}

func properties(t *testing.T, resource map[string]interface{}) map[string]interface{} {
	t.Helper()
	props, ok := resource["properties"].(map[string]interface{})
	if !ok {
		t.Fatalf("resource has no properties object: %#v", resource)
	}
	return props
}

func corsWithOrigin(origin string) map[string]interface{} {
	return map[string]interface{}{
		"corsRules": []interface{}{
			map[string]interface{}{
				"allowedOrigins":  []interface{}{origin},
				"allowedMethods":  []interface{}{"GET"},
				"allowedHeaders":  []interface{}{"*"},
				"exposedHeaders":  []interface{}{"*"},
				"maxAgeInSeconds": float64(60),
			},
		},
	}
}

// TestPatchServicePropertiesKeepsOmittedProperties verifies PATCH behaves like
// the Azure Storage RP: properties absent from the request body are left as
// they were, rather than being reset to defaults.
func TestPatchServicePropertiesKeepsOmittedProperties(t *testing.T) {
	serverURL := newStorageServer(t)

	cases := []struct {
		service string
		seed    map[string]interface{}
		patch   map[string]interface{}
	}{
		{
			service: "blobServices",
			seed: map[string]interface{}{
				"isVersioningEnabled": true,
				"deleteRetentionPolicy": map[string]interface{}{
					"enabled": true, "days": float64(30), "allowPermanentDelete": false,
				},
				"cors": corsWithOrigin("https://a.example"),
			},
			patch: map[string]interface{}{"cors": corsWithOrigin("https://b.example")},
		},
		{
			service: "fileServices",
			seed: map[string]interface{}{
				"shareDeleteRetentionPolicy": map[string]interface{}{"enabled": true, "days": float64(30)},
				"protocolSettings": map[string]interface{}{
					"smb": map[string]interface{}{"versions": "SMB3.1.1"},
				},
				"cors": corsWithOrigin("https://a.example"),
			},
			patch: map[string]interface{}{"cors": corsWithOrigin("https://b.example")},
		},
		{
			service: "queueServices",
			seed:    map[string]interface{}{"cors": corsWithOrigin("https://a.example")},
		},
		{
			service: "tableServices",
			seed:    map[string]interface{}{"cors": corsWithOrigin("https://a.example")},
		},
	}

	for _, tc := range cases {
		t.Run(tc.service, func(t *testing.T) {
			base := createAccount(t, serverURL, "patch"+tc.service)
			url := base + "/" + tc.service + "/default"

			putJSON(t, url, map[string]interface{}{"properties": tc.seed})

			body := map[string]interface{}{}
			if tc.patch != nil {
				body["properties"] = tc.patch
			}
			patched := patchJSON(t, url, body)
			got := getJSON(t, url, http.StatusOK)

			want := map[string]interface{}{}
			for k, v := range tc.seed {
				want[k] = v
			}
			for k, v := range tc.patch {
				want[k] = v
			}
			for name, resource := range map[string]map[string]interface{}{"PATCH response": patched, "GET": got} {
				props := properties(t, resource)
				for k, v := range want {
					if !reflect.DeepEqual(props[k], v) {
						t.Errorf("%s: properties.%s = %#v, want %#v", name, k, props[k], v)
					}
				}
			}
		})
	}
}

// TestPatchServicePropertiesWithoutStoredRecord verifies a PATCH on an account
// whose service properties were never written starts from the defaults, and
// that the patched record then survives later PATCHes of other properties.
func TestPatchServicePropertiesWithoutStoredRecord(t *testing.T) {
	base := createAccount(t, newStorageServer(t), "patchfresh")
	url := base + "/blobServices/default"

	first := patchJSON(t, url, map[string]interface{}{"properties": map[string]interface{}{"isVersioningEnabled": true}})
	props := properties(t, first)
	if props["isVersioningEnabled"] != true {
		t.Fatalf("isVersioningEnabled = %v, want true", props["isVersioningEnabled"])
	}
	wantRetention := map[string]interface{}{"enabled": true, "days": float64(7), "allowPermanentDelete": false}
	if !reflect.DeepEqual(props["deleteRetentionPolicy"], wantRetention) {
		t.Fatalf("deleteRetentionPolicy = %#v, want default %#v", props["deleteRetentionPolicy"], wantRetention)
	}

	patchJSON(t, url, map[string]interface{}{"properties": map[string]interface{}{"cors": corsWithOrigin("https://a.example")}})
	props = properties(t, getJSON(t, url, http.StatusOK))
	if props["isVersioningEnabled"] != true {
		t.Fatalf("isVersioningEnabled after second PATCH = %v, want true", props["isVersioningEnabled"])
	}
	if !reflect.DeepEqual(props["cors"], corsWithOrigin("https://a.example")) {
		t.Fatalf("cors = %#v, want the patched rules", props["cors"])
	}
}

func TestPatchServicePropertiesAccountNotFound(t *testing.T) {
	serverURL := newStorageServer(t)
	for _, service := range []string{"blobServices", "fileServices", "queueServices", "tableServices"} {
		url := serverURL + storageAccountsPath + "missing/" + service + "/default"
		if status := requestStatus(t, http.MethodPatch, url); status != http.StatusNotFound {
			t.Errorf("PATCH %s status = %d, want %d", service, status, http.StatusNotFound)
		}
	}
}

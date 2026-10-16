package subscriptions

import "testing"

func TestProviderEntryIncludesStorageAccountPreviewAPIVersion(t *testing.T) {
	entry := providerEntry("Microsoft.Storage")
	resourceTypes := entry["resourceTypes"].([]interface{})
	if len(resourceTypes) != 1 {
		t.Fatalf("expected one storage resource type, got %#v", resourceTypes)
	}

	storageAccounts := resourceTypes[0].(map[string]interface{})
	if storageAccounts["resourceType"] != "storageAccounts" {
		t.Fatalf("expected storageAccounts resource type, got %#v", storageAccounts)
	}

	for _, version := range storageAccounts["apiVersions"].([]interface{}) {
		if version == "2022-05-01-preview" {
			return
		}
	}
	t.Fatalf("expected 2022-05-01-preview api-version in %#v", storageAccounts["apiVersions"])
}

package vm

import (
	"reflect"
	"testing"

	"github.com/moabukar/miniblue/internal/store"
)

// NIC and disk handlers encode the stored maps without locking, so VM
// bookkeeping must replace them rather than mutate them in place.
func TestReferencesDoNotMutateStoredResources(t *testing.T) {
	const nicRec, diskRec = "nic:sub1:rg2:nic1", "disk:sub1:rg2:data1"
	vmID := vmARMID("sub1", "rg1", "vm1")
	nicID := "/subscriptions/sub1/resourceGroups/rg2/providers/Microsoft.Network/networkInterfaces/nic1"
	dataDiskID := diskARMID("sub1", "rg2", "data1")

	s := store.New()
	s.Set(nicRec, map[string]interface{}{"id": nicID, "properties": map[string]interface{}{}})
	s.Set(diskRec, map[string]interface{}{"id": dataDiskID, "properties": map[string]interface{}{"diskState": "Unattached"}})
	rec := map[string]interface{}{
		"id": vmID,
		"properties": map[string]interface{}{
			"networkProfile": map[string]interface{}{"networkInterfaces": []interface{}{map[string]interface{}{"id": nicID}}},
			"storageProfile": map[string]interface{}{"dataDisks": []interface{}{map[string]interface{}{"managedDisk": map[string]interface{}{"id": dataDiskID}}}},
		},
	}

	stored := func(key string) map[string]interface{} {
		t.Helper()
		v, ok := s.Get(key)
		if !ok {
			t.Fatalf("%s missing from the store", key)
		}
		return asMap(v)
	}

	for _, step := range []struct {
		name         string
		run          func()
		wantAttached bool
	}{
		{"setReferences", func() { setReferences(s, "sub1", rec) }, true},
		{"clearReferences", func() { clearReferences(s, "sub1", rec) }, false},
	} {
		nicBefore, diskBefore := stored(nicRec), stored(diskRec)
		nicSnapshot, diskSnapshot := recursiveCopy(nicBefore), recursiveCopy(diskBefore)

		step.run()

		if !reflect.DeepEqual(nicBefore, nicSnapshot) || !reflect.DeepEqual(diskBefore, diskSnapshot) {
			t.Fatalf("%s mutated a stored resource in place", step.name)
		}
		nicProps, diskProps := asMap(stored(nicRec)["properties"]), asMap(stored(diskRec)["properties"])
		if attached := idFromRef(nicProps["virtualMachine"]) == vmID; attached != step.wantAttached {
			t.Fatalf("%s: NIC attached=%v, want %v", step.name, attached, step.wantAttached)
		}
		if attached := diskProps["managedBy"] == vmID && diskProps["diskState"] == "Attached"; attached != step.wantAttached {
			t.Fatalf("%s: disk attached=%v, want %v (%v)", step.name, attached, step.wantAttached, diskProps)
		}
	}
}

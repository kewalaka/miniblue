package vm

import (
	"strings"
	"time"

	"github.com/moabukar/miniblue/internal/store"
)

// ---------------------------------------------------------------------------
// Managed disk and NIC bookkeeping
//
// Disks ("disk:") and NICs ("nic:") are owned by the disks and
// networkinterfaces services; the VM keeps their back-references
// (managedBy/diskState, properties.virtualMachine) consistent, and persists
// the auto-created OS disk so it is listable like in Azure.
// ---------------------------------------------------------------------------

func diskKey(sub, rg, name string) string {
	return "disk:" + sub + ":" + rg + ":" + name
}

func requestHasManagedDiskID(request map[string]interface{}) bool {
	osDisk := asMap(asMap(asMap(request["properties"])["storageProfile"])["osDisk"])
	return idFromRef(osDisk["managedDisk"]) != ""
}

// syncDependencies persists the auto-created OS disk and refreshes the
// disk/NIC back-references for a VM record just written to the store.
func syncDependencies(s *store.Store, sub, rg string, requestSuppliedManagedDisk bool, rec map[string]interface{}) {
	ensureAutoManagedOSDisk(s, sub, rg, requestSuppliedManagedDisk, rec)
	clearReferences(s, sub, rec)
	setReferences(s, sub, rec)
}

// removeDependencies is the delete-side counterpart of syncDependencies.
func removeDependencies(s *store.Store, sub, rg string, rec map[string]interface{}) {
	clearReferences(s, sub, rec)
	osDiskName := firstString(asMap(asMap(asMap(rec["properties"])["storageProfile"])["osDisk"])["name"])
	if osDiskName == "" {
		return
	}
	k := diskKey(sub, rg, osDiskName)
	if disk, ok := s.Get(k); ok && strings.EqualFold(firstString(asMap(disk)["_autoCreatedBy"]), firstString(rec["id"])) {
		s.Delete(k)
	}
}

func ensureAutoManagedOSDisk(s *store.Store, sub, rg string, requestSuppliedManagedDisk bool, rec map[string]interface{}) {
	storageProfile := asMap(asMap(rec["properties"])["storageProfile"])
	osDisk := asMap(storageProfile["osDisk"])
	if osDisk == nil || requestSuppliedManagedDisk || !strings.EqualFold(firstString(osDisk["createOption"]), "FromImage") {
		return
	}
	name := firstString(osDisk["name"])
	if name == "" {
		return
	}

	vmID := firstString(rec["id"])
	k := diskKey(sub, rg, name)
	var existingProps map[string]interface{}
	if v, ok := s.Get(k); ok {
		existing := asMap(v)
		// Never take over a disk someone else created.
		if owner := firstString(existing["_autoCreatedBy"]); owner != "" && !strings.EqualFold(owner, vmID) {
			return
		}
		existingProps = asMap(existing["properties"])
	}

	managedDisk := asMap(osDisk["managedDisk"])
	imageRef := asMap(storageProfile["imageReference"])
	osType, defaultSize := imageOSAndDefaultDiskSize(imageRef)
	sizeGB := firstNumber(osDisk["diskSizeGB"], defaultSize)
	timeCreated := firstString(existingProps["timeCreated"], time.Now().UTC().Format(time.RFC3339))

	s.Set(k, map[string]interface{}{
		"id":       firstString(idFromRef(managedDisk), diskARMID(sub, rg, name)),
		"name":     name,
		"type":     "Microsoft.Compute/disks",
		"location": firstString(rec["location"], "eastus"),
		"tags":     map[string]interface{}{},
		"sku":      map[string]interface{}{"name": firstString(managedDisk["storageAccountType"], "Standard_LRS")},
		"etag":     "W/\"miniblue\"",
		"properties": map[string]interface{}{
			"provisioningState": "Succeeded",
			"creationData": map[string]interface{}{
				"createOption":   "FromImage",
				"imageReference": recursiveCopy(imageRef),
			},
			"diskSizeGB":          sizeGB,
			"diskSizeBytes":       sizeGB * 1024 * 1024 * 1024,
			"osType":              osType,
			"timeCreated":         timeCreated,
			"uniqueId":            firstString(existingProps["uniqueId"], "miniblue-"+sub+"-"+rg+"-"+name),
			"encryption":          map[string]interface{}{"type": "EncryptionAtRestWithPlatformKey"},
			"networkAccessPolicy": "AllowAll",
			"publicNetworkAccess": "Enabled",
			"managedBy":           vmID,
			"diskState":           "Attached",
		},
		"_autoCreatedBy": vmID,
	})
}

// setReferences marks every NIC and managed disk the VM uses as attached to it.
func setReferences(s *store.Store, sub string, rec map[string]interface{}) {
	vmID := firstString(rec["id"])
	props := asMap(rec["properties"])
	for _, nic := range asArray(asMap(props["networkProfile"])["networkInterfaces"]) {
		if nicID := idFromRef(nic); nicID != "" {
			updateByID(s, "nic:"+sub+":", nicID, func(p map[string]interface{}) {
				p["virtualMachine"] = map[string]interface{}{"id": vmID}
			})
		}
	}

	storageProfile := asMap(props["storageProfile"])
	diskIDs := []string{idFromRef(asMap(storageProfile["osDisk"])["managedDisk"])}
	for _, dd := range asArray(storageProfile["dataDisks"]) {
		diskIDs = append(diskIDs, idFromRef(asMap(dd)["managedDisk"]))
	}
	for _, diskID := range diskIDs {
		if diskID == "" {
			continue
		}
		updateByID(s, "disk:"+sub+":", diskID, func(p map[string]interface{}) {
			p["managedBy"] = vmID
			p["diskState"] = "Attached"
		})
	}
}

// clearReferences detaches every NIC and disk currently pointing at the VM.
func clearReferences(s *store.Store, sub string, rec map[string]interface{}) {
	vmID := firstString(rec["id"])
	for _, k := range s.ListKeysByPrefix("nic:" + sub + ":") {
		if v, ok := s.Get(k); ok {
			nic := asMap(v)
			props := asMap(nic["properties"])
			if strings.EqualFold(idFromRef(props["virtualMachine"]), vmID) {
				delete(props, "virtualMachine")
				s.Set(k, nic)
			}
		}
	}
	for _, k := range s.ListKeysByPrefix("disk:" + sub + ":") {
		if v, ok := s.Get(k); ok {
			disk := asMap(v)
			props := asMap(disk["properties"])
			if strings.EqualFold(firstString(props["managedBy"]), vmID) {
				props["managedBy"] = ""
				props["diskState"] = "Unattached"
				s.Set(k, disk)
			}
		}
	}
}

// updateByID applies fn to the properties of the stored resource under prefix
// whose ARM id matches (case-insensitively).
func updateByID(s *store.Store, prefix, id string, fn func(props map[string]interface{})) {
	for _, k := range s.ListKeysByPrefix(prefix) {
		v, ok := s.Get(k)
		if !ok {
			continue
		}
		resource := asMap(v)
		if !strings.EqualFold(firstString(resource["id"]), id) {
			continue
		}
		if props := asMap(resource["properties"]); props != nil {
			fn(props)
			s.Set(k, resource)
		}
		return
	}
}

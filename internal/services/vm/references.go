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
	if disk, ok := s.Get(k); ok && autoCreatedBy(disk, firstString(rec["id"])) {
		s.Delete(k)
	}
}

// autoCreatedBy reports whether the stored disk is the OS disk the given VM
// auto-created. A disk with no recorded creator was made through the managed
// disk API and never belongs to a VM.
func autoCreatedBy(disk interface{}, vmID string) bool {
	owner := firstString(asMap(disk)["_autoCreatedBy"])
	return owner != "" && strings.EqualFold(owner, vmID)
}

// autoOSDiskName returns the name of the OS disk Azure creates for the VM
// (createOption FromImage without a supplied managed disk), or "" if none.
func autoOSDiskName(requestSuppliedManagedDisk bool, rec map[string]interface{}) string {
	osDisk := asMap(asMap(asMap(rec["properties"])["storageProfile"])["osDisk"])
	if requestSuppliedManagedDisk || !strings.EqualFold(firstString(osDisk["createOption"]), "FromImage") {
		return ""
	}
	return firstString(osDisk["name"])
}

// osDiskConflict returns the name of the VM's auto-created OS disk when a
// disk of that name already exists and the VM did not create it. Azure
// rejects such a request rather than adopting the existing disk.
func osDiskConflict(s *store.Store, sub, rg string, requestSuppliedManagedDisk bool, rec map[string]interface{}) string {
	name := autoOSDiskName(requestSuppliedManagedDisk, rec)
	if name == "" {
		return ""
	}
	if disk, ok := s.Get(diskKey(sub, rg, name)); ok && !autoCreatedBy(disk, firstString(rec["id"])) {
		return name
	}
	return ""
}

func ensureAutoManagedOSDisk(s *store.Store, sub, rg string, requestSuppliedManagedDisk bool, rec map[string]interface{}) {
	name := autoOSDiskName(requestSuppliedManagedDisk, rec)
	if name == "" {
		return
	}
	storageProfile := asMap(asMap(rec["properties"])["storageProfile"])
	osDisk := asMap(storageProfile["osDisk"])

	vmID := firstString(rec["id"])
	k := diskKey(sub, rg, name)
	var existingProps map[string]interface{}
	if v, ok := s.Get(k); ok {
		// Never take over a disk this VM did not create.
		if !autoCreatedBy(v, vmID) {
			return
		}
		existingProps = asMap(asMap(v)["properties"])
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
			updateResources(s, "nic:"+sub+":", hasID(nicID), func(p map[string]interface{}) {
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
		updateResources(s, "disk:"+sub+":", hasID(diskID), func(p map[string]interface{}) {
			p["managedBy"] = vmID
			p["diskState"] = "Attached"
		})
	}
}

// clearReferences detaches every NIC and disk currently pointing at the VM.
func clearReferences(s *store.Store, sub string, rec map[string]interface{}) {
	vmID := firstString(rec["id"])
	updateResources(s, "nic:"+sub+":", func(nic map[string]interface{}) bool {
		return strings.EqualFold(idFromRef(asMap(nic["properties"])["virtualMachine"]), vmID)
	}, func(p map[string]interface{}) {
		delete(p, "virtualMachine")
	})
	updateResources(s, "disk:"+sub+":", func(disk map[string]interface{}) bool {
		return strings.EqualFold(firstString(asMap(disk["properties"])["managedBy"]), vmID)
	}, func(p map[string]interface{}) {
		p["managedBy"] = ""
		p["diskState"] = "Unattached"
	})
}

func hasID(id string) func(resource map[string]interface{}) bool {
	return func(resource map[string]interface{}) bool {
		return strings.EqualFold(firstString(resource["id"]), id)
	}
}

// updateResources applies fn to the properties of every stored resource under
// prefix that match accepts. Handlers encode stored maps without locking, so
// the change is made on a deep copy that replaces the stored value.
func updateResources(s *store.Store, prefix string, match func(resource map[string]interface{}) bool, fn func(props map[string]interface{})) {
	for _, k := range s.ListKeysByPrefix(prefix) {
		v, ok := s.Get(k)
		if !ok {
			continue
		}
		resource := asMap(v)
		if resource == nil || !match(resource) {
			continue
		}
		updated := asMap(recursiveCopy(resource))
		if props := asMap(updated["properties"]); props != nil {
			fn(props)
			s.Set(k, updated)
		}
	}
}

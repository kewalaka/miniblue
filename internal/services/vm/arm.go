package vm

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/moabukar/miniblue/internal/services/compute"
)

// ---------------------------------------------------------------------------
// ARM fidelity
//
// The Docker backend only cares about the image; Terraform providers (azurerm
// in particular) additionally diff the full ARM payload on every plan, so the
// record must replay what the client sent plus the defaults real Azure adds.
// ---------------------------------------------------------------------------

const (
	stateRunning     = "running"
	stateStopped     = "stopped"
	stateDeallocated = "deallocated"
)

func asMap(v interface{}) map[string]interface{} {
	m, _ := v.(map[string]interface{})
	return m
}

func asArray(v interface{}) []interface{} {
	a, _ := v.([]interface{})
	return a
}

func copyMap(src map[string]interface{}) map[string]interface{} {
	dst := make(map[string]interface{}, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

func recursiveCopy(v interface{}) interface{} {
	switch x := v.(type) {
	case map[string]interface{}:
		m := make(map[string]interface{}, len(x))
		for k, item := range x {
			m[k] = recursiveCopy(item)
		}
		return m
	case []interface{}:
		items := make([]interface{}, len(x))
		for i, item := range x {
			items[i] = recursiveCopy(item)
		}
		return items
	default:
		return v
	}
}

// mergePatch applies an ARM PATCH body: top-level fields are replaced, and
// "properties" is merged one level deep.
func mergePatch(existing, patch map[string]interface{}) map[string]interface{} {
	merged := copyMap(existing)
	for k, v := range patch {
		if k == "properties" {
			props := copyMap(asMap(existing["properties"]))
			for pk, pv := range asMap(v) {
				props[pk] = pv
			}
			merged[k] = props
			continue
		}
		merged[k] = v
	}
	return merged
}

func firstString(values ...interface{}) string {
	for _, v := range values {
		if s, ok := v.(string); ok && s != "" {
			return s
		}
	}
	return ""
}

func firstNumber(values ...interface{}) float64 {
	for _, v := range values {
		if n, ok := v.(float64); ok && n != 0 {
			return n
		}
	}
	return 0
}

func idFromRef(v interface{}) string {
	return firstString(asMap(v)["id"])
}

// vmProperties builds the ARM properties for a PUT/PATCH. Request properties
// are replayed as sent; the profile sections fall back to the stored ones when
// the request omits them.
func vmProperties(sub, rg, name string, inProps, existingProps map[string]interface{}) map[string]interface{} {
	props := asMap(recursiveCopy(inProps))
	delete(props, "miniblue")
	if len(props) == 0 {
		props = asMap(recursiveCopy(existingProps))
		delete(props, "miniblue")
	}
	if props == nil {
		props = map[string]interface{}{}
	}
	for _, k := range []string{"hardwareProfile", "storageProfile", "osProfile", "networkProfile"} {
		if _, ok := props[k]; ok {
			continue
		}
		if v, ok := existingProps[k]; ok {
			props[k] = recursiveCopy(v)
		}
	}
	if props["hardwareProfile"] == nil {
		props["hardwareProfile"] = map[string]interface{}{"vmSize": "Standard_B1s"}
	}

	normalizeStorageProfile(sub, rg, name, props)
	normalizeAdditionalCapabilities(props)
	if firstString(props["vmId"]) == "" {
		props["vmId"] = firstString(existingProps["vmId"], uuid.NewSHA1(uuid.NameSpaceURL, []byte(strings.ToLower(vmARMID(sub, rg, name)))).String())
	}
	props["provisioningState"] = "Succeeded"
	return props
}

// normalizeStorageProfile fills the osDisk fields Azure always returns
// (size, name, managed disk id) so GET does not drift from what a client
// computed at plan time.
func normalizeStorageProfile(sub, rg, name string, props map[string]interface{}) {
	storageProfile := asMap(props["storageProfile"])
	osDisk := asMap(storageProfile["osDisk"])
	if osDisk == nil {
		return
	}

	if firstNumber(osDisk["diskSizeGB"]) == 0 {
		_, osDisk["diskSizeGB"] = imageOSAndDefaultDiskSize(asMap(storageProfile["imageReference"]))
	}

	diskName := firstString(osDisk["name"])
	if diskName == "" {
		sum := sha1.Sum([]byte(sub + ":" + rg + ":" + name))
		diskName = fmt.Sprintf("%s_OsDisk_%s", name, hex.EncodeToString(sum[:4]))
		osDisk["name"] = diskName
	}

	managedDisk := asMap(osDisk["managedDisk"])
	if managedDisk == nil {
		managedDisk = map[string]interface{}{}
		osDisk["managedDisk"] = managedDisk
	}
	if firstString(managedDisk["id"]) == "" {
		managedDisk["id"] = diskARMID(sub, rg, diskName)
	}
}

func diskARMID(sub, rg, name string) string {
	return "/subscriptions/" + sub + "/resourceGroups/" + rg + "/providers/Microsoft.Compute/disks/" + name
}

// imageOSAndDefaultDiskSize returns the OS type and Azure's default OS disk
// size (GB) for an imageReference.
func imageOSAndDefaultDiskSize(imageRef map[string]interface{}) (string, float64) {
	if !isWindowsImage(imageRef) {
		return "Linux", 30
	}
	if strings.Contains(strings.ToLower(firstString(imageRef["sku"])), "smalldisk") {
		return "Windows", 30
	}
	return "Windows", 127
}

// isWindowsImage consults the compute catalog first, then falls back to the
// publisher: stub mode accepts references the catalog does not list (for
// example Windows Server 2022 or Windows 11), and Windows publishers all start
// with "MicrosoftWindows".
func isWindowsImage(imageRef map[string]interface{}) bool {
	publisher := firstString(imageRef["publisher"])
	if osType, ok := compute.LookupImage(publisher, firstString(imageRef["offer"]), firstString(imageRef["sku"])); ok {
		return strings.EqualFold(osType, "Windows")
	}
	return strings.HasPrefix(strings.ToLower(publisher), "microsoftwindows")
}

// normalizeAdditionalCapabilities replays the client's payload faithfully:
// sub-fields are never auto-filled, and an empty or non-object value drops
// the key, as real ARM does. Clients that rely on key presence/absence would
// otherwise see false drift.
func normalizeAdditionalCapabilities(props map[string]interface{}) {
	raw, present := props["additionalCapabilities"]
	if present && len(asMap(raw)) == 0 {
		delete(props, "additionalCapabilities")
	}
}

// instanceViewFor renders the instance view for a stored power state.
func instanceViewFor(state string) map[string]interface{} {
	now := time.Now().UTC().Format(time.RFC3339)
	status := func(code, display string) map[string]interface{} {
		return map[string]interface{}{"code": code, "displayStatus": display, "level": "Info", "time": now}
	}
	return map[string]interface{}{
		"vmAgent": map[string]interface{}{
			"vmAgentVersion": "miniblue",
			"statuses":       []interface{}{status("ProvisioningState/succeeded", "Ready")},
		},
		"statuses": []interface{}{
			status("ProvisioningState/succeeded", "Provisioning succeeded"),
			status("PowerState/"+state, "VM "+state),
		},
	}
}

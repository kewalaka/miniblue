package virtualmachines

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/moabukar/miniblue/internal/azerr"
	"github.com/moabukar/miniblue/internal/services/compute"
	"github.com/moabukar/miniblue/internal/store"
)

type Handler struct {
	store *store.Store
}

func NewHandler(s *store.Store) *Handler {
	return &Handler{store: s}
}

func (h *Handler) Register(r chi.Router) {
	r.Route("/subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/Microsoft.Compute/virtualMachines", func(r chi.Router) {
		r.Get("/", h.List)
		r.Route("/{vmName}", func(r chi.Router) {
			r.Put("/", h.CreateOrUpdate)
			r.Patch("/", h.Update)
			r.Get("/", h.Get)
			r.Delete("/", h.Delete)
			r.Get("/instanceView", h.InstanceView)
			r.Post("/start", h.Start)
			r.Post("/powerOff", h.PowerOff)
			r.Post("/restart", h.Restart)
			r.Post("/deallocate", h.Deallocate)
			r.Post("/redeploy", h.Redeploy)
			r.Route("/extensions", func(r chi.Router) {
				r.Get("/", h.ListExtensions)
				r.Route("/{extensionName}", func(r chi.Router) {
					r.Put("/", h.CreateOrUpdateExtension)
					r.Patch("/", h.UpdateExtension)
					r.Get("/", h.GetExtension)
					r.Delete("/", h.DeleteExtension)
				})
			})
		})
	})
	r.Get("/subscriptions/{subscriptionId}/providers/Microsoft.Compute/virtualMachines", h.ListInSubscription)
}

func (h *Handler) key(sub, rg, name string) string {
	return "vm:" + sub + ":" + rg + ":" + name
}

func (h *Handler) extensionKey(sub, rg, vm, name string) string {
	return "vmext:" + sub + ":" + rg + ":" + vm + ":" + name
}

func resourceID(sub, rg, name string) string {
	return "/subscriptions/" + sub + "/resourceGroups/" + rg + "/providers/Microsoft.Compute/virtualMachines/" + name
}

func extensionResourceID(sub, rg, vm, name string) string {
	return resourceID(sub, rg, vm) + "/extensions/" + name
}

func asMap(v interface{}) map[string]interface{} {
	m, _ := v.(map[string]interface{})
	return m
}

func asArray(v interface{}) []interface{} {
	if a, ok := v.([]interface{}); ok {
		return a
	}
	return []interface{}{}
}

func copyMap(src map[string]interface{}) map[string]interface{} {
	dst := map[string]interface{}{}
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

func recursiveCopy(v interface{}) interface{} {
	switch x := v.(type) {
	case map[string]interface{}:
		m := map[string]interface{}{}
		for k, item := range x {
			m[k] = recursiveCopy(item)
		}
		return m
	case []interface{}:
		items := make([]interface{}, 0, len(x))
		for _, item := range x {
			items = append(items, recursiveCopy(item))
		}
		return items
	default:
		return v
	}
}

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
		switch n := v.(type) {
		case float64:
			if n != 0 {
				return n
			}
		case int:
			if n != 0 {
				return float64(n)
			}
		}
	}
	return 0
}

func powerState(vm map[string]interface{}) string {
	state, _ := vm["_powerState"].(string)
	if state == "" {
		return "PowerState/running"
	}
	return state
}

func instanceViewFor(state string) map[string]interface{} {
	now := time.Now().UTC().Format(time.RFC3339)
	display := strings.TrimPrefix(state, "PowerState/")
	return map[string]interface{}{
		"vmAgent": map[string]interface{}{
			"vmAgentVersion": "miniblue",
			"statuses": []interface{}{
				map[string]interface{}{
					"code":          "ProvisioningState/succeeded",
					"displayStatus": "Ready",
					"level":         "Info",
					"time":          now,
				},
			},
		},
		"statuses": []interface{}{
			map[string]interface{}{
				"code":          "ProvisioningState/succeeded",
				"displayStatus": "Provisioning succeeded",
				"level":         "Info",
				"time":          now,
			},
			map[string]interface{}{
				"code":          state,
				"displayStatus": "VM " + display,
				"level":         "Info",
				"time":          now,
			},
		},
	}
}

func buildResponse(sub, rg, name string, input map[string]interface{}, existing map[string]interface{}) map[string]interface{} {
	id := resourceID(sub, rg, name)
	props := asMap(input["properties"])
	if props == nil {
		props = map[string]interface{}{}
	}
	existingProps := asMap(existing["properties"])

	location := firstString(input["location"], existing["location"])
	if location == "" {
		location = "eastus"
	}
	tags := asMap(input["tags"])
	if tags == nil {
		tags = asMap(existing["tags"])
	}
	if tags == nil {
		tags = map[string]interface{}{}
	}

	vmProps := copyMap(props)
	if len(vmProps) == 0 {
		vmProps = copyMap(existingProps)
	}
	if _, ok := vmProps["hardwareProfile"]; !ok {
		if v, ok := existingProps["hardwareProfile"]; ok {
			vmProps["hardwareProfile"] = v
		}
	}
	if _, ok := vmProps["storageProfile"]; !ok {
		if v, ok := existingProps["storageProfile"]; ok {
			vmProps["storageProfile"] = v
		}
	}
	if _, ok := vmProps["osProfile"]; !ok {
		if v, ok := existingProps["osProfile"]; ok {
			vmProps["osProfile"] = v
		}
	}
	if _, ok := vmProps["networkProfile"]; !ok {
		if v, ok := existingProps["networkProfile"]; ok {
			vmProps["networkProfile"] = v
		}
	}
	normalizeVMStorageProfile(sub, rg, name, vmProps)
	normalizeAdditionalCapabilities(vmProps)
	ensureVMID(sub, rg, name, vmProps, existingProps)
	vmProps["provisioningState"] = "Succeeded"

	state := firstString(existing["_powerState"])
	if state == "" {
		state = "PowerState/running"
	}

	resp := map[string]interface{}{
		"id":          id,
		"name":        name,
		"type":        "Microsoft.Compute/virtualMachines",
		"location":    location,
		"tags":        tags,
		"properties":  vmProps,
		"_powerState": state,
	}
	for _, field := range []string{"identity", "plan", "sku", "zones", "extendedLocation"} {
		if v, ok := input[field]; ok {
			resp[field] = v
		} else if v, ok := existing[field]; ok {
			resp[field] = v
		}
	}
	return resp
}

func sanitizeVM(vm map[string]interface{}, includeInstanceView bool) map[string]interface{} {
	out := recursiveCopy(vm).(map[string]interface{})
	delete(out, "_powerState")
	props := asMap(out["properties"])
	if props == nil {
		return out
	}
	if includeInstanceView {
		props["instanceView"] = instanceViewFor(powerState(vm))
	} else {
		delete(props, "instanceView")
	}
	osProfile := asMap(props["osProfile"])
	if osProfile == nil {
		return out
	}
	delete(osProfile, "adminPassword")
	return out
}

func normalizeVMStorageProfile(sub, rg, name string, vmProps map[string]interface{}) {
	storageProfile := asMap(vmProps["storageProfile"])
	if storageProfile == nil {
		return
	}
	osDisk := asMap(storageProfile["osDisk"])
	if osDisk == nil {
		return
	}

	if diskSize := firstNumber(osDisk["diskSizeGB"]); diskSize == 0 {
		imageRef := asMap(storageProfile["imageReference"])
		_, defaultSize := imageOSAndDefaultDiskSize(imageRef)
		osDisk["diskSizeGB"] = defaultSize
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
		managedDisk["id"] = "/subscriptions/" + sub + "/resourceGroups/" + rg + "/providers/Microsoft.Compute/disks/" + diskName
	}
}

func imageOSAndDefaultDiskSize(imageRef map[string]interface{}) (string, float64) {
	publisher, _ := imageRef["publisher"].(string)
	offer, _ := imageRef["offer"].(string)
	sku, _ := imageRef["sku"].(string)
	osType, ok := compute.LookupImage(publisher, offer, sku)
	if ok && strings.EqualFold(osType, "Windows") {
		if strings.Contains(strings.ToLower(sku), "smalldisk") {
			return "Windows", 30
		}
		return "Windows", 127
	}
	return "Linux", 30
}

func normalizeAdditionalCapabilities(vmProps map[string]interface{}) {
	raw, present := vmProps["additionalCapabilities"]
	if !present {
		return
	}
	additionalCapabilities := asMap(raw)
	// Faithfully replay the client's payload: do not auto-fill missing
	// sub-fields, and drop the key entirely when the client provided no
	// usable content (empty object or non-object value). Real Azure ARM
	// echoes back exactly what was PUT for additionalCapabilities, so
	// miniblue must do the same to avoid false drift for clients that
	// rely on key presence/absence semantics.
	if len(additionalCapabilities) == 0 {
		delete(vmProps, "additionalCapabilities")
		return
	}
}

func ensureVMID(sub, rg, name string, vmProps map[string]interface{}, existingProps map[string]interface{}) {
	if firstString(vmProps["vmId"]) != "" {
		return
	}
	if existingVMID := firstString(existingProps["vmId"]); existingVMID != "" {
		vmProps["vmId"] = existingVMID
		return
	}
	sum := sha1.Sum([]byte(sub + ":" + rg + ":" + name))
	vmProps["vmId"] = fmt.Sprintf("%x-%x-%x-%x-%x", sum[0:4], sum[4:6], sum[6:8], sum[8:10], sum[10:16])
}

func sanitizeExtension(ext map[string]interface{}) map[string]interface{} {
	out := recursiveCopy(ext).(map[string]interface{})
	props := asMap(out["properties"])
	delete(props, "protectedSettings")
	return out
}

func idFromRef(v interface{}) string {
	m := asMap(v)
	id, _ := m["id"].(string)
	return id
}

func (h *Handler) CreateOrUpdate(w http.ResponseWriter, r *http.Request) {
	sub := chi.URLParam(r, "subscriptionId")
	rg := chi.URLParam(r, "resourceGroupName")
	name := chi.URLParam(r, "vmName")

	var input map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		azerr.BadRequest(w, "The request content was invalid: "+err.Error())
		return
	}
	if input == nil {
		input = map[string]interface{}{}
	}

	k := h.key(sub, rg, name)
	existing, exists := h.store.Get(k)
	requestSuppliedManagedDiskID := requestHasManagedDiskID(input)
	vm := buildResponse(sub, rg, name, input, asMap(existing))
	h.ensureAutoManagedOSDisk(sub, rg, requestSuppliedManagedDiskID, vm)
	h.store.Set(k, vm)
	h.refreshReferences(sub, vm)
	if exists {
		w.WriteHeader(http.StatusOK)
	} else {
		w.WriteHeader(http.StatusCreated)
	}
	json.NewEncoder(w).Encode(sanitizeVM(vm, false))
}

func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	sub := chi.URLParam(r, "subscriptionId")
	rg := chi.URLParam(r, "resourceGroupName")
	name := chi.URLParam(r, "vmName")

	k := h.key(sub, rg, name)
	existing, ok := h.store.Get(k)
	if !ok {
		azerr.NotFound(w, "Microsoft.Compute/virtualMachines", name)
		return
	}
	var patch map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
		azerr.BadRequest(w, "The request content was invalid: "+err.Error())
		return
	}
	if patch == nil {
		patch = map[string]interface{}{}
	}
	existingMap := asMap(existing)
	requestSuppliedManagedDiskID := requestHasManagedDiskID(patch)
	vm := buildResponse(sub, rg, name, mergePatch(existingMap, patch), existingMap)
	h.ensureAutoManagedOSDisk(sub, rg, requestSuppliedManagedDiskID, vm)
	h.store.Set(k, vm)
	h.refreshReferences(sub, vm)
	json.NewEncoder(w).Encode(sanitizeVM(vm, false))
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	sub := chi.URLParam(r, "subscriptionId")
	rg := chi.URLParam(r, "resourceGroupName")
	name := chi.URLParam(r, "vmName")

	v, ok := h.store.Get(h.key(sub, rg, name))
	if !ok {
		azerr.NotFound(w, "Microsoft.Compute/virtualMachines", name)
		return
	}
	expand := strings.EqualFold(r.URL.Query().Get("$expand"), "instanceView")
	json.NewEncoder(w).Encode(sanitizeVM(asMap(v), expand))
}

func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	sub := chi.URLParam(r, "subscriptionId")
	rg := chi.URLParam(r, "resourceGroupName")
	name := chi.URLParam(r, "vmName")

	k := h.key(sub, rg, name)
	v, ok := h.store.Get(k)
	if !ok {
		azerr.NotFound(w, "Microsoft.Compute/virtualMachines", name)
		return
	}
	h.clearReferences(sub, asMap(v))
	h.deleteAutoManagedOSDisk(sub, rg, asMap(v))
	h.store.DeleteByPrefix(h.extensionKey(sub, rg, name, ""))
	h.store.Delete(k)
	w.WriteHeader(http.StatusAccepted)
}

func (h *Handler) ensureAutoManagedOSDisk(sub, rg string, requestSuppliedManagedDiskID bool, vm map[string]interface{}) {
	vmProps := asMap(vm["properties"])
	storageProfile := asMap(vmProps["storageProfile"])
	osDisk := asMap(storageProfile["osDisk"])
	if osDisk == nil || !strings.EqualFold(firstString(osDisk["createOption"]), "FromImage") {
		return
	}
	if requestSuppliedManagedDiskID {
		return
	}

	osDiskName := firstString(osDisk["name"])
	if osDiskName == "" {
		return
	}
	managedDisk := asMap(osDisk["managedDisk"])
	managedDiskID := firstString(managedDisk["id"])
	if managedDiskID == "" {
		managedDiskID = "/subscriptions/" + sub + "/resourceGroups/" + rg + "/providers/Microsoft.Compute/disks/" + osDiskName
	}
	vmID := firstString(vm["id"])
	diskKey := "disk:" + sub + ":" + rg + ":" + osDiskName
	existingDisk := map[string]interface{}{}
	if v, ok := h.store.Get(diskKey); ok {
		existingDisk = asMap(v)
		if owner := firstString(existingDisk["_autoCreatedBy"]); owner != "" && !strings.EqualFold(owner, vmID) {
			return
		}
	}

	location := firstString(vm["location"])
	if location == "" {
		location = "eastus"
	}
	imageRef := asMap(storageProfile["imageReference"])
	osType, defaultSize := imageOSAndDefaultDiskSize(imageRef)
	diskSizeGB := firstNumber(osDisk["diskSizeGB"])
	if diskSizeGB == 0 {
		diskSizeGB = defaultSize
	}
	diskSizeBytes := diskSizeGB * 1024 * 1024 * 1024
	storageAccountType := firstString(managedDisk["storageAccountType"])
	if storageAccountType == "" {
		storageAccountType = "Standard_LRS"
	}
	existingDiskProps := asMap(existingDisk["properties"])
	timeCreated := firstString(existingDiskProps["timeCreated"])
	if timeCreated == "" {
		timeCreated = time.Now().UTC().Format(time.RFC3339)
	}
	uniqueID := firstString(existingDiskProps["uniqueId"])
	if uniqueID == "" {
		uniqueID = fmt.Sprintf("miniblue-%s-%s-%s", sub, rg, osDiskName)
	}

	disk := map[string]interface{}{
		"id":       managedDiskID,
		"name":     osDiskName,
		"type":     "Microsoft.Compute/disks",
		"location": location,
		"tags":     map[string]interface{}{},
		"sku":      map[string]interface{}{"name": storageAccountType},
		"etag":     "W/\"miniblue\"",
		"properties": map[string]interface{}{
			"provisioningState": "Succeeded",
			"creationData": map[string]interface{}{
				"createOption":   "FromImage",
				"imageReference": recursiveCopy(imageRef),
			},
			"diskSizeGB":          diskSizeGB,
			"diskSizeBytes":       diskSizeBytes,
			"osType":              osType,
			"timeCreated":         timeCreated,
			"uniqueId":            uniqueID,
			"encryption":          map[string]interface{}{"type": "EncryptionAtRestWithPlatformKey"},
			"networkAccessPolicy": "AllowAll",
			"publicNetworkAccess": "Enabled",
			"managedBy":           vmID,
			"diskState":           "Attached",
		},
		"_autoCreatedBy": vmID,
	}
	h.store.Set(diskKey, disk)
}

func requestHasManagedDiskID(request map[string]interface{}) bool {
	props := asMap(request["properties"])
	storageProfile := asMap(props["storageProfile"])
	osDisk := asMap(storageProfile["osDisk"])
	managedDisk := asMap(osDisk["managedDisk"])
	return firstString(managedDisk["id"]) != ""
}

func (h *Handler) deleteAutoManagedOSDisk(sub, rg string, vm map[string]interface{}) {
	vmID := firstString(vm["id"])
	osDisk := asMap(asMap(asMap(vm["properties"])["storageProfile"])["osDisk"])
	osDiskName := firstString(osDisk["name"])
	if vmID == "" || osDiskName == "" {
		return
	}
	diskKey := "disk:" + sub + ":" + rg + ":" + osDiskName
	disk, ok := h.store.Get(diskKey)
	if !ok {
		return
	}
	if strings.EqualFold(firstString(asMap(disk)["_autoCreatedBy"]), vmID) {
		h.store.Delete(diskKey)
	}
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	sub := chi.URLParam(r, "subscriptionId")
	rg := chi.URLParam(r, "resourceGroupName")
	items := h.sanitizedVMList("vm:" + sub + ":" + rg + ":")
	json.NewEncoder(w).Encode(map[string]interface{}{"value": items})
}

func (h *Handler) ListInSubscription(w http.ResponseWriter, r *http.Request) {
	sub := chi.URLParam(r, "subscriptionId")
	items := h.sanitizedVMList("vm:" + sub + ":")
	json.NewEncoder(w).Encode(map[string]interface{}{"value": items})
}

func (h *Handler) sanitizedVMList(prefix string) []interface{} {
	items := h.store.ListByPrefix(prefix)
	out := make([]interface{}, 0, len(items))
	for _, item := range items {
		if vm := asMap(item); vm != nil {
			out = append(out, sanitizeVM(vm, false))
		}
	}
	return out
}

func (h *Handler) InstanceView(w http.ResponseWriter, r *http.Request) {
	sub := chi.URLParam(r, "subscriptionId")
	rg := chi.URLParam(r, "resourceGroupName")
	name := chi.URLParam(r, "vmName")
	v, ok := h.store.Get(h.key(sub, rg, name))
	if !ok {
		azerr.NotFound(w, "Microsoft.Compute/virtualMachines", name)
		return
	}
	json.NewEncoder(w).Encode(instanceViewFor(powerState(asMap(v))))
}

func (h *Handler) Start(w http.ResponseWriter, r *http.Request) {
	h.setPowerState(w, r, "PowerState/running")
}

func (h *Handler) PowerOff(w http.ResponseWriter, r *http.Request) {
	h.setPowerState(w, r, "PowerState/stopped")
}

func (h *Handler) Restart(w http.ResponseWriter, r *http.Request) {
	h.setPowerState(w, r, "PowerState/running")
}

func (h *Handler) Deallocate(w http.ResponseWriter, r *http.Request) {
	h.setPowerState(w, r, "PowerState/deallocated")
}

func (h *Handler) Redeploy(w http.ResponseWriter, r *http.Request) {
	h.setPowerState(w, r, "PowerState/running")
}

func (h *Handler) setPowerState(w http.ResponseWriter, r *http.Request, state string) {
	sub := chi.URLParam(r, "subscriptionId")
	rg := chi.URLParam(r, "resourceGroupName")
	name := chi.URLParam(r, "vmName")
	k := h.key(sub, rg, name)
	v, ok := h.store.Get(k)
	if !ok {
		azerr.NotFound(w, "Microsoft.Compute/virtualMachines", name)
		return
	}
	vm := asMap(v)
	vm["_powerState"] = state
	h.store.Set(k, vm)
	w.WriteHeader(http.StatusAccepted)
}

func (h *Handler) refreshReferences(sub string, vm map[string]interface{}) {
	h.clearReferences(sub, vm)
	vmID, _ := vm["id"].(string)
	props := asMap(vm["properties"])
	networkProfile := asMap(props["networkProfile"])
	for _, item := range asArray(networkProfile["networkInterfaces"]) {
		nicID := idFromRef(item)
		if nicID == "" {
			continue
		}
		h.setReferenceOnMatchingResource("nic:"+sub+":", nicID, "virtualMachine", map[string]interface{}{"id": vmID})
	}

	for _, diskID := range diskIDsFromStorageProfile(asMap(props["storageProfile"])) {
		h.setReferenceOnMatchingResource("disk:"+sub+":", diskID, "managedBy", vmID)
		h.setReferenceOnMatchingResource("disk:"+sub+":", diskID, "diskState", "Attached")
	}
}

func (h *Handler) clearReferences(sub string, vm map[string]interface{}) {
	vmID, _ := vm["id"].(string)
	for _, k := range h.store.ListKeysByPrefix("nic:" + sub + ":") {
		if !strings.HasPrefix(k, "nic:"+sub+":") {
			continue
		}
		item, ok := h.store.Get(k)
		if !ok {
			continue
		}
		nic := asMap(item)
		props := asMap(nic["properties"])
		if strings.EqualFold(idFromRef(props["virtualMachine"]), vmID) {
			delete(props, "virtualMachine")
			h.store.Set(k, nic)
		}
	}
	for _, k := range h.store.ListKeysByPrefix("disk:" + sub + ":") {
		if !strings.HasPrefix(k, "disk:"+sub+":") {
			continue
		}
		item, ok := h.store.Get(k)
		if !ok {
			continue
		}
		disk := asMap(item)
		props := asMap(disk["properties"])
		if strings.EqualFold(firstString(props["managedBy"]), vmID) {
			props["managedBy"] = ""
			props["diskState"] = "Unattached"
			h.store.Set(k, disk)
		}
	}
}

func diskIDsFromStorageProfile(storageProfile map[string]interface{}) []string {
	ids := []string{}
	if osDiskID := idFromRef(asMap(storageProfile["osDisk"])["managedDisk"]); osDiskID != "" {
		ids = append(ids, osDiskID)
	}
	for _, item := range asArray(storageProfile["dataDisks"]) {
		disk := asMap(item)
		if diskID := idFromRef(disk["managedDisk"]); diskID != "" {
			ids = append(ids, diskID)
		}
	}
	return ids
}

func (h *Handler) setReferenceOnMatchingResource(prefix, targetID, property string, value interface{}) {
	for _, k := range h.store.ListKeysByPrefix(prefix) {
		if !strings.HasPrefix(k, prefix) {
			continue
		}
		item, ok := h.store.Get(k)
		if !ok {
			continue
		}
		resource := asMap(item)
		id, _ := resource["id"].(string)
		if !strings.EqualFold(id, targetID) {
			continue
		}
		props := asMap(resource["properties"])
		props[property] = value
		h.store.Set(k, resource)
		return
	}
}

func buildExtensionResponse(sub, rg, vm, name string, input map[string]interface{}, existing map[string]interface{}) map[string]interface{} {
	vmResource := asMap(existing["_vm"])
	location := firstString(input["location"], existing["location"], vmResource["location"])
	if location == "" {
		location = "eastus"
	}
	props := asMap(input["properties"])
	if props == nil {
		props = copyMap(asMap(existing["properties"]))
	}
	extensionProps := copyMap(props)
	extensionProps["provisioningState"] = "Succeeded"
	resp := map[string]interface{}{
		"id":         extensionResourceID(sub, rg, vm, name),
		"name":       name,
		"type":       "Microsoft.Compute/virtualMachines/extensions",
		"location":   location,
		"properties": extensionProps,
	}
	if tags := asMap(input["tags"]); tags != nil {
		resp["tags"] = tags
	} else if tags := asMap(existing["tags"]); tags != nil {
		resp["tags"] = tags
	}
	return resp
}

func (h *Handler) CreateOrUpdateExtension(w http.ResponseWriter, r *http.Request) {
	h.writeExtension(w, r, false)
}

func (h *Handler) UpdateExtension(w http.ResponseWriter, r *http.Request) {
	h.writeExtension(w, r, true)
}

func (h *Handler) writeExtension(w http.ResponseWriter, r *http.Request, patch bool) {
	sub := chi.URLParam(r, "subscriptionId")
	rg := chi.URLParam(r, "resourceGroupName")
	vmName := chi.URLParam(r, "vmName")
	name := chi.URLParam(r, "extensionName")
	vm, ok := h.store.Get(h.key(sub, rg, vmName))
	if !ok {
		azerr.NotFound(w, "Microsoft.Compute/virtualMachines", vmName)
		return
	}
	var input map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		azerr.BadRequest(w, "The request content was invalid: "+err.Error())
		return
	}
	if input == nil {
		input = map[string]interface{}{}
	}
	k := h.extensionKey(sub, rg, vmName, name)
	existing, exists := h.store.Get(k)
	if patch {
		if !exists {
			azerr.NotFound(w, "Microsoft.Compute/virtualMachines/extensions", name)
			return
		}
		input = mergePatch(asMap(existing), input)
	}
	existingMap := asMap(existing)
	if existingMap == nil {
		existingMap = map[string]interface{}{}
	}
	existingMap["_vm"] = vm
	ext := buildExtensionResponse(sub, rg, vmName, name, input, existingMap)
	h.store.Set(k, ext)
	if !patch && !exists {
		w.WriteHeader(http.StatusCreated)
	}
	json.NewEncoder(w).Encode(sanitizeExtension(ext))
}

func (h *Handler) GetExtension(w http.ResponseWriter, r *http.Request) {
	sub := chi.URLParam(r, "subscriptionId")
	rg := chi.URLParam(r, "resourceGroupName")
	vmName := chi.URLParam(r, "vmName")
	name := chi.URLParam(r, "extensionName")
	v, ok := h.store.Get(h.extensionKey(sub, rg, vmName, name))
	if !ok {
		azerr.NotFound(w, "Microsoft.Compute/virtualMachines/extensions", name)
		return
	}
	json.NewEncoder(w).Encode(sanitizeExtension(asMap(v)))
}

func (h *Handler) DeleteExtension(w http.ResponseWriter, r *http.Request) {
	sub := chi.URLParam(r, "subscriptionId")
	rg := chi.URLParam(r, "resourceGroupName")
	vmName := chi.URLParam(r, "vmName")
	name := chi.URLParam(r, "extensionName")
	if !h.store.Delete(h.extensionKey(sub, rg, vmName, name)) {
		azerr.NotFound(w, "Microsoft.Compute/virtualMachines/extensions", name)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

func (h *Handler) ListExtensions(w http.ResponseWriter, r *http.Request) {
	sub := chi.URLParam(r, "subscriptionId")
	rg := chi.URLParam(r, "resourceGroupName")
	vmName := chi.URLParam(r, "vmName")
	if !h.store.Exists(h.key(sub, rg, vmName)) {
		azerr.NotFound(w, "Microsoft.Compute/virtualMachines", vmName)
		return
	}
	items := h.store.ListByPrefix(h.extensionKey(sub, rg, vmName, ""))
	out := make([]interface{}, 0, len(items))
	for _, item := range items {
		if ext := asMap(item); ext != nil {
			out = append(out, sanitizeExtension(ext))
		}
	}
	json.NewEncoder(w).Encode(map[string]interface{}{"value": out})
}

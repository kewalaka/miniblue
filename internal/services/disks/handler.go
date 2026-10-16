package disks

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/moabukar/miniblue/internal/azerr"
	"github.com/moabukar/miniblue/internal/store"
)

type Handler struct {
	store *store.Store
}

const (
	fakeSASHost      = "https://miniblue.local"
	fakeSASVersion   = "miniblue"
	fakeSASSignature = "fake"
)

func NewHandler(s *store.Store) *Handler {
	return &Handler{store: s}
}

func (h *Handler) Register(r chi.Router) {
	r.Route("/subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/Microsoft.Compute/disks", func(r chi.Router) {
		r.Get("/", h.List)
		r.Route("/{diskName}", func(r chi.Router) {
			r.Put("/", h.CreateOrUpdate)
			r.Patch("/", h.Update)
			r.Get("/", h.Get)
			r.Delete("/", h.Delete)
			r.Post("/beginGetAccess", h.BeginGetAccess)
			r.Post("/endGetAccess", h.EndGetAccess)
		})
	})
	r.Get("/subscriptions/{subscriptionId}/providers/Microsoft.Compute/disks", h.ListInSubscription)
}

func (h *Handler) key(sub, rg, name string) string {
	return "disk:" + sub + ":" + rg + ":" + name
}

func resourceID(sub, rg, name string) string {
	return "/subscriptions/" + sub + "/resourceGroups/" + rg + "/providers/Microsoft.Compute/disks/" + name
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

func diskState(props map[string]interface{}) string {
	if managedBy, _ := props["managedBy"].(string); managedBy != "" {
		return "Attached"
	}
	if len(asArray(props["managedByExtended"])) > 0 {
		return "Attached"
	}
	creationData := asMap(props["creationData"])
	createOption, _ := creationData["createOption"].(string)
	if strings.EqualFold(createOption, "Upload") || strings.EqualFold(createOption, "UploadPreparedSecure") {
		return "ReadyToUpload"
	}
	return "Unattached"
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
	sku := asMap(input["sku"])
	if sku == nil {
		sku = asMap(existing["sku"])
	}
	if sku == nil {
		sku = map[string]interface{}{"name": "Standard_LRS", "tier": "Standard"}
	}

	creationData := asMap(props["creationData"])
	if creationData == nil {
		creationData = asMap(existingProps["creationData"])
	}
	if creationData == nil {
		creationData = map[string]interface{}{"createOption": "Empty"}
	}
	if firstString(creationData["createOption"]) == "" {
		creationData["createOption"] = "Empty"
	}

	diskSizeGB := firstNumber(props["diskSizeGB"], existingProps["diskSizeGB"])
	if diskSizeGB == 0 {
		diskSizeGB = 32
	}
	diskSizeBytes := firstNumber(props["diskSizeBytes"], existingProps["diskSizeBytes"])
	if diskSizeBytes == 0 {
		diskSizeBytes = diskSizeGB * 1024 * 1024 * 1024
	}
	timeCreated := firstString(existingProps["timeCreated"])
	if timeCreated == "" {
		timeCreated = time.Now().UTC().Format(time.RFC3339)
	}
	uniqueID := firstString(existingProps["uniqueId"])
	if uniqueID == "" {
		uniqueID = fmt.Sprintf("miniblue-%s-%s-%s", sub, rg, name)
	}

	diskProps := map[string]interface{}{
		"provisioningState":   "Succeeded",
		"creationData":        creationData,
		"diskSizeGB":          diskSizeGB,
		"diskSizeBytes":       diskSizeBytes,
		"timeCreated":         timeCreated,
		"uniqueId":            uniqueID,
		"diskState":           "Unattached",
		"encryption":          map[string]interface{}{"type": "EncryptionAtRestWithPlatformKey"},
		"networkAccessPolicy": "AllowAll",
		"publicNetworkAccess": "Enabled",
	}

	for _, field := range []string{
		"osType", "hyperVGeneration", "architecture", "managedBy", "managedByExtended",
		"diskIOPSReadWrite", "diskMBpsReadWrite", "diskIOPSReadOnly", "diskMBpsReadOnly",
		"maxShares", "tier", "encryption", "encryptionSettingsCollection", "diskAccessId",
		"networkAccessPolicy", "publicNetworkAccess", "securityProfile", "supportsHibernation",
		"supportedCapabilities", "purchasePlan", "completionPercent",
	} {
		if v, ok := props[field]; ok {
			diskProps[field] = v
		} else if v, ok := existingProps[field]; ok {
			diskProps[field] = v
		}
	}
	diskProps["diskState"] = diskState(diskProps)

	resp := map[string]interface{}{
		"id":         id,
		"name":       name,
		"type":       "Microsoft.Compute/disks",
		"location":   location,
		"tags":       tags,
		"sku":        sku,
		"etag":       "W/\"miniblue\"",
		"properties": diskProps,
	}
	if zones := asArray(input["zones"]); len(zones) > 0 {
		resp["zones"] = zones
	} else if zones := asArray(existing["zones"]); len(zones) > 0 {
		resp["zones"] = zones
	}
	return resp
}

func (h *Handler) CreateOrUpdate(w http.ResponseWriter, r *http.Request) {
	sub := chi.URLParam(r, "subscriptionId")
	rg := chi.URLParam(r, "resourceGroupName")
	name := chi.URLParam(r, "diskName")

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
	disk := buildResponse(sub, rg, name, input, asMap(existing))
	h.store.Set(k, disk)
	if exists {
		w.WriteHeader(http.StatusOK)
	} else {
		w.WriteHeader(http.StatusCreated)
	}
	json.NewEncoder(w).Encode(disk)
}

func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	sub := chi.URLParam(r, "subscriptionId")
	rg := chi.URLParam(r, "resourceGroupName")
	name := chi.URLParam(r, "diskName")

	k := h.key(sub, rg, name)
	existing, ok := h.store.Get(k)
	if !ok {
		azerr.NotFound(w, "Microsoft.Compute/disks", name)
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
	disk := buildResponse(sub, rg, name, mergePatch(existingMap, patch), existingMap)
	h.store.Set(k, disk)
	json.NewEncoder(w).Encode(disk)
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	sub := chi.URLParam(r, "subscriptionId")
	rg := chi.URLParam(r, "resourceGroupName")
	name := chi.URLParam(r, "diskName")

	v, ok := h.store.Get(h.key(sub, rg, name))
	if !ok {
		azerr.NotFound(w, "Microsoft.Compute/disks", name)
		return
	}
	json.NewEncoder(w).Encode(v)
}

func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	sub := chi.URLParam(r, "subscriptionId")
	rg := chi.URLParam(r, "resourceGroupName")
	name := chi.URLParam(r, "diskName")

	if !h.store.Delete(h.key(sub, rg, name)) {
		azerr.NotFound(w, "Microsoft.Compute/disks", name)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	sub := chi.URLParam(r, "subscriptionId")
	rg := chi.URLParam(r, "resourceGroupName")
	items := h.store.ListByPrefix("disk:" + sub + ":" + rg + ":")
	json.NewEncoder(w).Encode(map[string]interface{}{"value": items})
}

func (h *Handler) ListInSubscription(w http.ResponseWriter, r *http.Request) {
	sub := chi.URLParam(r, "subscriptionId")
	items := h.store.ListByPrefix("disk:" + sub + ":")
	json.NewEncoder(w).Encode(map[string]interface{}{"value": items})
}

func (h *Handler) BeginGetAccess(w http.ResponseWriter, r *http.Request) {
	sub := chi.URLParam(r, "subscriptionId")
	rg := chi.URLParam(r, "resourceGroupName")
	name := chi.URLParam(r, "diskName")
	if _, ok := h.store.Get(h.key(sub, rg, name)); !ok {
		azerr.NotFound(w, "Microsoft.Compute/disks", name)
		return
	}
	id := resourceID(sub, rg, name)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"accessSAS": fakeSASHost + id + "?sv=" + fakeSASVersion + "&sig=" + fakeSASSignature,
	})
}

func (h *Handler) EndGetAccess(w http.ResponseWriter, r *http.Request) {
	sub := chi.URLParam(r, "subscriptionId")
	rg := chi.URLParam(r, "resourceGroupName")
	name := chi.URLParam(r, "diskName")
	if _, ok := h.store.Get(h.key(sub, rg, name)); !ok {
		azerr.NotFound(w, "Microsoft.Compute/disks", name)
		return
	}
	json.NewEncoder(w).Encode(map[string]interface{}{})
}

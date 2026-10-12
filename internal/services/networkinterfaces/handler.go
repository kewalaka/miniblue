package networkinterfaces

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/moabukar/miniblue/internal/azerr"
	"github.com/moabukar/miniblue/internal/store"
)

var privateIPCounter uint32

type Handler struct {
	store *store.Store
}

func NewHandler(s *store.Store) *Handler {
	return &Handler{store: s}
}

func (h *Handler) Register(r chi.Router) {
	r.Route("/subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/Microsoft.Network/networkInterfaces", func(r chi.Router) {
		r.Get("/", h.List)
		r.Route("/{networkInterfaceName}", func(r chi.Router) {
			r.Put("/", h.CreateOrUpdate)
			r.Patch("/", h.Update)
			r.Get("/", h.Get)
			r.Delete("/", h.Delete)
		})
	})
	r.Get("/subscriptions/{subscriptionId}/providers/Microsoft.Network/networkInterfaces", h.ListInSubscription)
}

func (h *Handler) key(sub, rg, name string) string {
	return "nic:" + sub + ":" + rg + ":" + name
}

func resourceID(sub, rg, name string) string {
	return "/subscriptions/" + sub + "/resourceGroups/" + rg + "/providers/Microsoft.Network/networkInterfaces/" + name
}

func nextPrivateIP() string {
	n := atomic.AddUint32(&privateIPCounter, 1) + 3
	return fmt.Sprintf("10.0.%d.%d", (n/256)%256, n%256)
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

func optionalProps(dst, src map[string]interface{}, fields ...string) {
	for _, field := range fields {
		if v, ok := src[field]; ok {
			dst[field] = v
		}
	}
}

func existingIPConfig(existing map[string]interface{}, name string) map[string]interface{} {
	props := asMap(existing["properties"])
	for _, item := range asArray(props["ipConfigurations"]) {
		ipconfig := asMap(item)
		if ipconfig["name"] == name {
			return ipconfig
		}
	}
	return nil
}

func buildIPConfiguration(parentID, name string, input map[string]interface{}, primary bool, existing map[string]interface{}) map[string]interface{} {
	props := asMap(input["properties"])
	if props == nil {
		props = map[string]interface{}{}
	}
	existingProps := asMap(existing["properties"])

	allocationMethod, _ := props["privateIPAllocationMethod"].(string)
	if allocationMethod == "" {
		allocationMethod, _ = existingProps["privateIPAllocationMethod"].(string)
	}
	if allocationMethod == "" {
		allocationMethod = "Dynamic"
	}

	addressVersion, _ := props["privateIPAddressVersion"].(string)
	if addressVersion == "" {
		addressVersion, _ = existingProps["privateIPAddressVersion"].(string)
	}
	if addressVersion == "" {
		addressVersion = "IPv4"
	}

	privateIP, _ := props["privateIPAddress"].(string)
	if privateIP == "" {
		privateIP, _ = existingProps["privateIPAddress"].(string)
	}
	if privateIP == "" {
		privateIP = nextPrivateIP()
	}

	ipProps := map[string]interface{}{
		"provisioningState":         "Succeeded",
		"privateIPAddress":          privateIP,
		"privateIPAllocationMethod": allocationMethod,
		"privateIPAddressVersion":   addressVersion,
		"primary":                   primary,
	}

	if v, ok := props["primary"]; ok {
		ipProps["primary"] = v
	} else if v, ok := existingProps["primary"]; ok {
		ipProps["primary"] = v
	}

	optionalProps(ipProps, props,
		"subnet",
		"publicIPAddress",
		"applicationGatewayBackendAddressPools",
		"applicationSecurityGroups",
		"gatewayLoadBalancer",
		"loadBalancerBackendAddressPools",
		"loadBalancerInboundNatRules",
		"privateLinkConnectionProperties",
	)

	id := parentID + "/ipConfigurations/" + name
	return map[string]interface{}{
		"id":         id,
		"name":       name,
		"type":       "Microsoft.Network/networkInterfaces/ipConfigurations",
		"etag":       "W/\"miniblue\"",
		"properties": ipProps,
	}
}

func buildIPConfigurations(parentID string, props map[string]interface{}, existing map[string]interface{}) []interface{} {
	inputConfigs := asArray(props["ipConfigurations"])
	configs := make([]interface{}, 0, len(inputConfigs))
	for i, item := range inputConfigs {
		inputConfig := asMap(item)
		name, _ := inputConfig["name"].(string)
		if name == "" {
			name = fmt.Sprintf("ipconfig%d", i+1)
		}
		configs = append(configs, buildIPConfiguration(parentID, name, inputConfig, i == 0, existingIPConfig(existing, name)))
	}
	return configs
}

func buildResponse(sub, rg, name string, input map[string]interface{}, existing map[string]interface{}) map[string]interface{} {
	id := resourceID(sub, rg, name)

	props := asMap(input["properties"])
	if props == nil {
		props = map[string]interface{}{}
	}
	existingProps := asMap(existing["properties"])

	location, _ := input["location"].(string)
	if location == "" {
		location, _ = existing["location"].(string)
	}
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

	resourceGuid, _ := existingProps["resourceGuid"].(string)
	if resourceGuid == "" {
		resourceGuid = uuid.New().String()
	}

	macAddress, _ := existingProps["macAddress"].(string)
	if macAddress == "" {
		macAddress = strings.ReplaceAll(strings.ToUpper(uuid.New().String()[:17]), "-", "")
	}

	dnsSettings := asMap(props["dnsSettings"])
	if dnsSettings == nil {
		dnsSettings = asMap(existingProps["dnsSettings"])
	}
	if dnsSettings == nil {
		dnsSettings = map[string]interface{}{
			"dnsServers":               []interface{}{},
			"appliedDnsServers":        []interface{}{},
			"internalDomainNameSuffix": "internal.cloudapp.net",
		}
	}

	nicProps := map[string]interface{}{
		"provisioningState":           "Succeeded",
		"resourceGuid":                resourceGuid,
		"ipConfigurations":            buildIPConfigurations(id, props, existing),
		"dnsSettings":                 dnsSettings,
		"macAddress":                  macAddress,
		"primary":                     true,
		"enableAcceleratedNetworking": false,
		"enableIPForwarding":          false,
		"disableTcpStateTracking":     false,
		"hostedWorkloads":             []interface{}{},
		"tapConfigurations":           []interface{}{},
		"nicType":                     "Standard",
		"allowPort25Out":              true,
	}

	for _, field := range []string{"primary", "enableAcceleratedNetworking", "enableIPForwarding", "disableTcpStateTracking"} {
		if v, ok := props[field]; ok {
			nicProps[field] = v
		} else if v, ok := existingProps[field]; ok {
			nicProps[field] = v
		}
	}

	optionalProps(nicProps, props,
		"networkSecurityGroup",
		"virtualMachine",
		"dscpConfiguration",
		"migrationPhase",
		"workloadType",
		"auxiliaryMode",
		"auxiliarySku",
	)

	return map[string]interface{}{
		"id":         id,
		"name":       name,
		"type":       "Microsoft.Network/networkInterfaces",
		"location":   location,
		"tags":       tags,
		"etag":       "W/\"miniblue\"",
		"properties": nicProps,
	}
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

func (h *Handler) CreateOrUpdate(w http.ResponseWriter, r *http.Request) {
	sub := chi.URLParam(r, "subscriptionId")
	rg := chi.URLParam(r, "resourceGroupName")
	name := chi.URLParam(r, "networkInterfaceName")

	var input map[string]interface{}
	json.NewDecoder(r.Body).Decode(&input)
	if input == nil {
		input = map[string]interface{}{}
	}

	k := h.key(sub, rg, name)
	existing, exists := h.store.Get(k)
	nic := buildResponse(sub, rg, name, input, asMap(existing))
	h.store.Set(k, nic)
	h.refreshReferences(sub, rg)

	if exists {
		w.WriteHeader(http.StatusOK)
	} else {
		w.WriteHeader(http.StatusCreated)
	}
	json.NewEncoder(w).Encode(nic)
}

func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	sub := chi.URLParam(r, "subscriptionId")
	rg := chi.URLParam(r, "resourceGroupName")
	name := chi.URLParam(r, "networkInterfaceName")

	k := h.key(sub, rg, name)
	existing, ok := h.store.Get(k)
	if !ok {
		azerr.NotFound(w, "Microsoft.Network/networkInterfaces", name)
		return
	}

	var patch map[string]interface{}
	json.NewDecoder(r.Body).Decode(&patch)
	if patch == nil {
		patch = map[string]interface{}{}
	}

	existingMap := asMap(existing)
	nic := buildResponse(sub, rg, name, mergePatch(existingMap, patch), existingMap)
	h.store.Set(k, nic)
	h.refreshReferences(sub, rg)

	json.NewEncoder(w).Encode(nic)
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	sub := chi.URLParam(r, "subscriptionId")
	rg := chi.URLParam(r, "resourceGroupName")
	name := chi.URLParam(r, "networkInterfaceName")

	v, ok := h.store.Get(h.key(sub, rg, name))
	if !ok {
		azerr.NotFound(w, "Microsoft.Network/networkInterfaces", name)
		return
	}
	json.NewEncoder(w).Encode(v)
}

func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	sub := chi.URLParam(r, "subscriptionId")
	rg := chi.URLParam(r, "resourceGroupName")
	name := chi.URLParam(r, "networkInterfaceName")

	if !h.store.Delete(h.key(sub, rg, name)) {
		azerr.NotFound(w, "Microsoft.Network/networkInterfaces", name)
		return
	}
	h.refreshReferences(sub, rg)
	w.WriteHeader(http.StatusAccepted)
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	sub := chi.URLParam(r, "subscriptionId")
	rg := chi.URLParam(r, "resourceGroupName")
	items := h.store.ListByPrefix("nic:" + sub + ":" + rg + ":")
	json.NewEncoder(w).Encode(map[string]interface{}{"value": items})
}

func (h *Handler) ListInSubscription(w http.ResponseWriter, r *http.Request) {
	sub := chi.URLParam(r, "subscriptionId")
	items := h.store.ListByPrefix("nic:" + sub + ":")
	json.NewEncoder(w).Encode(map[string]interface{}{"value": items})
}

func reference(id string) map[string]interface{} {
	return map[string]interface{}{"id": id}
}

func idFromRef(v interface{}) string {
	m := asMap(v)
	id, _ := m["id"].(string)
	return id
}

func (h *Handler) refreshReferences(sub, rg string) {
	subnets := h.store.ListByPrefix("subnet:" + sub + ":" + rg + ":")
	for _, item := range subnets {
		if props := asMap(asMap(item)["properties"]); props != nil {
			props["ipConfigurations"] = []interface{}{}
		}
	}

	publicIPs := h.store.ListByPrefix("publicip:" + sub + ":" + rg + ":")
	for _, item := range publicIPs {
		if props := asMap(asMap(item)["properties"]); props != nil {
			props["ipConfiguration"] = nil
		}
	}

	nsgs := h.store.ListByPrefix("nsg:" + sub + ":" + rg + ":")
	for _, item := range nsgs {
		if props := asMap(asMap(item)["properties"]); props != nil {
			props["networkInterfaces"] = []interface{}{}
		}
	}

	for _, item := range h.store.ListByPrefix("nic:" + sub + ":" + rg + ":") {
		nic := asMap(item)
		nicID, _ := nic["id"].(string)
		props := asMap(nic["properties"])
		if nsgID := idFromRef(props["networkSecurityGroup"]); nsgID != "" {
			appendToMatchingResource(nsgs, nsgID, "networkInterfaces", reference(nicID))
		}
		for _, cfgItem := range asArray(props["ipConfigurations"]) {
			cfg := asMap(cfgItem)
			cfgID, _ := cfg["id"].(string)
			cfgProps := asMap(cfg["properties"])
			if subnetID := idFromRef(cfgProps["subnet"]); subnetID != "" {
				appendToMatchingResource(subnets, subnetID, "ipConfigurations", reference(cfgID))
			}
			if publicIPID := idFromRef(cfgProps["publicIPAddress"]); publicIPID != "" {
				setOnMatchingResource(publicIPs, publicIPID, "ipConfiguration", reference(cfgID))
			}
		}
	}
}

func appendToMatchingResource(resources []interface{}, targetID, property string, ref map[string]interface{}) {
	for _, item := range resources {
		resource := asMap(item)
		id, _ := resource["id"].(string)
		if !strings.EqualFold(id, targetID) {
			continue
		}
		props := asMap(resource["properties"])
		props[property] = append(asArray(props[property]), ref)
		return
	}
}

func setOnMatchingResource(resources []interface{}, targetID, property string, ref map[string]interface{}) {
	for _, item := range resources {
		resource := asMap(item)
		id, _ := resource["id"].(string)
		if !strings.EqualFold(id, targetID) {
			continue
		}
		props := asMap(resource["properties"])
		props[property] = ref
		return
	}
}

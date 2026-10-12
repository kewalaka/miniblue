package network

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/moabukar/miniblue/internal/azerr"
	"github.com/moabukar/miniblue/internal/store"
)

// The struct definitions below mirror the Microsoft.Network/virtualNetworks
// (and …/subnets) shape from API version 2023-11-01. They are not used by the
// handlers (which build map[string]interface{} responses for full control over
// JSON shape and conditional field emission) but kept as documentation of the
// fields miniblue is aware of.

type VNet struct {
	ID               string                 `json:"id"`
	Name             string                 `json:"name"`
	Type             string                 `json:"type"`
	Location         string                 `json:"location"`
	Etag             string                 `json:"etag,omitempty"`
	Tags             map[string]string      `json:"tags,omitempty"`
	ExtendedLocation *ExtendedLocation      `json:"extendedLocation,omitempty"`
	Properties       VNetProps              `json:"properties"`
}

type ExtendedLocation struct {
	Name string `json:"name,omitempty"`
	Type string `json:"type,omitempty"` // EdgeZone
}

type VNetProps struct {
	ProvisioningState           string             `json:"provisioningState"`
	ResourceGuid                string             `json:"resourceGuid,omitempty"`
	AddressSpace                AddressSpace       `json:"addressSpace"`
	DhcpOptions                 *DhcpOptions       `json:"dhcpOptions,omitempty"`
	Subnets                     []SubnetRef        `json:"subnets,omitempty"`
	VirtualNetworkPeerings      []interface{}      `json:"virtualNetworkPeerings,omitempty"`
	EnableDdosProtection        bool               `json:"enableDdosProtection,omitempty"`
	EnableVmProtection          bool               `json:"enableVmProtection,omitempty"`
	DdosProtectionPlan          *SubResourceRef    `json:"ddosProtectionPlan,omitempty"`
	BgpCommunities              *BgpCommunities    `json:"bgpCommunities,omitempty"`
	Encryption                  *VNetEncryption    `json:"encryption,omitempty"`
	FlowLogs                    []interface{}      `json:"flowLogs,omitempty"` // ReadOnly
	FlowTimeoutInMinutes        int                `json:"flowTimeoutInMinutes,omitempty"`
	IPAllocations               []SubResourceRef   `json:"ipAllocations,omitempty"`
	PrivateEndpointVNetPolicies string             `json:"privateEndpointVNetPolicies,omitempty"`
}

type AddressSpace struct {
	AddressPrefixes []string `json:"addressPrefixes"`
}

type DhcpOptions struct {
	DnsServers []string `json:"dnsServers"`
}

type BgpCommunities struct {
	VirtualNetworkCommunity string `json:"virtualNetworkCommunity"`
	RegionalCommunity       string `json:"regionalCommunity,omitempty"` // ReadOnly
}

type VNetEncryption struct {
	Enabled     bool   `json:"enabled"`
	Enforcement string `json:"enforcement,omitempty"` // DropUnencrypted | AllowUnencrypted
}

type SubnetRef struct {
	ID         string      `json:"id"`
	Name       string      `json:"name"`
	Type       string      `json:"type,omitempty"`
	Etag       string      `json:"etag,omitempty"`
	Properties SubnetProps `json:"properties"`
}

type SubnetProps struct {
	ProvisioningState                  string           `json:"provisioningState"`
	AddressPrefix                      string           `json:"addressPrefix,omitempty"`
	AddressPrefixes                    []string         `json:"addressPrefixes,omitempty"`
	NetworkSecurityGroup               *SubResourceRef  `json:"networkSecurityGroup,omitempty"`
	RouteTable                         *SubResourceRef  `json:"routeTable,omitempty"`
	NatGateway                         *SubResourceRef  `json:"natGateway,omitempty"`
	ServiceEndpoints                   []interface{}    `json:"serviceEndpoints"`
	ServiceEndpointPolicies            []interface{}    `json:"serviceEndpointPolicies"`
	Delegations                        []interface{}    `json:"delegations"`
	IPConfigurations                   []interface{}    `json:"ipConfigurations"`           // ReadOnly
	IPConfigurationProfiles            []interface{}    `json:"ipConfigurationProfiles"`    // ReadOnly
	IPAllocations                      []SubResourceRef `json:"ipAllocations,omitempty"`
	ApplicationGatewayIPConfigurations []interface{}    `json:"applicationGatewayIPConfigurations,omitempty"`
	PrivateEndpoints                   []interface{}    `json:"privateEndpoints"`           // ReadOnly
	ResourceNavigationLinks            []interface{}    `json:"resourceNavigationLinks"`    // ReadOnly
	ServiceAssociationLinks            []interface{}    `json:"serviceAssociationLinks"`    // ReadOnly
	PrivateEndpointNetworkPolicies     string           `json:"privateEndpointNetworkPolicies"`
	PrivateLinkServiceNetworkPolicies  string           `json:"privateLinkServiceNetworkPolicies"`
	DefaultOutboundAccess              bool             `json:"defaultOutboundAccess"`
	SharingScope                       string           `json:"sharingScope,omitempty"` // Tenant | DelegatedServices
	Purpose                            string           `json:"purpose,omitempty"`      // ReadOnly
}

type SubResourceRef struct {
	ID string `json:"id"`
}

type Handler struct {
	store *store.Store
}

func NewHandler(s *store.Store) *Handler {
	return &Handler{store: s}
}

func (h *Handler) Register(r chi.Router) {
	r.Route("/subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/Microsoft.Network/virtualNetworks", func(r chi.Router) {
		r.Get("/", h.ListVNets)
		r.Route("/{vnetName}", func(r chi.Router) {
			r.Put("/", h.CreateOrUpdateVNet)
			r.Get("/", h.GetVNet)
			r.Delete("/", h.DeleteVNet)
			r.Route("/subnets", func(r chi.Router) {
				r.Get("/", h.ListSubnets)
				r.Route("/{subnetName}", func(r chi.Router) {
					r.Put("/", h.CreateOrUpdateSubnet)
					r.Get("/", h.GetSubnet)
					r.Delete("/", h.DeleteSubnet)
				})
			})
		})
	})
	r.Get("/subscriptions/{subscriptionId}/providers/Microsoft.Network/virtualNetworks", h.ListVNetsInSubscription)
}

func (h *Handler) ListVNetsInSubscription(w http.ResponseWriter, r *http.Request) {
	sub := chi.URLParam(r, "subscriptionId")
	items := h.store.ListByPrefix("vnet:" + sub + ":")
	json.NewEncoder(w).Encode(map[string]interface{}{"value": items})
}

func (h *Handler) vnetKey(sub, rg, name string) string {
	return "vnet:" + sub + ":" + rg + ":" + name
}

func (h *Handler) subnetKey(sub, rg, vnet, subnet string) string {
	return "subnet:" + sub + ":" + rg + ":" + vnet + ":" + subnet
}

func buildVNetResponse(sub, rg, name string, input map[string]interface{}) map[string]interface{} {
	id := "/subscriptions/" + sub + "/resourceGroups/" + rg + "/providers/Microsoft.Network/virtualNetworks/" + name

	props, _ := input["properties"].(map[string]interface{})
	if props == nil {
		props = map[string]interface{}{}
	}
	addrSpace, _ := props["addressSpace"].(map[string]interface{})
	if addrSpace == nil {
		addrSpace = map[string]interface{}{"addressPrefixes": []interface{}{}}
	}

	location, _ := input["location"].(string)
	if location == "" {
		location = "eastus"
	}

	tags, _ := input["tags"].(map[string]interface{})
	if tags == nil {
		tags = map[string]interface{}{}
	}

	respProps := map[string]interface{}{
		"provisioningState":      "Succeeded",
		"resourceGuid":           uuid.New().String(),
		"addressSpace":           addrSpace,
		"dhcpOptions":            map[string]interface{}{"dnsServers": []interface{}{}},
		"subnets":                []interface{}{},
		"virtualNetworkPeerings": []interface{}{},
		"enableDdosProtection":   false,
		"enableVmProtection":     false,
		// ReadOnly collection — Azure always returns an array (possibly empty).
		"flowLogs": []interface{}{},
	}

	// dhcpOptions: echo caller-provided value (e.g. custom DNS servers).
	if v, ok := props["dhcpOptions"].(map[string]interface{}); ok {
		respProps["dhcpOptions"] = v
	}

	// Optional scalars / sub-objects: only echo when the caller actually set
	// them on PUT, so GET reflects exactly what was written. This is the same
	// pattern as `privateEndpointVNetPolicies` and avoids fabricating values
	// that would cause Terraform azurerm v4 to report phantom diffs.
	for _, k := range []string{
		"privateEndpointVNetPolicies",
		"flowTimeoutInMinutes",
		"bgpCommunities",
		"ddosProtectionPlan",
		"encryption",
		"ipAllocations",
	} {
		if v, ok := props[k]; ok {
			respProps[k] = v
		}
	}

	resp := map[string]interface{}{
		"id":         id,
		"name":       name,
		"type":       "Microsoft.Network/virtualNetworks",
		"location":   location,
		"tags":       tags,
		"etag":       "W/\"miniblue\"",
		"properties": respProps,
	}

	// extendedLocation is a top-level field on the resource (Edge Zone support).
	if el, ok := input["extendedLocation"]; ok {
		resp["extendedLocation"] = el
	}

	return resp
}

func (h *Handler) CreateOrUpdateVNet(w http.ResponseWriter, r *http.Request) {
	sub := chi.URLParam(r, "subscriptionId")
	rg := chi.URLParam(r, "resourceGroupName")
	name := chi.URLParam(r, "vnetName")

	var input map[string]interface{}
	json.NewDecoder(r.Body).Decode(&input)

	vnet := buildVNetResponse(sub, rg, name, input)
	k := h.vnetKey(sub, rg, name)
	_, exists := h.store.Get(k)
	h.store.Set(k, vnet)
	h.persistInlineSubnets(sub, rg, name, input)
	h.updateVNetSubnets(sub, rg, name)
	if updated, ok := h.store.Get(k); ok {
		if updatedVNet, ok := updated.(map[string]interface{}); ok {
			vnet = updatedVNet
		}
	}

	if exists {
		w.WriteHeader(http.StatusOK)
	} else {
		w.WriteHeader(http.StatusCreated)
	}
	json.NewEncoder(w).Encode(vnet)
}

func (h *Handler) persistInlineSubnets(sub, rg, vnetName string, input map[string]interface{}) {
	props, _ := input["properties"].(map[string]interface{})
	if props == nil {
		return
	}
	rawSubnets, ok := props["subnets"].([]interface{})
	if !ok {
		return
	}

	h.store.DeleteByPrefix(h.subnetKey(sub, rg, vnetName, ""))
	for _, rawSubnet := range rawSubnets {
		subnetInput, ok := rawSubnet.(map[string]interface{})
		if !ok {
			continue
		}
		subnetName, _ := subnetInput["name"].(string)
		if subnetName == "" {
			continue
		}
		subnet := buildSubnetResponse(sub, rg, vnetName, subnetName, subnetInput)
		h.store.Set(h.subnetKey(sub, rg, vnetName, subnetName), subnet)
	}
}

func (h *Handler) GetVNet(w http.ResponseWriter, r *http.Request) {
	sub := chi.URLParam(r, "subscriptionId")
	rg := chi.URLParam(r, "resourceGroupName")
	name := chi.URLParam(r, "vnetName")

	v, ok := h.store.Get(h.vnetKey(sub, rg, name))
	if !ok {
		azerr.NotFound(w, "Microsoft.Network/virtualNetworks", name)
		return
	}

	// Re-populate subnets from the store
	if vnet, ok := v.(map[string]interface{}); ok {
		subnetItems := h.store.ListByPrefix(h.subnetKey(sub, rg, name, ""))
		if props, ok := vnet["properties"].(map[string]interface{}); ok {
			if len(subnetItems) > 0 {
				props["subnets"] = subnetItems
			} else {
				props["subnets"] = []interface{}{}
			}
		}
		json.NewEncoder(w).Encode(vnet)
		return
	}
	json.NewEncoder(w).Encode(v)
}

func (h *Handler) DeleteVNet(w http.ResponseWriter, r *http.Request) {
	sub := chi.URLParam(r, "subscriptionId")
	rg := chi.URLParam(r, "resourceGroupName")
	name := chi.URLParam(r, "vnetName")
	if !h.store.Delete(h.vnetKey(sub, rg, name)) {
		azerr.NotFound(w, "Microsoft.Network/virtualNetworks", name)
		return
	}
	// Clean up subnets
	h.store.DeleteByPrefix(h.subnetKey(sub, rg, name, ""))
	w.WriteHeader(http.StatusAccepted)
}

func (h *Handler) ListVNets(w http.ResponseWriter, r *http.Request) {
	sub := chi.URLParam(r, "subscriptionId")
	rg := chi.URLParam(r, "resourceGroupName")
	items := h.store.ListByPrefix("vnet:" + sub + ":" + rg + ":")
	json.NewEncoder(w).Encode(map[string]interface{}{"value": items})
}

// buildSubnetResponse builds a raw map matching the Azure 2023-11-01 subnet schema exactly.
// Using a map instead of a struct gives us full control over JSON field names and nil handling.
func buildSubnetResponse(sub, rg, vnetName, subnetName string, input map[string]interface{}) map[string]interface{} {
	id := "/subscriptions/" + sub + "/resourceGroups/" + rg + "/providers/Microsoft.Network/virtualNetworks/" + vnetName + "/subnets/" + subnetName

	props, _ := input["properties"].(map[string]interface{})
	if props == nil {
		props = map[string]interface{}{}
	}

	// Extract addressPrefix(es)
	prefix, _ := props["addressPrefix"].(string)
	prefixes, _ := props["addressPrefixes"].([]interface{})
	if prefix == "" && len(prefixes) > 0 {
		prefix, _ = prefixes[0].(string)
	}
	if len(prefixes) == 0 && prefix != "" {
		prefixes = []interface{}{prefix}
	}

	subnetProps := map[string]interface{}{
		"provisioningState":                 "Succeeded",
		"addressPrefix":                     prefix,
		"addressPrefixes":                   prefixes,
		"serviceEndpoints":                  []interface{}{},
		"serviceEndpointPolicies":           []interface{}{},
		"delegations":                       []interface{}{},
		"privateEndpointNetworkPolicies":    "Disabled",
		"privateLinkServiceNetworkPolicies": "Enabled",
		"defaultOutboundAccess":             true,
		// ReadOnly collections — Azure always returns these as (possibly empty)
		// arrays, so emit them to avoid Terraform phantom diffs on refresh.
		"ipConfigurations":        []interface{}{},
		"ipConfigurationProfiles": []interface{}{},
		"privateEndpoints":        []interface{}{},
		"resourceNavigationLinks": []interface{}{},
		"serviceAssociationLinks": []interface{}{},
	}

	if nsg, ok := props["networkSecurityGroup"]; ok {
		subnetProps["networkSecurityGroup"] = nsg
	}

	if rt, ok := props["routeTable"]; ok {
		subnetProps["routeTable"] = rt
	}

	// Other optional scalars / sub-objects: only echo when the caller set them
	// on PUT, so GET reflects exactly what was written and Terraform azurerm v4
	// doesn't see phantom diffs for unspecified fields.
	for _, k := range []string{
		"natGateway",
		"sharingScope",
		"ipAllocations",
		"applicationGatewayIPConfigurations",
	} {
		if v, ok := props[k]; ok {
			subnetProps[k] = v
		}
	}

	return map[string]interface{}{
		"id":         id,
		"name":       subnetName,
		"etag":       "W/\"miniblue\"",
		"type":       "Microsoft.Network/virtualNetworks/subnets",
		"properties": subnetProps,
	}
}

func (h *Handler) CreateOrUpdateSubnet(w http.ResponseWriter, r *http.Request) {
	sub := chi.URLParam(r, "subscriptionId")
	rg := chi.URLParam(r, "resourceGroupName")
	vnetName := chi.URLParam(r, "vnetName")
	subnetName := chi.URLParam(r, "subnetName")

	if !h.store.Exists(h.vnetKey(sub, rg, vnetName)) {
		azerr.NotFound(w, "Microsoft.Network/virtualNetworks", vnetName)
		return
	}

	var input map[string]interface{}
	json.NewDecoder(r.Body).Decode(&input)

	subnet := buildSubnetResponse(sub, rg, vnetName, subnetName, input)
	k := h.subnetKey(sub, rg, vnetName, subnetName)
	_, exists := h.store.Get(k)
	h.store.Set(k, subnet)
	h.updateVNetSubnets(sub, rg, vnetName)

	if exists {
		w.WriteHeader(http.StatusOK)
	} else {
		w.WriteHeader(http.StatusCreated)
	}
	json.NewEncoder(w).Encode(subnet)
}

func (h *Handler) GetSubnet(w http.ResponseWriter, r *http.Request) {
	sub := chi.URLParam(r, "subscriptionId")
	rg := chi.URLParam(r, "resourceGroupName")
	vnetName := chi.URLParam(r, "vnetName")
	subnetName := chi.URLParam(r, "subnetName")

	v, ok := h.store.Get(h.subnetKey(sub, rg, vnetName, subnetName))
	if !ok {
		azerr.NotFound(w, "Microsoft.Network/virtualNetworks/subnets", subnetName)
		return
	}
	json.NewEncoder(w).Encode(v)
}

func (h *Handler) DeleteSubnet(w http.ResponseWriter, r *http.Request) {
	sub := chi.URLParam(r, "subscriptionId")
	rg := chi.URLParam(r, "resourceGroupName")
	vnetName := chi.URLParam(r, "vnetName")
	subnetName := chi.URLParam(r, "subnetName")

	if !h.store.Delete(h.subnetKey(sub, rg, vnetName, subnetName)) {
		azerr.NotFound(w, "Microsoft.Network/virtualNetworks/subnets", subnetName)
		return
	}
	// Update the parent VNet's subnets array
	h.updateVNetSubnets(sub, rg, vnetName)
	w.WriteHeader(http.StatusAccepted)
}

func (h *Handler) ListSubnets(w http.ResponseWriter, r *http.Request) {
	sub := chi.URLParam(r, "subscriptionId")
	rg := chi.URLParam(r, "resourceGroupName")
	vnetName := chi.URLParam(r, "vnetName")
	items := h.store.ListByPrefix(h.subnetKey(sub, rg, vnetName, ""))
	json.NewEncoder(w).Encode(map[string]interface{}{"value": items})
}

// updateVNetSubnets refreshes the subnets array stored inside the parent VNet.
func (h *Handler) updateVNetSubnets(sub, rg, vnetName string) {
	vk := h.vnetKey(sub, rg, vnetName)
	v, ok := h.store.Get(vk)
	if !ok {
		return
	}
	vnet, ok := v.(map[string]interface{})
	if !ok {
		return
	}
	subnetItems := h.store.ListByPrefix(h.subnetKey(sub, rg, vnetName, ""))
	if props, ok := vnet["properties"].(map[string]interface{}); ok {
		if len(subnetItems) > 0 {
			props["subnets"] = subnetItems
		} else {
			props["subnets"] = []interface{}{}
		}
	}
	h.store.Set(vk, vnet)
}

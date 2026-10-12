package storageaccounts

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/moabukar/miniblue/internal/azerr"
	"github.com/moabukar/miniblue/internal/storageauth"
	"github.com/moabukar/miniblue/internal/store"
)

const (
	envStorageEndpoint = "MINIBLUE_STORAGE_ENDPOINT"
)

// Store key prefixes match the historical blob handler so existing persisted state stays valid.

type Handler struct {
	store *store.Store
}

func NewHandler(s *store.Store) *Handler {
	return &Handler{store: s}
}

func (h *Handler) Register(r chi.Router) {
	// Subscription-scoped list (azurerm and Azure SDK use this after create/update).
	r.Get("/subscriptions/{subscriptionId}/providers/Microsoft.Storage/storageAccounts", h.ListStorageAccountsInSubscription)

	r.Route("/subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/Microsoft.Storage/storageAccounts", func(r chi.Router) {
		r.Get("/", h.ListStorageAccounts)
		r.Route("/{accountName}", func(r chi.Router) {
			r.Put("/", h.CreateOrUpdateStorageAccount)
			r.Get("/", h.GetStorageAccount)
			r.Delete("/", h.DeleteStorageAccount)
			r.Post("/listKeys", h.ListKeys)
			r.Put("/managementPolicies/default", h.SetManagementPolicy)
			r.Get("/managementPolicies/default", h.GetManagementPolicy)
			r.Delete("/managementPolicies/default", h.DeleteManagementPolicy)

			// Blob service
			r.Get("/blobServices/default", h.GetBlobServiceProperties)
			r.Put("/blobServices/default", h.SetBlobServiceProperties)
			r.Patch("/blobServices/default", h.PatchBlobServiceProperties)
			r.Route("/blobServices/default/containers", func(r chi.Router) {
				r.Get("/", h.ListContainersARM)
				r.Route("/{containerName}", func(r chi.Router) {
					r.Put("/", h.CreateContainerARM)
					r.Get("/", h.GetContainerARM)
					r.Delete("/", h.DeleteContainerARM)
				})
			})

			// File service
			r.Get("/fileServices/default", h.GetFileServiceProperties)
			r.Put("/fileServices/default", h.SetFileServiceProperties)
			r.Patch("/fileServices/default", h.PatchFileServiceProperties)

			// Queue service
			r.Get("/queueServices/default", h.GetQueueServiceProperties)
			r.Put("/queueServices/default", h.SetQueueServiceProperties)
			r.Patch("/queueServices/default", h.PatchQueueServiceProperties)

			// Table service
			r.Get("/tableServices/default", h.GetTableServiceProperties)
			r.Put("/tableServices/default", h.SetTableServiceProperties)
			r.Patch("/tableServices/default", h.PatchTableServiceProperties)
		})
	})
}

func (h *Handler) CreateOrUpdateStorageAccount(w http.ResponseWriter, r *http.Request) {
	sub := chi.URLParam(r, "subscriptionId")
	rg := chi.URLParam(r, "resourceGroupName")
	name := chi.URLParam(r, "accountName")

	var input map[string]interface{}
	json.NewDecoder(r.Body).Decode(&input)

	acct := h.buildStorageAccountResponse(sub, rg, name, input)
	h.store.Set(h.storageAccountKey(sub, rg, name), acct)
	storageauth.PersistSharedKeyContext(h.store, sub, rg, name)

	// Azure ARM and the azurerm Terraform provider expect 200 OK for PUT create/update,
	// not 201 Created (the Go SDK rejects 201 for this operation).
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(acct)
}

func (h *Handler) GetStorageAccount(w http.ResponseWriter, r *http.Request) {
	sub := chi.URLParam(r, "subscriptionId")
	rg := chi.URLParam(r, "resourceGroupName")
	name := chi.URLParam(r, "accountName")

	v, ok := h.store.Get(h.storageAccountKey(sub, rg, name))
	if !ok {
		azerr.NotFound(w, "Microsoft.Storage/storageAccounts", name)
		return
	}
	json.NewEncoder(w).Encode(v)
}

func (h *Handler) DeleteStorageAccount(w http.ResponseWriter, r *http.Request) {
	sub := chi.URLParam(r, "subscriptionId")
	rg := chi.URLParam(r, "resourceGroupName")
	name := chi.URLParam(r, "accountName")

	// Delete Storage Account is a synchronous operation in the real Azure
	// Storage RP: it returns 200 OK when the account existed and was deleted,
	// or 204 No Content when it did not. Returning a bare 202 Accepted (with no
	// body and no Azure-AsyncOperation/Location poll URL) makes the azurerm
	// provider / Go SDK fail with "unexpected status 202 received with no body".
	if !h.store.Delete(h.storageAccountKey(sub, rg, name)) {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	storageauth.DeleteSharedKeyContext(h.store, name)
	w.WriteHeader(http.StatusOK)
}

// ListKeys implements POST .../storageAccounts/{accountName}/listKeys (Azure Storage RP).
// azurerm calls this to obtain account keys for the data-plane poller.
func (h *Handler) ListKeys(w http.ResponseWriter, r *http.Request) {
	sub := chi.URLParam(r, "subscriptionId")
	rg := chi.URLParam(r, "resourceGroupName")
	name := chi.URLParam(r, "accountName")

	if _, ok := h.store.Get(h.storageAccountKey(sub, rg, name)); !ok {
		azerr.NotFound(w, "Microsoft.Storage/storageAccounts", name)
		return
	}

	keys := []map[string]interface{}{
		{"keyName": "key1", "value": storageauth.DeterministicAccountKey(sub, rg, name, "1"), "permissions": "Full"},
		{"keyName": "key2", "value": storageauth.DeterministicAccountKey(sub, rg, name, "2"), "permissions": "Full"},
	}
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{"keys": keys})
}

func (h *Handler) ListStorageAccounts(w http.ResponseWriter, r *http.Request) {
	sub := chi.URLParam(r, "subscriptionId")
	rg := chi.URLParam(r, "resourceGroupName")
	items := h.store.ListByPrefix("blob:account:" + sub + ":" + rg + ":")
	json.NewEncoder(w).Encode(map[string]interface{}{"value": items})
}

func (h *Handler) ListStorageAccountsInSubscription(w http.ResponseWriter, r *http.Request) {
	sub := chi.URLParam(r, "subscriptionId")
	// Keys are blob:account:{sub}:{rg}:{name}
	items := h.store.ListByPrefix("blob:account:" + sub + ":")
	json.NewEncoder(w).Encode(map[string]interface{}{"value": items})
}

// Private helper functions
func (h *Handler) storageAccountKey(sub, rg, name string) string {
	return "blob:account:" + sub + ":" + rg + ":" + name
}

// servicePropsKey is the per-(account,serviceType) key used to persist the body
// of PUT/PATCH on the {blob,file,queue,table}Services/default sub-resource so
// GET reflects exactly what was last written.
func (h *Handler) servicePropsKey(sub, rg, account, serviceType string) string {
	return "blob:svcprops:" + sub + ":" + rg + ":" + account + ":" + serviceType
}

func writeServiceNotFound(w http.ResponseWriter, resourceType, name string) {
	azerr.NotFound(w, resourceType, name)
}

// deriveSkuTier returns the tier ("Standard" or "Premium") implied by an Azure
// storage SKU name. The 2025-01-01 schema marks `sku.tier` as ReadOnly and
// derived from `sku.name`, so we should not let callers spoof it.
func deriveSkuTier(skuName string) string {
	if skuName == "" {
		return "Standard"
	}
	if strings.HasPrefix(skuName, "Premium") {
		return "Premium"
	}
	return "Standard"
}

func defaultEncryption(now string) map[string]interface{} {
	svc := func() map[string]interface{} {
		return map[string]interface{}{
			"enabled":         true,
			"keyType":         "Account",
			"lastEnabledTime": now,
		}
	}
	return map[string]interface{}{
		"keySource":                       "Microsoft.Storage",
		"requireInfrastructureEncryption": false,
		"services": map[string]interface{}{
			"blob":  svc(),
			"file":  svc(),
			"queue": svc(),
			"table": svc(),
		},
	}
}

func defaultNetworkAcls() map[string]interface{} {
	return map[string]interface{}{
		"bypass":              "AzureServices",
		"defaultAction":       "Allow",
		"ipRules":             []interface{}{},
		"virtualNetworkRules": []interface{}{},
		"resourceAccessRules": []interface{}{},
	}
}

// buildServicePropertiesResponse constructs the schema-faithful response body
// for the {blob,file,queue,table}Services/default sub-resource. When the
// caller-supplied PUT/PATCH body contains a recognised property field, the
// caller's value is echoed (so GET reflects exactly what was written).
func (h *Handler) buildServicePropertiesResponse(sub, rg, account, serviceType string, input map[string]interface{}) map[string]interface{} {
	id := "/subscriptions/" + sub + "/resourceGroups/" + rg + "/providers/Microsoft.Storage/storageAccounts/" + account + "/" + serviceType + "/default"

	var props map[string]interface{}
	var echoFields []string

	switch serviceType {
	case "blobServices":
		props = map[string]interface{}{
			"cors": map[string]interface{}{"corsRules": []interface{}{}},
			"deleteRetentionPolicy": map[string]interface{}{
				"enabled":              true,
				"days":                 7,
				"allowPermanentDelete": false,
			},
			"containerDeleteRetentionPolicy": map[string]interface{}{
				"enabled": false,
			},
			"changeFeed":          map[string]interface{}{"enabled": false},
			"isVersioningEnabled": false,
			"restorePolicy":       map[string]interface{}{"enabled": false},
		}
		echoFields = []string{
			"cors", "deleteRetentionPolicy", "containerDeleteRetentionPolicy",
			"changeFeed", "isVersioningEnabled", "restorePolicy",
			"lastAccessTimeTrackingPolicy", "defaultServiceVersion",
		}
	case "fileServices":
		props = map[string]interface{}{
			"cors": map[string]interface{}{"corsRules": []interface{}{}},
			"shareDeleteRetentionPolicy": map[string]interface{}{
				"enabled": true,
				"days":    7,
			},
			"protocolSettings": map[string]interface{}{
				"smb": map[string]interface{}{},
			},
		}
		echoFields = []string{"cors", "shareDeleteRetentionPolicy", "protocolSettings"}
	default: // queueServices, tableServices
		props = map[string]interface{}{
			"cors": map[string]interface{}{"corsRules": []interface{}{}},
		}
		echoFields = []string{"cors"}
	}

	if inProps, ok := input["properties"].(map[string]interface{}); ok {
		for _, k := range echoFields {
			if v, ok := inProps[k]; ok {
				props[k] = v
			}
		}
	}

	resp := map[string]interface{}{
		"id":         id,
		"name":       "default",
		"type":       "Microsoft.Storage/storageAccounts/" + serviceType,
		"properties": props,
	}

	// blobServices and fileServices include a `sku` (mirrors the parent
	// account); queueServices and tableServices do not.
	if serviceType == "blobServices" || serviceType == "fileServices" {
		sku := map[string]interface{}{
			"name": "Standard_LRS",
			"tier": "Standard",
		}
		if acct, ok := h.store.Get(h.storageAccountKey(sub, rg, account)); ok {
			if a, ok := acct.(map[string]interface{}); ok {
				if s, ok := a["sku"].(map[string]interface{}); ok {
					sku = s
				}
			}
		}
		resp["sku"] = sku
	}

	return resp
}

func (h *Handler) buildStorageAccountResponse(sub, rg, name string, input map[string]interface{}) map[string]interface{} {
	nowUTC := time.Now().UTC().Format(time.RFC3339)

	var blobEndpoint, queueEndpoint, tableEndpoint, fileEndpoint, dfsEndpoint, webEndpoint string
	localEndpoint := os.Getenv(envStorageEndpoint)

	if localEndpoint != "" {
		blobEndpoint = localEndpoint + "/blob/" + name + "/"
		queueEndpoint = localEndpoint + "/queue/" + name + "/"
		tableEndpoint = localEndpoint + "/table/" + name + "/"
		fileEndpoint = localEndpoint + "/file/" + name + "/"
		dfsEndpoint = localEndpoint + "/dfs/" + name + "/"
		webEndpoint = localEndpoint + "/web/" + name + "/"
	} else {
		blobEndpoint = "https://" + name + ".blob.core.windows.net/"
		queueEndpoint = "https://" + name + ".queue.core.windows.net/"
		tableEndpoint = "https://" + name + ".table.core.windows.net/"
		fileEndpoint = "https://" + name + ".file.core.windows.net/"
		dfsEndpoint = "https://" + name + ".dfs.core.windows.net/"
		webEndpoint = "https://" + name + ".z01.web.core.windows.net/"
	}

	location := "eastus"
	kind := "StorageV2"
	skuName := "Standard_LRS"
	tags := map[string]interface{}{}

	if input != nil {
		if v, ok := input["location"].(string); ok && v != "" {
			location = v
		}
		if v, ok := input["kind"].(string); ok && v != "" {
			kind = v
		}
		if t, ok := input["tags"].(map[string]interface{}); ok && t != nil {
			tags = t
		}
		if s, ok := input["sku"].(map[string]interface{}); ok {
			if n, ok := s["name"].(string); ok && n != "" {
				skuName = n
			}
		}
	}

	sku := map[string]interface{}{
		"name": skuName,
		// `tier` is ReadOnly per the 2025-01-01 schema and derived from `name`.
		"tier": deriveSkuTier(skuName),
	}

	props := map[string]interface{}{
		"provisioningState":            "Succeeded",
		"creationTime":                 nowUTC,
		"primaryLocation":              location,
		"statusOfPrimary":              "available",
		"supportsHttpsTrafficOnly":     true,
		"allowBlobPublicAccess":        false,
		"allowSharedKeyAccess":         true,
		"allowCrossTenantReplication":  false,
		"defaultToOAuthAuthentication": false,
		"minimumTlsVersion":            "TLS1_2",
		"publicNetworkAccess":          "Enabled",
		"dnsEndpointType":              "Standard",
		"isHnsEnabled":                 false,
		"isNfsV3Enabled":               false,
		"isSftpEnabled":                false,
		"isLocalUserEnabled":           false,
		"encryption":                   defaultEncryption(nowUTC),
		"networkAcls":                  defaultNetworkAcls(),
		"keyCreationTime": map[string]interface{}{
			"key1": nowUTC,
			"key2": nowUTC,
		},
		"primaryEndpoints": map[string]interface{}{
			"blob":  blobEndpoint,
			"queue": queueEndpoint,
			"table": tableEndpoint,
			"file":  fileEndpoint,
			"dfs":   dfsEndpoint,
			"web":   webEndpoint,
		},
		// ReadOnly collection — Azure always returns an array (possibly empty).
		"privateEndpointConnections": []interface{}{},
	}

	// Echo caller-provided property fields when present, so GET reflects what
	// was written and Terraform azurerm v4 doesn't see phantom diffs.
	if inProps, ok := input["properties"].(map[string]interface{}); ok {
		for _, k := range []string{
			"accessTier",
			"allowBlobPublicAccess",
			"allowSharedKeyAccess",
			"allowCrossTenantReplication",
			"allowedCopyScope",
			"defaultToOAuthAuthentication",
			"minimumTlsVersion",
			"publicNetworkAccess",
			"dnsEndpointType",
			"isHnsEnabled",
			"isNfsV3Enabled",
			"isSftpEnabled",
			"isLocalUserEnabled",
			"supportsHttpsTrafficOnly",
			"largeFileSharesState",
			"enableExtendedGroups",
			"azureFilesIdentityBasedAuthentication",
			"customDomain",
			"encryption",
			"networkAcls",
			"routingPreference",
			"sasPolicy",
			"keyPolicy",
			"immutableStorageWithVersioning",
		} {
			if v, ok := inProps[k]; ok {
				props[k] = v
			}
		}
	}

	resp := map[string]interface{}{
		"id":         "/subscriptions/" + sub + "/resourceGroups/" + rg + "/providers/Microsoft.Storage/storageAccounts/" + name,
		"name":       name,
		"type":       "Microsoft.Storage/storageAccounts",
		"location":   location,
		"tags":       tags,
		"kind":       kind,
		"sku":        sku,
		"etag":       "W/\"miniblue\"",
		"properties": props,
	}

	// Optional top-level fields are only echoed when the caller actually set
	// them on PUT (consistent with the pattern used by VNet/NSG handlers).
	for _, k := range []string{"identity", "extendedLocation", "placement"} {
		if v, ok := input[k]; ok {
			resp[k] = v
		}
	}

	return resp
}

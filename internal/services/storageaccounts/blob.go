package storageaccounts

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
)

func (h *Handler) GetBlobServiceProperties(w http.ResponseWriter, r *http.Request) {
	sub := chi.URLParam(r, "subscriptionId")
	rg := chi.URLParam(r, "resourceGroupName")
	account := chi.URLParam(r, "accountName")

	if _, ok := h.store.Get(h.storageAccountKey(sub, rg, account)); !ok {
		writeServiceNotFound(w, "Microsoft.Storage/storageAccounts/blobServices", account)
		return
	}

	if v, ok := h.store.Get(h.servicePropsKey(sub, rg, account, "blobServices")); ok {
		json.NewEncoder(w).Encode(v)
		return
	}
	json.NewEncoder(w).Encode(h.buildServicePropertiesResponse(sub, rg, account, "blobServices", nil))
}

func (h *Handler) SetBlobServiceProperties(w http.ResponseWriter, r *http.Request) {
	sub := chi.URLParam(r, "subscriptionId")
	rg := chi.URLParam(r, "resourceGroupName")
	account := chi.URLParam(r, "accountName")

	if _, ok := h.store.Get(h.storageAccountKey(sub, rg, account)); !ok {
		writeServiceNotFound(w, "Microsoft.Storage/storageAccounts/blobServices", account)
		return
	}

	var input map[string]interface{}
	json.NewDecoder(r.Body).Decode(&input)

	resp := h.buildServicePropertiesResponse(sub, rg, account, "blobServices", input)
	h.store.Set(h.servicePropsKey(sub, rg, account, "blobServices"), resp)

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(resp)
}

func (h *Handler) PatchBlobServiceProperties(w http.ResponseWriter, r *http.Request) {
	sub := chi.URLParam(r, "subscriptionId")
	rg := chi.URLParam(r, "resourceGroupName")
	account := chi.URLParam(r, "accountName")

	if _, ok := h.store.Get(h.storageAccountKey(sub, rg, account)); !ok {
		writeServiceNotFound(w, "Microsoft.Storage/storageAccounts/blobServices", account)
		return
	}

	var input map[string]interface{}
	json.NewDecoder(r.Body).Decode(&input)

	resp := h.buildServicePropertiesResponse(sub, rg, account, "blobServices", input)
	h.store.Set(h.servicePropsKey(sub, rg, account, "blobServices"), resp)

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(resp)
}

func (h *Handler) armContainerKey(sub, rg, account, name string) string {
	return "blob:armcontainer:" + sub + ":" + rg + ":" + account + ":" + name
}

// buildARMContainerResponse builds the schema-faithful response body for a
// blob container under the ARM Storage RP.
//
// Fixes vs. the prior implementation:
//  - lastModifiedTime is now an RFC3339 UTC timestamp (was an etag-shaped hex
//    string, which is what the etag header should look like).
//  - etag is moved to the top-level resource (per the 2025-01-01 schema) and
//    emitted in the canonical hex format.
//  - Caller-supplied properties (publicAccess, metadata, encryption scope
//    settings, NFSv3 squash flags, etc.) are echoed so GET reflects PUT.
func (h *Handler) buildARMContainerResponse(sub, rg, account, name string, input map[string]interface{}) map[string]interface{} {
	now := time.Now().UTC()
	lastModified := now.Format(time.RFC3339)
	etag := fmt.Sprintf("\"0x%X\"", now.UnixNano())

	props := map[string]interface{}{
		"publicAccess":                "None",
		"leaseStatus":                 "Unlocked",
		"leaseState":                  "Available",
		"lastModifiedTime":            lastModified,
		"hasImmutabilityPolicy":       false,
		"hasLegalHold":                false,
		"deleted":                     false,
		"denyEncryptionScopeOverride": false,
		"metadata":                    map[string]interface{}{},
		"immutableStorageWithVersioning": map[string]interface{}{
			"enabled": false,
		},
	}

	if inProps, ok := input["properties"].(map[string]interface{}); ok {
		for _, k := range []string{
			"publicAccess",
			"metadata",
			"defaultEncryptionScope",
			"denyEncryptionScopeOverride",
			"enableNfsV3RootSquash",
			"enableNfsV3AllSquash",
			"immutableStorageWithVersioning",
		} {
			if v, ok := inProps[k]; ok {
				props[k] = v
			}
		}
	}

	return map[string]interface{}{
		"id":         "/subscriptions/" + sub + "/resourceGroups/" + rg + "/providers/Microsoft.Storage/storageAccounts/" + account + "/blobServices/default/containers/" + name,
		"name":       name,
		"type":       "Microsoft.Storage/storageAccounts/blobServices/containers",
		"etag":       etag,
		"properties": props,
	}
}

func (h *Handler) CreateContainerARM(w http.ResponseWriter, r *http.Request) {
	sub := chi.URLParam(r, "subscriptionId")
	rg := chi.URLParam(r, "resourceGroupName")
	account := chi.URLParam(r, "accountName")
	name := chi.URLParam(r, "containerName")

	var input map[string]interface{}
	json.NewDecoder(r.Body).Decode(&input)

	k := h.armContainerKey(sub, rg, account, name)
	c := h.buildARMContainerResponse(sub, rg, account, name, input)
	h.store.Set(k, c)

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(c)
}

func (h *Handler) GetContainerARM(w http.ResponseWriter, r *http.Request) {
	sub := chi.URLParam(r, "subscriptionId")
	rg := chi.URLParam(r, "resourceGroupName")
	account := chi.URLParam(r, "accountName")
	name := chi.URLParam(r, "containerName")

	v, ok := h.store.Get(h.armContainerKey(sub, rg, account, name))
	if !ok {
		writeServiceNotFound(w, "Microsoft.Storage/storageAccounts/blobServices/containers", name)
		return
	}
	json.NewEncoder(w).Encode(v)
}

func (h *Handler) DeleteContainerARM(w http.ResponseWriter, r *http.Request) {
	sub := chi.URLParam(r, "subscriptionId")
	rg := chi.URLParam(r, "resourceGroupName")
	account := chi.URLParam(r, "accountName")
	name := chi.URLParam(r, "containerName")

	if !h.store.Delete(h.armContainerKey(sub, rg, account, name)) {
		writeServiceNotFound(w, "Microsoft.Storage/storageAccounts/blobServices/containers", name)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

func (h *Handler) ListContainersARM(w http.ResponseWriter, r *http.Request) {
	sub := chi.URLParam(r, "subscriptionId")
	rg := chi.URLParam(r, "resourceGroupName")
	account := chi.URLParam(r, "accountName")
	items := h.store.ListByPrefix("blob:armcontainer:" + sub + ":" + rg + ":" + account + ":")
	json.NewEncoder(w).Encode(map[string]interface{}{"value": items})
}

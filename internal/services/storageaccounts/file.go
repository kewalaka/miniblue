package storageaccounts

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
)

func (h *Handler) GetFileServiceProperties(w http.ResponseWriter, r *http.Request) {
	sub := chi.URLParam(r, "subscriptionId")
	rg := chi.URLParam(r, "resourceGroupName")
	account := chi.URLParam(r, "accountName")

	if _, ok := h.store.Get(h.storageAccountKey(sub, rg, account)); !ok {
		writeServiceNotFound(w, "Microsoft.Storage/storageAccounts/fileServices", account)
		return
	}

	if v, ok := h.store.Get(h.servicePropsKey(sub, rg, account, "fileServices")); ok {
		json.NewEncoder(w).Encode(v)
		return
	}
	json.NewEncoder(w).Encode(h.buildServicePropertiesResponse(sub, rg, account, "fileServices", nil))
}

func (h *Handler) SetFileServiceProperties(w http.ResponseWriter, r *http.Request) {
	sub := chi.URLParam(r, "subscriptionId")
	rg := chi.URLParam(r, "resourceGroupName")
	account := chi.URLParam(r, "accountName")

	if _, ok := h.store.Get(h.storageAccountKey(sub, rg, account)); !ok {
		writeServiceNotFound(w, "Microsoft.Storage/storageAccounts/fileServices", account)
		return
	}

	var input map[string]interface{}
	json.NewDecoder(r.Body).Decode(&input)

	resp := h.buildServicePropertiesResponse(sub, rg, account, "fileServices", input)
	h.store.Set(h.servicePropsKey(sub, rg, account, "fileServices"), resp)

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(resp)
}

func (h *Handler) PatchFileServiceProperties(w http.ResponseWriter, r *http.Request) {
	sub := chi.URLParam(r, "subscriptionId")
	rg := chi.URLParam(r, "resourceGroupName")
	account := chi.URLParam(r, "accountName")

	if _, ok := h.store.Get(h.storageAccountKey(sub, rg, account)); !ok {
		writeServiceNotFound(w, "Microsoft.Storage/storageAccounts/fileServices", account)
		return
	}

	var input map[string]interface{}
	json.NewDecoder(r.Body).Decode(&input)

	resp := h.buildServicePropertiesResponse(sub, rg, account, "fileServices", input)
	h.store.Set(h.servicePropsKey(sub, rg, account, "fileServices"), resp)

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(resp)
}

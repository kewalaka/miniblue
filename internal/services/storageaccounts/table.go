package storageaccounts

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
)

func (h *Handler) GetTableServiceProperties(w http.ResponseWriter, r *http.Request) {
	sub := chi.URLParam(r, "subscriptionId")
	rg := chi.URLParam(r, "resourceGroupName")
	account := chi.URLParam(r, "accountName")

	if _, ok := h.store.Get(h.storageAccountKey(sub, rg, account)); !ok {
		writeServiceNotFound(w, "Microsoft.Storage/storageAccounts/tableServices", account)
		return
	}

	if v, ok := h.store.Get(h.servicePropsKey(sub, rg, account, "tableServices")); ok {
		json.NewEncoder(w).Encode(v)
		return
	}
	json.NewEncoder(w).Encode(h.buildServicePropertiesResponse(sub, rg, account, "tableServices", nil))
}

func (h *Handler) SetTableServiceProperties(w http.ResponseWriter, r *http.Request) {
	sub := chi.URLParam(r, "subscriptionId")
	rg := chi.URLParam(r, "resourceGroupName")
	account := chi.URLParam(r, "accountName")

	if _, ok := h.store.Get(h.storageAccountKey(sub, rg, account)); !ok {
		writeServiceNotFound(w, "Microsoft.Storage/storageAccounts/tableServices", account)
		return
	}

	var input map[string]interface{}
	json.NewDecoder(r.Body).Decode(&input)

	resp := h.buildServicePropertiesResponse(sub, rg, account, "tableServices", input)
	h.store.Set(h.servicePropsKey(sub, rg, account, "tableServices"), resp)

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(resp)
}

func (h *Handler) PatchTableServiceProperties(w http.ResponseWriter, r *http.Request) {
	sub := chi.URLParam(r, "subscriptionId")
	rg := chi.URLParam(r, "resourceGroupName")
	account := chi.URLParam(r, "accountName")

	if _, ok := h.store.Get(h.storageAccountKey(sub, rg, account)); !ok {
		writeServiceNotFound(w, "Microsoft.Storage/storageAccounts/tableServices", account)
		return
	}

	var input map[string]interface{}
	json.NewDecoder(r.Body).Decode(&input)

	resp := h.buildServicePropertiesResponse(sub, rg, account, "tableServices", input)
	h.store.Set(h.servicePropsKey(sub, rg, account, "tableServices"), resp)

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(resp)
}

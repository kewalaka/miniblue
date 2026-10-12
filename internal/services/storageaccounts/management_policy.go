package storageaccounts

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
)

func (h *Handler) managementPolicyKey(sub, rg, account string) string {
	return "blob:managementpolicy:" + sub + ":" + rg + ":" + account + ":default"
}

func (h *Handler) buildManagementPolicyResponse(sub, rg, account string, input map[string]interface{}) map[string]interface{} {
	resp := map[string]interface{}{
		"id":   "/subscriptions/" + sub + "/resourceGroups/" + rg + "/providers/Microsoft.Storage/storageAccounts/" + account + "/managementPolicies/default",
		"name": "default",
		"type": "Microsoft.Storage/storageAccounts/managementPolicies",
	}

	for k, v := range input {
		resp[k] = v
	}

	return resp
}

func (h *Handler) SetManagementPolicy(w http.ResponseWriter, r *http.Request) {
	sub := chi.URLParam(r, "subscriptionId")
	rg := chi.URLParam(r, "resourceGroupName")
	account := chi.URLParam(r, "accountName")

	if _, ok := h.store.Get(h.storageAccountKey(sub, rg, account)); !ok {
		writeServiceNotFound(w, "Microsoft.Storage/storageAccounts/managementPolicies", "default")
		return
	}

	var input map[string]interface{}
	json.NewDecoder(r.Body).Decode(&input)

	resp := h.buildManagementPolicyResponse(sub, rg, account, input)
	h.store.Set(h.managementPolicyKey(sub, rg, account), resp)

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(resp)
}

func (h *Handler) GetManagementPolicy(w http.ResponseWriter, r *http.Request) {
	sub := chi.URLParam(r, "subscriptionId")
	rg := chi.URLParam(r, "resourceGroupName")
	account := chi.URLParam(r, "accountName")

	v, ok := h.store.Get(h.managementPolicyKey(sub, rg, account))
	if !ok {
		writeServiceNotFound(w, "Microsoft.Storage/storageAccounts/managementPolicies", "default")
		return
	}
	json.NewEncoder(w).Encode(v)
}

func (h *Handler) DeleteManagementPolicy(w http.ResponseWriter, r *http.Request) {
	sub := chi.URLParam(r, "subscriptionId")
	rg := chi.URLParam(r, "resourceGroupName")
	account := chi.URLParam(r, "accountName")

	if !h.store.Delete(h.managementPolicyKey(sub, rg, account)) {
		writeServiceNotFound(w, "Microsoft.Storage/storageAccounts/managementPolicies", "default")
		return
	}
	w.WriteHeader(http.StatusOK)
}

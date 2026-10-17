package storageaccounts

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/moabukar/miniblue/internal/azerr"
)

// GetAccountMigration implements GET .../storageAccounts/{accountName}/accountMigrations/default.
// azurerm reads it on every account read; miniblue never migrates, so it always reports Complete.
func (h *Handler) GetAccountMigration(w http.ResponseWriter, r *http.Request) {
	sub := chi.URLParam(r, "subscriptionId")
	rg := chi.URLParam(r, "resourceGroupName")
	name := chi.URLParam(r, "accountName")

	v, ok := h.store.Get(h.storageAccountKey(sub, rg, name))
	if !ok {
		azerr.NotFound(w, "Microsoft.Storage/storageAccounts", name)
		return
	}

	skuName := "Standard_LRS"
	if acct, ok := v.(map[string]interface{}); ok {
		if sku, ok := acct["sku"].(map[string]interface{}); ok {
			if n, ok := sku["name"].(string); ok {
				skuName = n
			}
		}
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"id":   "/subscriptions/" + sub + "/resourceGroups/" + rg + "/providers/Microsoft.Storage/storageAccounts/" + name + "/accountMigrations/default",
		"name": "default",
		"type": "Microsoft.Storage/storageAccounts/accountMigrations",
		"properties": map[string]interface{}{
			"targetSkuName":   skuName,
			"migrationStatus": "Complete",
		},
	})
}

package keyvault

import (
	"strings"

	"github.com/moabukar/miniblue/internal/store"
)

// CleanupRG removes the vaults of a resource group and their data-plane
// secrets. Soft-deleted vaults are subscription/location scoped and stay.
func CleanupRG(s *store.Store, sub, rg string) {
	h := NewHandler(s)
	prefix := h.vaultResourceGroupPrefix(sub, rg)
	for _, item := range s.ListByPrefix(prefix) {
		name := stringValue(mapValue(item)["name"])
		if name == "" {
			continue
		}
		// Secrets are keyed by the vault name as the client sent it: verbatim
		// on the path form, lower-cased on the Host form.
		for _, n := range []string{name, strings.ToLower(name)} {
			s.DeleteByPrefix("kv:" + n + ":")
			s.DeleteByPrefix("kvver:" + n + ":")
			s.DeleteByPrefix("kvdel:" + n + ":")
		}
	}
	s.DeleteByPrefix(prefix)
}

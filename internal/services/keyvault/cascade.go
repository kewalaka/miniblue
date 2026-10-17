package keyvault

import (
	"strings"

	"github.com/moabukar/miniblue/internal/store"
)

// CleanupRG removes the vaults of a resource group and their data-plane
// secrets. Soft-deleted vaults are subscription/location scoped and stay.
func CleanupRG(s *store.Store, sub, rg string) {
	prefix := NewHandler(s).vaultResourceGroupPrefix(sub, rg)
	vaults := map[string]bool{}
	for _, k := range s.ListKeysByPrefix(prefix) {
		if name, ok := strings.CutPrefix(k, prefix); ok {
			vaults[name] = true
		}
	}
	if len(vaults) > 0 {
		// Secrets are keyed by the vault name as the client sent it (verbatim
		// on the path form, lower-cased on the Host form), so match the vault
		// component case-insensitively. Vault records are keyed lower-case.
		for _, family := range []string{"kv:", "kvver:", "kvdel:"} {
			for _, k := range s.ListKeysByPrefix(family) {
				rest, ok := strings.CutPrefix(k, family)
				if !ok {
					continue
				}
				vault, _, _ := strings.Cut(rest, ":")
				if vaults[strings.ToLower(vault)] {
					s.Delete(k)
				}
			}
		}
	}
	s.DeleteByPrefix(prefix)
}

package identity

import "github.com/moabukar/miniblue/internal/store"

// CleanupRG removes the user-assigned identities of a resource group.
func CleanupRG(s *store.Store, sub, rg string) {
	s.DeleteByPrefix("msi:identity:" + sub + ":" + rg + ":")
}

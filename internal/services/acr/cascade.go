package acr

import "github.com/moabukar/miniblue/internal/store"

// CleanupRG removes the container registries of a resource group.
func CleanupRG(s *store.Store, sub, rg string) {
	s.DeleteByPrefix("acr:registry:" + sub + ":" + rg + ":")
}

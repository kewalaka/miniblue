package aci

import "github.com/moabukar/miniblue/internal/store"

// CleanupRG removes the container groups of a resource group.
func CleanupRG(s *store.Store, sub, rg string) {
	s.DeleteByPrefix("aci:containergroup:" + sub + ":" + rg + ":")
}

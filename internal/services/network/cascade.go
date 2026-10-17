package network

import "github.com/moabukar/miniblue/internal/store"

// CleanupRG removes the virtual networks and subnets of a resource group.
func CleanupRG(s *store.Store, sub, rg string) {
	p := sub + ":" + rg + ":"
	s.DeleteByPrefix("vnet:" + p)
	s.DeleteByPrefix("subnet:" + p)
}

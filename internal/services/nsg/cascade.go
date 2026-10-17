package nsg

import "github.com/moabukar/miniblue/internal/store"

// CleanupRG removes the network security groups and rules of a resource group.
func CleanupRG(s *store.Store, sub, rg string) {
	p := sub + ":" + rg + ":"
	s.DeleteByPrefix("nsg:" + p)
	s.DeleteByPrefix("nsgrule:" + p)
}

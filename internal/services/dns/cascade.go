package dns

import "github.com/moabukar/miniblue/internal/store"

// CleanupRG removes the DNS zones and record sets of a resource group.
func CleanupRG(s *store.Store, sub, rg string) {
	p := sub + ":" + rg + ":"
	s.DeleteByPrefix("dns:zone:" + p)
	s.DeleteByPrefix("dns:record:" + p)
}

package loadbalancer

import "github.com/moabukar/miniblue/internal/store"

// CleanupRG removes the load balancers of a resource group.
func CleanupRG(s *store.Store, sub, rg string) {
	s.DeleteByPrefix("lb:" + sub + ":" + rg + ":")
}

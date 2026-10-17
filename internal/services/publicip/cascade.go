package publicip

import "github.com/moabukar/miniblue/internal/store"

// CleanupRG removes the public IP addresses of a resource group.
func CleanupRG(s *store.Store, sub, rg string) {
	s.DeleteByPrefix("publicip:" + sub + ":" + rg + ":")
}

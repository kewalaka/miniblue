package sites

import "github.com/moabukar/miniblue/internal/store"

// CleanupRG removes the web and function apps of a resource group.
func CleanupRG(s *store.Store, sub, rg string) {
	s.DeleteByPrefix("func:" + sub + ":" + rg + ":")
}

package appgw

import "github.com/moabukar/miniblue/internal/store"

// CleanupRG removes the application gateways of a resource group.
func CleanupRG(s *store.Store, sub, rg string) {
	s.DeleteByPrefix("appgw:" + sub + ":" + rg + ":")
}

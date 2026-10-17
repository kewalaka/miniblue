package appconfig

import "github.com/moabukar/miniblue/internal/store"

// CleanupRG removes the App Configuration stores of a resource group.
func CleanupRG(s *store.Store, sub, rg string) {
	s.DeleteByPrefix("appconfig:store:" + sub + ":" + rg + ":")
}

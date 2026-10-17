package disks

import "github.com/moabukar/miniblue/internal/store"

// CleanupRG removes the managed disks of a resource group.
func CleanupRG(s *store.Store, sub, rg string) {
	s.DeleteByPrefix("disk:" + sub + ":" + rg + ":")
}

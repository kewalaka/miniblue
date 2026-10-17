package networkinterfaces

import "github.com/moabukar/miniblue/internal/store"

// CleanupRG removes the network interfaces of a resource group.
func CleanupRG(s *store.Store, sub, rg string) {
	s.DeleteByPrefix("nic:" + sub + ":" + rg + ":")
}

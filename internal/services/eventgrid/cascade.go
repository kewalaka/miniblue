package eventgrid

import "github.com/moabukar/miniblue/internal/store"

// CleanupRG removes the topics of a resource group.
func CleanupRG(s *store.Store, sub, rg string) {
	s.DeleteByPrefix("eg:topic:" + sub + ":" + rg + ":")
}

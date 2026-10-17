package sqldb

import "github.com/moabukar/miniblue/internal/store"

// CleanupRG removes the SQL servers and databases of a resource group.
func CleanupRG(s *store.Store, sub, rg string) {
	p := sub + ":" + rg + ":"
	s.DeleteByPrefix("sqldb:server:" + p)
	s.DeleteByPrefix("sqldb:db:" + p)
}

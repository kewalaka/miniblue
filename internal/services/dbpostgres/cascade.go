package dbpostgres

import "github.com/moabukar/miniblue/internal/store"

// CleanupRG removes the PostgreSQL flexible servers and databases of a
// resource group.
func CleanupRG(s *store.Store, sub, rg string) {
	p := sub + ":" + rg + ":"
	s.DeleteByPrefix("pgflex:server:" + p)
	s.DeleteByPrefix("pgflex:db:" + p)
}

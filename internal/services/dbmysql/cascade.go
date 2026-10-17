package dbmysql

import "github.com/moabukar/miniblue/internal/store"

// CleanupRG removes the MySQL servers and databases of a resource group.
func CleanupRG(s *store.Store, sub, rg string) {
	p := sub + ":" + rg + ":"
	s.DeleteByPrefix("dbmysql:server:" + p)
	s.DeleteByPrefix("dbmysql:db:" + p)
}

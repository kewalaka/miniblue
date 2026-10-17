package cosmosdb

import "github.com/moabukar/miniblue/internal/store"

// CleanupRG removes the Cosmos DB accounts, SQL databases and containers of a
// resource group.
func CleanupRG(s *store.Store, sub, rg string) {
	p := sub + ":" + rg + ":"
	s.DeleteByPrefix("cosmos:account:" + p)
	s.DeleteByPrefix("cosmos:sqldb:" + p)
	s.DeleteByPrefix("cosmos:container:" + p)
}

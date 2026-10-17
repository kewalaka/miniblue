package redis

import "github.com/moabukar/miniblue/internal/store"

// CleanupRG removes the Redis caches of a resource group.
func CleanupRG(s *store.Store, sub, rg string) {
	s.DeleteByPrefix("redis:" + sub + ":" + rg + ":")
}

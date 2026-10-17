package servicebus

import "github.com/moabukar/miniblue/internal/store"

// CleanupRG removes the namespaces, queues and topics of a resource group.
func CleanupRG(s *store.Store, sub, rg string) {
	p := sub + ":" + rg + ":"
	s.DeleteByPrefix("sb:namespace:" + p)
	s.DeleteByPrefix("sb:armqueue:" + p)
	s.DeleteByPrefix("sb:armtopic:" + p)
}

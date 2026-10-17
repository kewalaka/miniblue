package storageaccounts

import "github.com/moabukar/miniblue/internal/store"

// CleanupRG removes the storage accounts of a resource group and their ARM
// children (blob containers, management policies, service properties).
func CleanupRG(s *store.Store, sub, rg string) {
	p := sub + ":" + rg + ":"
	s.DeleteByPrefix("blob:account:" + p)
	s.DeleteByPrefix("blob:armcontainer:" + p)
	s.DeleteByPrefix("blob:managementpolicy:" + p)
	s.DeleteByPrefix("blob:svcprops:" + p)
}

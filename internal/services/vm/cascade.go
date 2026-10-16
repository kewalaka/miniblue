package vm

import (
	"os"

	"github.com/moabukar/miniblue/internal/store"
)

// CleanupVMsInRG detaches the NICs and disks of every VM of the given
// subscription/resource group (they may live in other resource groups),
// removes the Docker containers, networks and identity-secret index entries,
// and drops auto-created OS disks, exactly as a direct VM delete does.
// Intended to be invoked by the resourcegroups handler during cascade
// delete, BEFORE it removes the vm:/vmsvc: keys from the store. Safe to call
// in stub mode and when the RG has no VMs; Docker errors are logged inside
// the helpers, never returned — the ARM-level delete must always succeed.
func CleanupVMsInRG(s *store.Store, sub, rg string) {
	dockerOK := checkDockerCached()
	for _, item := range s.ListByPrefix("vm:" + sub + ":" + rg + ":") {
		rec, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		removeDependencies(s, sub, rg, rec)
		name, _ := rec["name"].(string)
		if secret, _ := rec["_identitySecret"].(string); secret != "" {
			s.Delete(secretKey(secret))
		}
		if !dockerOK || name == "" {
			continue
		}
		for _, svcItem := range s.ListByPrefix("vmsvc:" + sub + ":" + rg + ":" + name + ":") {
			svc, _ := svcItem.(map[string]interface{})
			if cn, _ := svc["_containerName"].(string); cn != "" {
				dockerRemove(cn)
			}
		}
		dockerRemove(vmContainerName(rg, name))
		dockerNetworkRemove(vmNetworkName(rg, name))
	}
}

// checkDockerCached memoizes the Docker probe for cascade calls, which run in
// the resourcegroups handler without access to the VM handler instance.
var dockerProbe *bool

func checkDockerCached() bool {
	if os.Getenv("MINIBLUE_DISABLE_DOCKER") != "" {
		return false
	}
	if dockerProbe == nil {
		v := checkDocker()
		dockerProbe = &v
	}
	return *dockerProbe
}

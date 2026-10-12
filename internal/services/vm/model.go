package vm

import (
	"fmt"
	"os"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// Store keys, container naming, response shaping
// ---------------------------------------------------------------------------

func vmKey(sub, rg, name string) string {
	return "vm:" + sub + ":" + rg + ":" + name
}

func svcKey(sub, rg, vmName, svc string) string {
	return "vmsvc:" + sub + ":" + rg + ":" + vmName + ":" + svc
}

func secretKey(secret string) string {
	return "vmidsecret:" + secret
}

func vmContainerName(rg, name string) string {
	return "miniblue-vm-" + rg + "-" + name
}

func vmNetworkName(rg, name string) string {
	return "miniblue-vmnet-" + rg + "-" + name
}

func svcContainerName(rg, vmName, svc string) string {
	return "miniblue-vmsvc-" + rg + "-" + vmName + "-" + svc
}

func vmARMID(sub, rg, name string) string {
	return "/subscriptions/" + sub + "/resourceGroups/" + rg + "/providers/Microsoft.Compute/virtualMachines/" + name
}

// identityEnv builds the managed-identity environment variables injected into
// the VM container and every service container. The endpoint points back at
// the miniblue host port through the host gateway alias; MSI_* are the legacy
// names older Azure SDKs probe for.
func identityEnv(secret string) []string {
	port := os.Getenv("PORT")
	if port == "" {
		port = "4566"
	}
	endpoint := "http://host.docker.internal:" + port + "/metadata/identity/oauth2/token"
	return []string{
		"IDENTITY_ENDPOINT=" + endpoint,
		"IDENTITY_HEADER=" + secret,
		"MSI_ENDPOINT=" + endpoint,
		"MSI_SECRET=" + secret,
	}
}

// appendProvisionEvent records a timestamped lifecycle event on the VM record
// (the `_provisionLog` internal field backing the VM-level log view).
func appendProvisionEvent(rec map[string]interface{}, format string, args ...interface{}) {
	events, _ := rec["_provisionLog"].([]interface{})
	line := time.Now().UTC().Format(time.RFC3339) + " " + fmt.Sprintf(format, args...)
	rec["_provisionLog"] = append(events, line)
}

func provisionEvents(rec map[string]interface{}) []string {
	events, _ := rec["_provisionLog"].([]interface{})
	out := make([]string, 0, len(events))
	for _, e := range events {
		if s, ok := e.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// publicView returns a deep copy of a stored record suitable for API
// responses: internal fields (prefixed by "_") and write-only secrets are
// stripped.
func publicView(rec map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(rec))
	for k, v := range rec {
		if strings.HasPrefix(k, "_") {
			continue
		}
		out[k] = recursiveCopy(v)
	}
	delete(asMap(asMap(out["properties"])["osProfile"]), "adminPassword")
	return out
}

// vmView is publicView plus the instance view when the caller asked for it
// with $expand=instanceView.
func vmView(rec map[string]interface{}, expandInstanceView bool) map[string]interface{} {
	out := publicView(rec)
	if expandInstanceView {
		if props := asMap(out["properties"]); props != nil {
			props["instanceView"] = instanceViewFor(powerState(rec))
		}
	}
	return out
}

func getProps(rec map[string]interface{}) map[string]interface{} {
	props, _ := rec["properties"].(map[string]interface{})
	if props == nil {
		props = map[string]interface{}{}
		rec["properties"] = props
	}
	return props
}

func getMinibluProps(rec map[string]interface{}) map[string]interface{} {
	props := getProps(rec)
	mb, _ := props["miniblue"].(map[string]interface{})
	if mb == nil {
		mb = map[string]interface{}{}
		props["miniblue"] = mb
	}
	return mb
}

func powerState(rec map[string]interface{}) string {
	mb := getMinibluProps(rec)
	s, _ := mb["powerState"].(string)
	return s
}

// buildVMRecord assembles the stored VM record from a PUT/PATCH body. The
// request is replayed as sent (see vmProperties); on update, omitted fields
// keep their stored values and the miniblue runtime block, identity and
// internal "_" fields carry over from existing. image is recorded only for
// new VMs (an update never re-images the container) and may be empty when no
// container image could be resolved (stub mode).
func buildVMRecord(sub, rg, name string, input, existing map[string]interface{}, image string) map[string]interface{} {
	existingProps := asMap(existing["properties"])
	props := vmProperties(sub, rg, name, asMap(input["properties"]), existingProps)

	mb := asMap(recursiveCopy(existingProps["miniblue"]))
	if mb == nil {
		mb = map[string]interface{}{
			"powerState":    stateRunning,
			"containerName": vmContainerName(rg, name),
		}
		if image != "" {
			mb["image"] = image
		}
	}
	props["miniblue"] = mb

	rec := map[string]interface{}{
		"id":         vmARMID(sub, rg, name),
		"name":       name,
		"type":       "Microsoft.Compute/virtualMachines",
		"location":   firstString(input["location"], existing["location"], "eastus"),
		"tags":       map[string]interface{}{},
		"properties": props,
	}
	if tags := asMap(input["tags"]); tags != nil {
		rec["tags"] = tags
	} else if tags := asMap(existing["tags"]); tags != nil {
		rec["tags"] = tags
	}
	for _, k := range []string{"plan", "sku", "zones", "extendedLocation"} {
		if v, ok := input[k]; ok {
			rec[k] = v
		} else if v, ok := existing[k]; ok {
			rec[k] = v
		}
	}
	for k, v := range existing {
		if strings.HasPrefix(k, "_") || k == "identity" {
			rec[k] = v
		}
	}
	return rec
}

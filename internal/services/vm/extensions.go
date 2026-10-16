package vm

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/moabukar/miniblue/internal/azerr"
)

// ---------------------------------------------------------------------------
// VM extensions (Microsoft.Compute/virtualMachines/extensions)
//
// ARM-only: extensions are recorded and replayed, nothing is executed inside
// the VM container. protectedSettings is write-only, as in Azure.
// ---------------------------------------------------------------------------

const extensionType = "Microsoft.Compute/virtualMachines/extensions"

func extensionKey(sub, rg, vmName, name string) string {
	return "vmext:" + sub + ":" + rg + ":" + vmName + ":" + name
}

func extensionPublicView(ext map[string]interface{}) map[string]interface{} {
	out := asMap(recursiveCopy(ext))
	delete(asMap(out["properties"]), "protectedSettings")
	return out
}

func buildExtension(sub, rg, vmName, name string, input, existing, vmRec map[string]interface{}) map[string]interface{} {
	props := asMap(recursiveCopy(input["properties"]))
	if props == nil {
		props = asMap(recursiveCopy(existing["properties"]))
	}
	if props == nil {
		props = map[string]interface{}{}
	}
	props["provisioningState"] = "Succeeded"

	ext := map[string]interface{}{
		"id":         vmARMID(sub, rg, vmName) + "/extensions/" + name,
		"name":       name,
		"type":       extensionType,
		"location":   firstString(input["location"], existing["location"], vmRec["location"], "eastus"),
		"properties": props,
	}
	if tags := asMap(input["tags"]); tags != nil {
		ext["tags"] = tags
	} else if tags := asMap(existing["tags"]); tags != nil {
		ext["tags"] = tags
	}
	return ext
}

func (h *Handler) writeExtension(w http.ResponseWriter, r *http.Request, patch bool) {
	sub := chi.URLParam(r, "subscriptionId")
	rg := chi.URLParam(r, "resourceGroupName")
	vmName := chi.URLParam(r, "vmName")
	name := chi.URLParam(r, "extensionName")

	vmRec, ok := h.getVM(sub, rg, vmName)
	if !ok {
		azerr.NotFound(w, "Microsoft.Compute/virtualMachines", vmName)
		return
	}
	input, err := decodeBody(r)
	if err != nil {
		azerr.BadRequest(w, "The request content was invalid: "+err.Error())
		return
	}

	key := extensionKey(sub, rg, vmName, name)
	var existing map[string]interface{}
	if v, ok := h.store.Get(key); ok {
		existing = asMap(v)
	}
	if patch {
		if existing == nil {
			azerr.NotFound(w, extensionType, name)
			return
		}
		input = mergePatch(existing, input)
	}

	ext := buildExtension(sub, rg, vmName, name, input, existing, vmRec)
	h.store.Set(key, ext)
	if existing == nil {
		w.WriteHeader(http.StatusCreated)
	}
	json.NewEncoder(w).Encode(extensionPublicView(ext))
}

func (h *Handler) CreateOrUpdateExtension(w http.ResponseWriter, r *http.Request) {
	h.writeExtension(w, r, false)
}

func (h *Handler) UpdateExtension(w http.ResponseWriter, r *http.Request) {
	h.writeExtension(w, r, true)
}

func (h *Handler) GetExtension(w http.ResponseWriter, r *http.Request) {
	sub := chi.URLParam(r, "subscriptionId")
	rg := chi.URLParam(r, "resourceGroupName")
	vmName := chi.URLParam(r, "vmName")
	name := chi.URLParam(r, "extensionName")

	v, ok := h.store.Get(extensionKey(sub, rg, vmName, name))
	if !ok {
		azerr.NotFound(w, extensionType, name)
		return
	}
	json.NewEncoder(w).Encode(extensionPublicView(asMap(v)))
}

func (h *Handler) DeleteExtension(w http.ResponseWriter, r *http.Request) {
	sub := chi.URLParam(r, "subscriptionId")
	rg := chi.URLParam(r, "resourceGroupName")
	vmName := chi.URLParam(r, "vmName")
	name := chi.URLParam(r, "extensionName")

	if !h.store.Delete(extensionKey(sub, rg, vmName, name)) {
		azerr.NotFound(w, extensionType, name)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

func (h *Handler) ListExtensions(w http.ResponseWriter, r *http.Request) {
	sub := chi.URLParam(r, "subscriptionId")
	rg := chi.URLParam(r, "resourceGroupName")
	vmName := chi.URLParam(r, "vmName")

	if !h.store.Exists(vmKey(sub, rg, vmName)) {
		azerr.NotFound(w, "Microsoft.Compute/virtualMachines", vmName)
		return
	}
	items := h.store.ListByPrefix(extensionKey(sub, rg, vmName, ""))
	out := make([]interface{}, 0, len(items))
	for _, item := range items {
		if ext := asMap(item); ext != nil {
			out = append(out, extensionPublicView(ext))
		}
	}
	json.NewEncoder(w).Encode(map[string]interface{}{"value": out})
}

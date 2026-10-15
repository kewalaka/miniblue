package vm

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/moabukar/miniblue/internal/azerr"
	"github.com/moabukar/miniblue/internal/store"
)

// Handler serves Azure Virtual Machines (Microsoft.Compute/virtualMachines)
// endpoints. Each VM is backed by a Docker container; without Docker the
// handler runs in stub mode like ACI: records are fully manageable through
// the API, runtime operations return 409 DockerUnavailable.
type Handler struct {
	store       *store.Store
	dockerAvail bool
}

// NewHandler creates a new VM handler, probing Docker availability once at
// startup. MINIBLUE_DISABLE_DOCKER forces stub mode (used by tests to stay
// deterministic on machines that do have Docker).
func NewHandler(s *store.Store) *Handler {
	h := &Handler{store: s}
	if os.Getenv("MINIBLUE_DISABLE_DOCKER") == "" {
		h.dockerAvail = checkDocker()
	}
	if h.dockerAvail {
		log.Println("[vm] Docker is available – virtual machines will use real containers")
	} else {
		log.Println("[vm] Docker is NOT available – virtual machines will be stub-only")
	}
	return h
}

// Register mounts all VM ARM routes on the given router.
func (h *Handler) Register(r chi.Router) {
	r.Route("/subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/Microsoft.Compute/virtualMachines", func(r chi.Router) {
		r.Get("/", h.ListVMs)
		r.Route("/{vmName}", func(r chi.Router) {
			r.Put("/", h.CreateOrUpdateVM)
			r.Patch("/", h.UpdateVM)
			r.Get("/", h.GetVM)
			r.Delete("/", h.DeleteVM)
			r.Get("/instanceView", h.GetInstanceView)
			r.Post("/start", h.StartVM)
			r.Post("/powerOff", h.PowerOffVM)
			r.Post("/restart", h.RestartVM)
			r.Post("/deallocate", h.DeallocateVM)
			r.Post("/redeploy", h.RedeployVM)
			r.Post("/runCommand", h.RunCommand)
			r.Get("/logs", h.GetLogs)
			r.Route("/extensions", func(r chi.Router) {
				r.Get("/", h.ListExtensions)
				r.Route("/{extensionName}", func(r chi.Router) {
					r.Put("/", h.CreateOrUpdateExtension)
					r.Patch("/", h.UpdateExtension)
					r.Get("/", h.GetExtension)
					r.Delete("/", h.DeleteExtension)
				})
			})
			r.Route("/services", func(r chi.Router) {
				r.Get("/", h.ListServices)
				r.Route("/{serviceName}", func(r chi.Router) {
					r.Put("/", h.CreateOrUpdateService)
					r.Get("/", h.GetService)
					r.Delete("/", h.DeleteService)
				})
			})
		})
	})
	r.Get("/subscriptions/{subscriptionId}/providers/Microsoft.Compute/virtualMachines", h.ListAllVMs)
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func (h *Handler) getVM(sub, rg, name string) (map[string]interface{}, bool) {
	v, ok := h.store.Get(vmKey(sub, rg, name))
	if !ok {
		return nil, false
	}
	rec, ok := v.(map[string]interface{})
	return rec, ok
}

// refreshVM reconciles the stored power state with the real container state,
// so the API never reports "running" for a container that no longer runs
// (e.g. after an emulator or host restart). A stopped container keeps a
// stored "deallocated": Docker cannot tell the two apart.
func (h *Handler) refreshVM(rec map[string]interface{}) {
	if !h.dockerAvail {
		return
	}
	mb := getMinibluProps(rec)
	containerName, _ := mb["containerName"].(string)
	if containerName == "" {
		return
	}
	switch {
	case dockerInspectRunning(containerName):
		mb["powerState"] = stateRunning
	case powerState(rec) != stateDeallocated:
		mb["powerState"] = stateStopped
	}
}

func (h *Handler) serviceRecords(sub, rg, vmName string) []map[string]interface{} {
	items := h.store.ListByPrefix("vmsvc:" + sub + ":" + rg + ":" + vmName + ":")
	out := make([]map[string]interface{}, 0, len(items))
	for _, it := range items {
		if rec, ok := it.(map[string]interface{}); ok {
			out = append(out, rec)
		}
	}
	return out
}

func dockerUnavailable(w http.ResponseWriter) {
	azerr.WriteError(w, http.StatusConflict, "DockerUnavailable",
		"This operation requires the Docker backend, which is not available. Start the Docker daemon and restart miniblue to use real virtual machines.")
}

// decodeBody tolerates an empty request body (everything on a VM is
// defaultable except the name, which comes from the URL).
func decodeBody(r *http.Request) (map[string]interface{}, error) {
	var input map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		if errors.Is(err, io.EOF) {
			return map[string]interface{}{}, nil
		}
		return nil, err
	}
	if input == nil {
		input = map[string]interface{}{}
	}
	return input, nil
}

// ---------------------------------------------------------------------------
// VM CRUD
// ---------------------------------------------------------------------------

func (h *Handler) CreateOrUpdateVM(w http.ResponseWriter, r *http.Request) {
	h.writeVM(w, r, false)
}

func (h *Handler) UpdateVM(w http.ResponseWriter, r *http.Request) {
	h.writeVM(w, r, true)
}

// writeVM implements PUT (create or full update) and PATCH (merge into an
// existing VM). Updates are in place: the backing container is not re-imaged.
func (h *Handler) writeVM(w http.ResponseWriter, r *http.Request, patch bool) {
	sub := chi.URLParam(r, "subscriptionId")
	rg := chi.URLParam(r, "resourceGroupName")
	name := chi.URLParam(r, "vmName")

	input, err := decodeBody(r)
	if err != nil {
		azerr.BadRequest(w, "Could not parse request body: "+err.Error())
		return
	}

	if !h.store.Exists("rg:" + sub + ":" + rg) {
		azerr.WriteError(w, http.StatusNotFound, "ResourceGroupNotFound",
			"Resource group '"+rg+"' could not be found.")
		return
	}

	existing, exists := h.getVM(sub, rg, name)
	if patch && !exists {
		azerr.NotFound(w, "Microsoft.Compute/virtualMachines", name)
		return
	}

	identityBlock, err := resolveIdentityBlock(h.store, input)
	if err != nil {
		azerr.BadRequest(w, err.Error())
		return
	}
	_, hasIdentity := input["identity"]

	var image string
	if !exists {
		// An unmappable image only blocks VMs that need a container; in stub
		// mode the record is still accepted so ARM-only workflows (Windows
		// images, Terraform plans) keep working.
		if image, err = resolveImage(asMap(input["properties"])); err != nil && h.dockerAvail {
			azerr.BadRequest(w, err.Error())
			return
		}
	}
	suppliedManagedDisk := requestHasManagedDiskID(input)
	if patch {
		input = mergePatch(existing, input)
	}
	if exists {
		h.refreshVM(existing)
	}

	rec := buildVMRecord(sub, rg, name, input, existing, image)
	if disk := osDiskConflict(h.store, sub, rg, suppliedManagedDisk, rec); disk != "" {
		azerr.WriteError(w, http.StatusConflict, "OperationNotAllowed",
			"Disk "+disk+" already exists in resource group "+rg+". Only CreateOption.Attach is supported.")
		return
	}
	if hasIdentity {
		if identityBlock == nil {
			delete(rec, "identity")
		} else {
			rec["identity"] = identityBlock
		}
		appendProvisionEvent(rec, "identity assignments updated")
	}

	key := vmKey(sub, rg, name)
	status := http.StatusOK
	if exists {
		appendProvisionEvent(rec, "virtual machine updated")
	} else {
		status = http.StatusCreated
		secret := uuid.NewString()
		rec["_identitySecret"] = secret
		rec["_networkName"] = vmNetworkName(rg, name)
		appendProvisionEvent(rec, "virtual machine created%s", imageSuffix(image))

		if h.dockerAvail {
			if err := h.startContainer(rg, name, image, secret); err != nil {
				azerr.WriteError(w, http.StatusConflict, "ContainerStartFailed", err.Error())
				return
			}
			appendProvisionEvent(rec, "compute environment started (container %s)", vmContainerName(rg, name))
		}
		h.store.Set(secretKey(secret), map[string]interface{}{"vmKey": key})
	}

	h.store.Set(key, rec)
	syncDependencies(h.store, sub, rg, suppliedManagedDisk, rec)

	w.WriteHeader(status)
	json.NewEncoder(w).Encode(vmView(rec, false))
}

// startContainer creates the VM's network and container. It fails fast and
// leaves nothing behind (unlike ACI's silent stub fallback): a VM whose
// compute environment never started must not exist.
func (h *Handler) startContainer(rg, name, image, secret string) error {
	network := vmNetworkName(rg, name)
	containerName := vmContainerName(rg, name)
	if err := dockerNetworkCreate(network); err != nil {
		return err
	}
	// A stale container with the same name would make `docker run` fail;
	// remove leftovers from a previous unclean delete first.
	dockerRemove(containerName)
	_, err := dockerRun(runOpts{
		name:    containerName,
		image:   image,
		env:     identityEnv(secret),
		network: network,
		// Portable keep-alive: busybox sleep (alpine) rejects "infinity".
		cmd: []string{"sh", "-c", "while true; do sleep 3600; done"},
	})
	if err != nil {
		dockerNetworkRemove(network)
	}
	return err
}

func (h *Handler) GetVM(w http.ResponseWriter, r *http.Request) {
	sub := chi.URLParam(r, "subscriptionId")
	rg := chi.URLParam(r, "resourceGroupName")
	name := chi.URLParam(r, "vmName")

	rec, ok := h.getVM(sub, rg, name)
	if !ok {
		azerr.NotFound(w, "Microsoft.Compute/virtualMachines", name)
		return
	}
	h.refreshVM(rec)
	h.store.Set(vmKey(sub, rg, name), rec)
	expand := strings.EqualFold(r.URL.Query().Get("$expand"), "instanceView")
	json.NewEncoder(w).Encode(vmView(rec, expand))
}

func (h *Handler) GetInstanceView(w http.ResponseWriter, r *http.Request) {
	sub := chi.URLParam(r, "subscriptionId")
	rg := chi.URLParam(r, "resourceGroupName")
	name := chi.URLParam(r, "vmName")

	rec, ok := h.getVM(sub, rg, name)
	if !ok {
		azerr.NotFound(w, "Microsoft.Compute/virtualMachines", name)
		return
	}
	h.refreshVM(rec)
	json.NewEncoder(w).Encode(instanceViewFor(powerState(rec)))
}

func (h *Handler) listVMs(w http.ResponseWriter, prefix string) {
	items := h.store.ListByPrefix(prefix)
	value := make([]interface{}, 0, len(items))
	for _, it := range items {
		rec, ok := it.(map[string]interface{})
		if !ok {
			continue
		}
		h.refreshVM(rec)
		value = append(value, publicView(rec))
	}
	json.NewEncoder(w).Encode(map[string]interface{}{"value": value})
}

func (h *Handler) ListVMs(w http.ResponseWriter, r *http.Request) {
	sub := chi.URLParam(r, "subscriptionId")
	rg := chi.URLParam(r, "resourceGroupName")
	h.listVMs(w, "vm:"+sub+":"+rg+":")
}

func (h *Handler) ListAllVMs(w http.ResponseWriter, r *http.Request) {
	sub := chi.URLParam(r, "subscriptionId")
	h.listVMs(w, "vm:"+sub+":")
}

func (h *Handler) DeleteVM(w http.ResponseWriter, r *http.Request) {
	sub := chi.URLParam(r, "subscriptionId")
	rg := chi.URLParam(r, "resourceGroupName")
	name := chi.URLParam(r, "vmName")

	rec, ok := h.getVM(sub, rg, name)
	if !ok {
		azerr.NotFound(w, "Microsoft.Compute/virtualMachines", name)
		return
	}

	if h.dockerAvail {
		for _, svc := range h.serviceRecords(sub, rg, name) {
			if cn, _ := svc["_containerName"].(string); cn != "" {
				dockerRemove(cn)
			}
		}
		dockerRemove(vmContainerName(rg, name))
		dockerNetworkRemove(vmNetworkName(rg, name))
	}

	// Attached user disks and NICs survive the VM; only the auto-created OS
	// disk goes with it.
	removeDependencies(h.store, sub, rg, rec)
	h.store.DeleteByPrefix("vmsvc:" + sub + ":" + rg + ":" + name + ":")
	h.store.DeleteByPrefix(extensionKey(sub, rg, name, ""))
	if secret, _ := rec["_identitySecret"].(string); secret != "" {
		h.store.Delete(secretKey(secret))
	}
	h.store.Delete(vmKey(sub, rg, name))

	w.WriteHeader(http.StatusNoContent)
}

// ---------------------------------------------------------------------------
// Power actions
// ---------------------------------------------------------------------------

// powerTargets maps each power action to the state the VM ends up in.
var powerTargets = map[string]string{
	"start":      stateRunning,
	"powerOff":   stateStopped,
	"deallocate": stateDeallocated,
	"restart":    stateRunning,
	"redeploy":   stateRunning,
}

// powerAction moves the VM to the action's target state. With Docker the
// container is started/stopped for real; in stub mode only the recorded state
// changes. Both return 200 with the VM, the contract the Docker backend
// already shipped with.
func (h *Handler) powerAction(w http.ResponseWriter, r *http.Request, action string) {
	sub := chi.URLParam(r, "subscriptionId")
	rg := chi.URLParam(r, "resourceGroupName")
	name := chi.URLParam(r, "vmName")

	rec, ok := h.getVM(sub, rg, name)
	if !ok {
		azerr.NotFound(w, "Microsoft.Compute/virtualMachines", name)
		return
	}

	target := powerTargets[action]
	if h.dockerAvail {
		if err := h.applyContainerPower(rec, sub, rg, name, action); err != nil {
			azerr.WriteError(w, http.StatusConflict, "ContainerStartFailed", err.Error())
			return
		}
	}
	// Docker cannot tell stopped from deallocated, so a non-running target is
	// authoritative.
	if target != stateRunning || !h.dockerAvail {
		getMinibluProps(rec)["powerState"] = target
	}

	appendProvisionEvent(rec, "power action %s completed (state %s)", action, powerState(rec))
	h.store.Set(vmKey(sub, rg, name), rec)
	json.NewEncoder(w).Encode(publicView(rec))
}

func (h *Handler) applyContainerPower(rec map[string]interface{}, sub, rg, name, action string) error {
	containerName := vmContainerName(rg, name)
	services := h.serviceRecords(sub, rg, name)

	stop := func() error {
		for _, svc := range services {
			if cn, _ := svc["_containerName"].(string); cn != "" {
				dockerStop(cn)
			}
		}
		return dockerStop(containerName)
	}
	start := func() error {
		if err := dockerStart(containerName); err != nil {
			return err
		}
		for _, svc := range services {
			if cn, _ := svc["_containerName"].(string); cn != "" {
				dockerStart(cn)
			}
		}
		return nil
	}

	h.refreshVM(rec)
	running := powerState(rec) == stateRunning
	var err error
	switch action {
	case "start":
		if !running {
			err = start()
		}
	case "powerOff", "deallocate":
		if running {
			err = stop()
		}
	case "restart", "redeploy":
		stop()
		err = start()
	}
	if err == nil {
		h.refreshVM(rec)
	}
	return err
}

func (h *Handler) StartVM(w http.ResponseWriter, r *http.Request) {
	h.powerAction(w, r, "start")
}

func (h *Handler) PowerOffVM(w http.ResponseWriter, r *http.Request) {
	h.powerAction(w, r, "powerOff")
}

func (h *Handler) RestartVM(w http.ResponseWriter, r *http.Request) {
	h.powerAction(w, r, "restart")
}

func (h *Handler) DeallocateVM(w http.ResponseWriter, r *http.Request) {
	h.powerAction(w, r, "deallocate")
}

func (h *Handler) RedeployVM(w http.ResponseWriter, r *http.Request) {
	h.powerAction(w, r, "redeploy")
}

func imageSuffix(image string) string {
	if image == "" {
		return ""
	}
	return " (image " + image + ")"
}

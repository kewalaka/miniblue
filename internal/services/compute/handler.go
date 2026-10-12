package compute

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/moabukar/miniblue/internal/azerr"
	"github.com/moabukar/miniblue/internal/store"
)

type Handler struct{}

func NewHandler(_ *store.Store) *Handler {
	return &Handler{}
}

func (h *Handler) Register(r chi.Router) {
	r.Get("/subscriptions/{subscriptionId}/providers/Microsoft.Compute/locations/{location}/vmSizes", h.ListVMSizes)
	r.Get("/subscriptions/{subscriptionId}/providers/Microsoft.Compute/skus", h.ListSKUs)
	r.Route("/subscriptions/{subscriptionId}/providers/Microsoft.Compute/locations/{location}/publishers", func(r chi.Router) {
		r.Get("/", h.ListPublishers)
		r.Get("/{publisherName}/artifacttypes/vmimage/offers", h.ListOffers)
		r.Get("/{publisherName}/artifacttypes/vmimage/offers/{offerName}/skus", h.ListImageSKUs)
		r.Get("/{publisherName}/artifacttypes/vmimage/offers/{offerName}/skus/{skuName}/versions", h.ListImageVersions)
		r.Get("/{publisherName}/artifacttypes/vmimage/offers/{offerName}/skus/{skuName}/versions/{versionName}", h.GetImageVersion)
	})
}

func (h *Handler) ListVMSizes(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]interface{}{"value": []interface{}{
		map[string]interface{}{
			"name":                 "Standard_DS1_v2",
			"numberOfCores":        1,
			"osDiskSizeInMB":       1047552,
			"resourceDiskSizeInMB": 7168,
			"memoryInMB":           3584,
			"maxDataDiskCount":     4,
		},
		map[string]interface{}{
			"name":                 "Standard_DS2_v2",
			"numberOfCores":        2,
			"osDiskSizeInMB":       1047552,
			"resourceDiskSizeInMB": 14336,
			"memoryInMB":           7168,
			"maxDataDiskCount":     8,
		},
	}})
}

func (h *Handler) ListSKUs(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, map[string]interface{}{"value": []interface{}{
		computeSKU("Standard_DS1_v2", "virtualMachines", []interface{}{
			capability("vCPUs", "1"),
			capability("MemoryGB", "3.5"),
			capability("MaxDataDiskCount", "4"),
		}),
		computeSKU("Standard_DS2_v2", "virtualMachines", []interface{}{
			capability("vCPUs", "2"),
			capability("MemoryGB", "7"),
			capability("MaxDataDiskCount", "8"),
		}),
		computeSKU("Standard_LRS", "disks", nil),
		computeSKU("Premium_LRS", "disks", nil),
	}})
}

func (h *Handler) ListPublishers(w http.ResponseWriter, r *http.Request) {
	items := []interface{}{}
	for _, img := range imageCatalog() {
		items = append(items, catalogItem(r, img.publisher))
	}
	writeJSON(w, map[string]interface{}{"value": items})
}

func (h *Handler) ListOffers(w http.ResponseWriter, r *http.Request) {
	publisher := chi.URLParam(r, "publisherName")
	items := []interface{}{}
	for _, img := range imageCatalog() {
		if img.publisher == publisher {
			items = append(items, catalogItem(r, img.offer))
		}
	}
	writeJSON(w, map[string]interface{}{"value": items})
}

func (h *Handler) ListImageSKUs(w http.ResponseWriter, r *http.Request) {
	publisher := chi.URLParam(r, "publisherName")
	offer := chi.URLParam(r, "offerName")
	items := []interface{}{}
	for _, img := range imageCatalog() {
		if img.publisher == publisher && img.offer == offer {
			items = append(items, catalogItem(r, img.sku))
		}
	}
	writeJSON(w, map[string]interface{}{"value": items})
}

func (h *Handler) ListImageVersions(w http.ResponseWriter, r *http.Request) {
	publisher := chi.URLParam(r, "publisherName")
	offer := chi.URLParam(r, "offerName")
	sku := chi.URLParam(r, "skuName")
	items := []interface{}{}
	for _, img := range imageCatalog() {
		if img.publisher == publisher && img.offer == offer && img.sku == sku {
			items = append(items, imageVersion(r, img, img.version))
			if img.version != "latest" {
				items = append(items, imageVersion(r, img, "latest"))
			}
			break
		}
	}
	writeJSON(w, map[string]interface{}{"value": items})
}

func (h *Handler) GetImageVersion(w http.ResponseWriter, r *http.Request) {
	publisher := chi.URLParam(r, "publisherName")
	offer := chi.URLParam(r, "offerName")
	sku := chi.URLParam(r, "skuName")
	version := chi.URLParam(r, "versionName")
	for _, img := range imageCatalog() {
		if img.publisher == publisher && img.offer == offer && img.sku == sku {
			writeJSON(w, imageVersion(r, img, version))
			return
		}
	}
	azerr.NotFound(w, "Microsoft.Compute/locations/publishers/artifacttypes/vmimage", publisher+"/"+offer+"/"+sku+"/"+version)
}

func writeJSON(w http.ResponseWriter, v interface{}) {
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("[compute] failed to write JSON response: %v", err)
	}
}

func computeSKU(name, resourceType string, capabilities []interface{}) map[string]interface{} {
	sku := map[string]interface{}{
		"resourceType": resourceType,
		"name":         name,
		"tier":         "Standard",
		"size":         name,
		"locations":    []interface{}{"eastus", "westus"},
		"locationInfo": []interface{}{
			map[string]interface{}{"location": "eastus", "zones": []interface{}{}},
			map[string]interface{}{"location": "westus", "zones": []interface{}{}},
		},
		"capabilities": capabilities,
	}
	if resourceType == "virtualMachines" {
		sku["family"] = "standardDSv2Family"
	}
	return sku
}

func capability(name, value string) map[string]interface{} {
	return map[string]interface{}{"name": name, "value": value}
}

type imageRef struct {
	publisher string
	offer     string
	sku       string
	version   string
	osType    string
}

func imageCatalog() []imageRef {
	return []imageRef{
		{publisher: "Canonical", offer: "0001-com-ubuntu-server-jammy", sku: "22_04-lts", version: "22.04.202405210", osType: "Linux"},
		{publisher: "OpenLogic", offer: "CentOS", sku: "7_9", version: "7.9.2024052100", osType: "Linux"},
		{publisher: "MicrosoftWindowsServer", offer: "WindowsServer", sku: "2019-Datacenter", version: "17763.5936.240505", osType: "Windows"},
	}
}

func LookupImage(publisher, offer, sku string) (osType string, ok bool) {
	for _, img := range imageCatalog() {
		if strings.EqualFold(img.publisher, publisher) &&
			strings.EqualFold(img.offer, offer) &&
			strings.EqualFold(img.sku, sku) {
			return img.osType, true
		}
	}
	return "", false
}

func catalogItem(r *http.Request, name string) map[string]interface{} {
	return map[string]interface{}{
		"name":     name,
		"location": chi.URLParam(r, "location"),
		"id":       catalogID(r, name),
	}
}

func imageVersion(r *http.Request, img imageRef, version string) map[string]interface{} {
	if version == "" {
		version = img.version
	}
	return map[string]interface{}{
		"name":     version,
		"location": chi.URLParam(r, "location"),
		"id":       imageVersionID(r, img, version),
		"properties": map[string]interface{}{
			"storageProfile": map[string]interface{}{
				"osDiskImage":    map[string]interface{}{"operatingSystem": img.osType},
				"dataDiskImages": []interface{}{},
			},
		},
	}
}

func catalogID(r *http.Request, name string) string {
	return strings.TrimRight(r.URL.Path, "/") + "/" + name
}

func imageVersionID(r *http.Request, img imageRef, version string) string {
	return "/subscriptions/" + chi.URLParam(r, "subscriptionId") +
		"/providers/Microsoft.Compute/locations/" + chi.URLParam(r, "location") +
		"/publishers/" + img.publisher +
		"/artifacttypes/vmimage/offers/" + img.offer +
		"/skus/" + img.sku +
		"/versions/" + version
}

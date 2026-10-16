package appconfig

import (
	"encoding/json"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/moabukar/miniblue/internal/azerr"
	"github.com/moabukar/miniblue/internal/store"
)

type KeyValue struct {
	Key          string `json:"key"`
	Value        string `json:"value"`
	Label        string `json:"label,omitempty"`
	ContentType  string `json:"content_type,omitempty"`
	LastModified string `json:"last_modified"`
	Etag         string `json:"etag"`
}

type Handler struct {
	store *store.Store
}

func NewHandler(s *store.Store) *Handler {
	return &Handler{store: s}
}

func (h *Handler) Register(r chi.Router) {
	// ARM-style paths: used by Azure SDKs to enumerate and manage App Configuration stores
	r.Route("/subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/Microsoft.AppConfiguration/configurationStores", func(r chi.Router) {
		r.Get("/", h.ListStores)
		r.Route("/{configStoreName}", func(r chi.Router) {
			r.Put("/", h.CreateOrUpdateStore)
			r.Get("/", h.GetStore)
			r.Delete("/", h.DeleteStore)
			r.Post("/listKeys", h.ListKeys)
			r.Get("/replicas", h.ListReplicas)
		})
	})

	// Data-plane paths: used for key-value operations
	r.Get("/appconfig/{configStoreName}/kv", h.ListKeyValues)
	r.Get("/appconfig/{configStoreName}/kv/", h.ListKeyValues)
	r.Put("/appconfig/{configStoreName}/kv/*", h.SetKeyValue)
	r.Get("/appconfig/{configStoreName}/kv/*", h.GetKeyValue)
	r.Delete("/appconfig/{configStoreName}/kv/*", h.DeleteKeyValue)

	// Public Azure-compatible data-plane paths, routed by Host/SNI name
	// (for example: https://my-store.azconfig.io/kv/my-key).
	r.Get("/kv", h.ListKeyValues)
	r.Get("/kv/", h.ListKeyValues)
	r.Put("/kv/*", h.SetKeyValue)
	r.Get("/kv/*", h.GetKeyValue)
	r.Delete("/kv/*", h.DeleteKeyValue)
}

func (h *Handler) kvKey(storeName, key, label string) string {
	return h.kvPrefix(storeName) + url.QueryEscape(label) + ":" + url.PathEscape(key)
}

func (h *Handler) kvPrefix(storeName string) string {
	return "appconfig:kv:" + url.PathEscape(storeName) + ":"
}

func requestStoreName(r *http.Request) string {
	if storeName := chi.URLParam(r, "configStoreName"); storeName != "" {
		return storeName
	}
	host := r.Host
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	return strings.TrimSuffix(host, ".azconfig.io")
}

func requestKey(r *http.Request) string {
	return chi.URLParam(r, "*")
}

func requestLabel(r *http.Request, kv KeyValue) string {
	if label := r.URL.Query().Get("label"); label != "" {
		return label
	}
	return kv.Label
}

func (h *Handler) SetKeyValue(w http.ResponseWriter, r *http.Request) {
	storeName := requestStoreName(r)
	key := requestKey(r)

	var kv KeyValue
	json.NewDecoder(r.Body).Decode(&kv)
	kv.Key = key
	kv.Label = requestLabel(r, kv)
	kv.LastModified = time.Now().UTC().Format(time.RFC3339)
	kv.Etag = "etag-" + key

	w.Header().Set("ETag", kv.Etag)
	h.store.Set(h.kvKey(storeName, key, kv.Label), kv)
	json.NewEncoder(w).Encode(kv)
}

func (h *Handler) GetKeyValue(w http.ResponseWriter, r *http.Request) {
	storeName := requestStoreName(r)
	key := requestKey(r)
	label := r.URL.Query().Get("label")

	v, ok := h.store.Get(h.kvKey(storeName, key, label))
	if !ok {
		azerr.NotFound(w, "AppConfiguration/keyValues", key)
		return
	}
	if kv, ok := v.(KeyValue); ok {
		w.Header().Set("ETag", kv.Etag)
	}
	json.NewEncoder(w).Encode(v)
}

func (h *Handler) DeleteKeyValue(w http.ResponseWriter, r *http.Request) {
	storeName := requestStoreName(r)
	key := requestKey(r)
	label := r.URL.Query().Get("label")
	h.store.Delete(h.kvKey(storeName, key, label))
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) ListKeyValues(w http.ResponseWriter, r *http.Request) {
	storeName := requestStoreName(r)
	items := h.store.ListByPrefix(h.kvPrefix(storeName))
	json.NewEncoder(w).Encode(map[string]interface{}{"value": items})
}

// --- ARM configuration store handlers ---

func (h *Handler) storeARMKey(sub, rg, name string) string {
	return "appconfig:store:" + sub + ":" + rg + ":" + name
}

func (h *Handler) buildStoreResponse(sub, rg, name string, body map[string]interface{}) map[string]interface{} {
	location, _ := body["location"].(string)
	if location == "" {
		location = "eastus"
	}
	sku, _ := body["sku"].(map[string]interface{})
	if sku == nil {
		sku = map[string]interface{}{"name": "free"}
	}
	properties, _ := body["properties"].(map[string]interface{})
	if properties == nil {
		properties = map[string]interface{}{}
	}
	if encryption, ok := properties["encryption"].(map[string]interface{}); ok && isEmptyEncryption(encryption) {
		delete(properties, "encryption")
	}
	properties["provisioningState"] = "Succeeded"
	properties["endpoint"] = "https://" + name + ".azconfig.io"
	if _, ok := properties["creationDate"]; !ok {
		properties["creationDate"] = "2026-01-01T00:00:00Z"
	}
	if _, ok := properties["disableLocalAuth"]; !ok {
		properties["disableLocalAuth"] = false
	}

	store := map[string]interface{}{
		"id":         "/subscriptions/" + sub + "/resourceGroups/" + rg + "/providers/Microsoft.AppConfiguration/configurationStores/" + name,
		"name":       name,
		"type":       "Microsoft.AppConfiguration/configurationStores",
		"location":   location,
		"sku":        sku,
		"properties": properties,
	}
	if tags, ok := body["tags"]; ok {
		store["tags"] = tags
	}
	return store
}

func isEmptyEncryption(encryption map[string]interface{}) bool {
	if len(encryption) == 0 {
		return true
	}
	if len(encryption) != 1 {
		return false
	}
	keyVaultProperties, ok := encryption["keyVaultProperties"].(map[string]interface{})
	return ok && len(keyVaultProperties) == 0
}

func (h *Handler) CreateOrUpdateStore(w http.ResponseWriter, r *http.Request) {
	sub := chi.URLParam(r, "subscriptionId")
	rg := chi.URLParam(r, "resourceGroupName")
	name := chi.URLParam(r, "configStoreName")

	k := h.storeARMKey(sub, rg, name)
	_, exists := h.store.Get(k)

	var body map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		body = map[string]interface{}{}
	}
	store := h.buildStoreResponse(sub, rg, name, body)
	h.store.Set(k, store)

	if exists {
		w.WriteHeader(http.StatusOK)
	} else {
		w.WriteHeader(http.StatusCreated)
	}
	json.NewEncoder(w).Encode(store)
}

func (h *Handler) GetStore(w http.ResponseWriter, r *http.Request) {
	sub := chi.URLParam(r, "subscriptionId")
	rg := chi.URLParam(r, "resourceGroupName")
	name := chi.URLParam(r, "configStoreName")

	v, ok := h.store.Get(h.storeARMKey(sub, rg, name))
	if !ok {
		azerr.NotFound(w, "Microsoft.AppConfiguration/configurationStores", name)
		return
	}
	json.NewEncoder(w).Encode(v)
}

func (h *Handler) DeleteStore(w http.ResponseWriter, r *http.Request) {
	sub := chi.URLParam(r, "subscriptionId")
	rg := chi.URLParam(r, "resourceGroupName")
	name := chi.URLParam(r, "configStoreName")

	if !h.store.Delete(h.storeARMKey(sub, rg, name)) {
		azerr.NotFound(w, "Microsoft.AppConfiguration/configurationStores", name)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

func (h *Handler) ListStores(w http.ResponseWriter, r *http.Request) {
	sub := chi.URLParam(r, "subscriptionId")
	rg := chi.URLParam(r, "resourceGroupName")
	items := h.store.ListByPrefix("appconfig:store:" + sub + ":" + rg + ":")
	json.NewEncoder(w).Encode(map[string]interface{}{"value": items})
}

func (h *Handler) ListReplicas(w http.ResponseWriter, r *http.Request) {
	sub := chi.URLParam(r, "subscriptionId")
	rg := chi.URLParam(r, "resourceGroupName")
	name := chi.URLParam(r, "configStoreName")

	if _, ok := h.store.Get(h.storeARMKey(sub, rg, name)); !ok {
		azerr.NotFound(w, "Microsoft.AppConfiguration/configurationStores", name)
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{"value": []interface{}{}})
}

func (h *Handler) ListKeys(w http.ResponseWriter, r *http.Request) {
	sub := chi.URLParam(r, "subscriptionId")
	rg := chi.URLParam(r, "resourceGroupName")
	name := chi.URLParam(r, "configStoreName")

	v, ok := h.store.Get(h.storeARMKey(sub, rg, name))
	if !ok {
		azerr.NotFound(w, "Microsoft.AppConfiguration/configurationStores", name)
		return
	}

	endpoint := "https://" + name + ".azconfig.io"
	if store, ok := v.(map[string]interface{}); ok {
		if properties, ok := store["properties"].(map[string]interface{}); ok {
			if e, ok := properties["endpoint"].(string); ok && e != "" {
				endpoint = e
			}
		}
	}

	keys := []map[string]interface{}{
		{
			"id":               "primary",
			"name":             "Primary",
			"value":            "miniblue-primary-key",
			"connectionString": "Endpoint=" + endpoint + ";Id=primary;Secret=miniblue-primary-key",
			"lastModified":     "2024-01-01T00:00:00Z",
			"readOnly":         false,
		},
		{
			"id":               "secondary",
			"name":             "Secondary",
			"value":            "miniblue-secondary-key",
			"connectionString": "Endpoint=" + endpoint + ";Id=secondary;Secret=miniblue-secondary-key",
			"lastModified":     "2024-01-01T00:00:00Z",
			"readOnly":         false,
		},
	}
	json.NewEncoder(w).Encode(map[string]interface{}{"value": keys})
}

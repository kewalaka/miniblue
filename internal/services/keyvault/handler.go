package keyvault

import (
	"encoding/json"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/moabukar/miniblue/internal/azerr"
	"github.com/moabukar/miniblue/internal/store"
)

type Secret struct {
	ID          string                 `json:"id"`
	Value       string                 `json:"value,omitempty"`
	ContentType string                 `json:"contentType,omitempty"`
	Attributes  map[string]interface{} `json:"attributes,omitempty"`
	Tags        map[string]string      `json:"tags,omitempty"`
}

type Handler struct {
	store *store.Store
}

func NewHandler(s *store.Store) *Handler {
	return &Handler{store: s}
}

func (h *Handler) Register(r chi.Router) {
	r.Route("/subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/Microsoft.KeyVault/vaults", func(r chi.Router) {
		r.Get("/", h.ListVaultsByResourceGroup)
		r.Route("/{vaultName}", func(r chi.Router) {
			r.Put("/", h.CreateOrUpdateVault)
			r.Patch("/", h.UpdateVault)
			r.Get("/", h.GetVault)
			r.Delete("/", h.DeleteVault)
			r.Put("/accessPolicies/{operationKind}", h.UpdateAccessPolicies)
		})
	})
	r.Get("/subscriptions/{subscriptionId}/providers/Microsoft.KeyVault/vaults", h.ListVaultsBySubscription)
	r.Post("/subscriptions/{subscriptionId}/providers/Microsoft.KeyVault/checkNameAvailability", h.CheckNameAvailability)
	r.Get("/subscriptions/{subscriptionId}/providers/Microsoft.KeyVault/locations/{location}/operationResults/{operationId}", h.GetOperationResult)
	r.Get("/subscriptions/{subscriptionId}/providers/Microsoft.KeyVault/locations/{location}/deletedVaults", h.ListDeletedVaults)
	r.Route("/subscriptions/{subscriptionId}/providers/Microsoft.KeyVault/locations/{location}/deletedVaults/{vaultName}", func(r chi.Router) {
		r.Get("/", h.GetDeletedVault)
		r.Delete("/", h.PurgeDeletedVault)
	})

	r.Route("/keyvault/{vaultName}/secrets", func(r chi.Router) {
		r.Get("/", h.ListSecrets)
		r.Route("/{secretName}", func(r chi.Router) {
			r.Put("/", h.SetSecret)
			r.Get("/", h.GetSecret)
			r.Delete("/", h.DeleteSecret)
		})
	})

	// Azure-compatible Key Vault data-plane paths, routed by Host/SNI
	// (for example: https://myvault.vault.azure.net/secrets/my-secret).
	r.Get("/secrets", h.ListSecrets)
	r.Get("/secrets/", h.ListSecrets)
	r.Put("/secrets/{secretName}", h.SetSecret)
	r.Get("/secrets/{secretName}", h.GetSecret)
	r.Get("/secrets/{secretName}/", h.GetSecret)
	r.Get("/secrets/{secretName}/{secretVersion}", h.GetSecret)
	r.Delete("/secrets/{secretName}", h.DeleteSecret)
	r.Get("/deletedsecrets/{secretName}", h.GetDeletedSecret)
	r.Delete("/deletedsecrets/{secretName}", h.PurgeDeletedSecret)
}

func (h *Handler) key(vault, name string) string {
	return "kv:" + vault + ":" + name
}

func (h *Handler) versionKey(vault, name, version string) string {
	return "kvver:" + vault + ":" + name + ":" + version
}

func (h *Handler) deletedKey(vault, name string) string {
	return "kvdel:" + vault + ":" + name
}

func (h *Handler) vaultKey(sub, rg, name string) string {
	return "keyvault:vault:" + sub + ":" + rg + ":" + strings.ToLower(name)
}

func (h *Handler) vaultResourceGroupPrefix(sub, rg string) string {
	return "keyvault:vault:" + sub + ":" + rg + ":"
}

func (h *Handler) vaultSubscriptionPrefix(sub string) string {
	return "keyvault:vault:" + sub + ":"
}

func (h *Handler) deletedVaultKey(sub, location, name string) string {
	return "keyvault:deletedVault:" + sub + ":" + strings.ToLower(location) + ":" + strings.ToLower(name)
}

func (h *Handler) deletedVaultPrefix(sub, location string) string {
	return "keyvault:deletedVault:" + sub + ":" + strings.ToLower(location) + ":"
}

func vaultID(sub, rg, name string) string {
	return "/subscriptions/" + sub + "/resourceGroups/" + rg + "/providers/Microsoft.KeyVault/vaults/" + name
}

func mapValue(v interface{}) map[string]interface{} {
	m, _ := v.(map[string]interface{})
	return m
}

func stringValue(v interface{}) string {
	s, _ := v.(string)
	return s
}

func boolValue(v interface{}, def bool) bool {
	b, ok := v.(bool)
	if !ok {
		return def
	}
	return b
}

func intValue(v interface{}, def int) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	default:
		return def
	}
}

func sliceValue(v interface{}) []interface{} {
	if s, ok := v.([]interface{}); ok {
		return s
	}
	return nil
}

func mergeMap(dst, src map[string]interface{}) map[string]interface{} {
	if dst == nil {
		dst = map[string]interface{}{}
	}
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

func buildVaultResponse(sub, rg, name string, input, existing map[string]interface{}) map[string]interface{} {
	if input == nil {
		input = map[string]interface{}{}
	}
	if existing == nil {
		existing = map[string]interface{}{}
	}

	location := stringValue(input["location"])
	if location == "" {
		location = stringValue(existing["location"])
	}
	if location == "" {
		location = "eastus"
	}

	tags, tagsProvided := input["tags"]
	if !tagsProvided {
		tags = existing["tags"]
	}
	tagMap := mapValue(tags)
	if tagMap == nil {
		tagMap = map[string]interface{}{}
	}

	props := mergeMap(mapValue(existing["properties"]), mapValue(input["properties"]))
	if props == nil {
		props = map[string]interface{}{}
	}
	if stringValue(props["tenantId"]) == "" {
		props["tenantId"] = "00000000-0000-0000-0000-000000000000"
	}
	sku := mapValue(props["sku"])
	if sku == nil {
		sku = map[string]interface{}{}
	}
	if stringValue(sku["family"]) == "" {
		sku["family"] = "A"
	}
	if stringValue(sku["name"]) == "" {
		sku["name"] = "standard"
	}
	props["sku"] = sku
	if _, ok := props["accessPolicies"]; !ok {
		props["accessPolicies"] = []interface{}{}
	}
	if _, ok := props["enabledForDeployment"]; !ok {
		props["enabledForDeployment"] = false
	}
	if _, ok := props["enabledForDiskEncryption"]; !ok {
		props["enabledForDiskEncryption"] = false
	}
	if _, ok := props["enabledForTemplateDeployment"]; !ok {
		props["enabledForTemplateDeployment"] = false
	}
	if _, ok := props["enableSoftDelete"]; !ok {
		props["enableSoftDelete"] = true
	}
	if _, ok := props["enablePurgeProtection"]; !ok {
		props["enablePurgeProtection"] = false
	}
	if _, ok := props["enableRbacAuthorization"]; !ok {
		props["enableRbacAuthorization"] = false
	}
	if intValue(props["softDeleteRetentionInDays"], 0) == 0 {
		props["softDeleteRetentionInDays"] = 90
	}
	if stringValue(props["publicNetworkAccess"]) == "" {
		props["publicNetworkAccess"] = "Enabled"
	}
	if mapValue(props["networkAcls"]) == nil {
		props["networkAcls"] = map[string]interface{}{
			"bypass":              "AzureServices",
			"defaultAction":       "Allow",
			"ipRules":             []interface{}{},
			"virtualNetworkRules": []interface{}{},
		}
	}
	props["vaultUri"] = "https://" + name + ".vault.azure.net/"
	props["provisioningState"] = "Succeeded"

	systemData := mapValue(existing["systemData"])
	if systemData == nil {
		now := time.Now().UTC().Format(time.RFC3339)
		systemData = map[string]interface{}{
			"createdAt":          now,
			"createdBy":          "miniblue",
			"createdByType":      "Application",
			"lastModifiedAt":     now,
			"lastModifiedBy":     "miniblue",
			"lastModifiedByType": "Application",
		}
	}

	return map[string]interface{}{
		"id":         vaultID(sub, rg, name),
		"name":       name,
		"type":       "Microsoft.KeyVault/vaults",
		"location":   location,
		"tags":       tagMap,
		"systemData": systemData,
		"properties": props,
	}
}

func operationURL(r *http.Request, sub, location string) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + r.Host + "/subscriptions/" + sub + "/providers/Microsoft.KeyVault/locations/" + location + "/operationResults/" + strconv.FormatInt(time.Now().UTC().UnixNano(), 16) + "?api-version=" + r.URL.Query().Get("api-version")
}

func setOperationHeaders(w http.ResponseWriter, r *http.Request, sub, location string) {
	u := operationURL(r, sub, location)
	w.Header().Set("Azure-AsyncOperation", u)
	w.Header().Set("Location", u)
	w.Header().Set("Retry-After", "1")
}

func (h *Handler) CreateOrUpdateVault(w http.ResponseWriter, r *http.Request) {
	sub := chi.URLParam(r, "subscriptionId")
	rg := chi.URLParam(r, "resourceGroupName")
	name := chi.URLParam(r, "vaultName")

	var input map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		azerr.BadRequest(w, "The request content was invalid: "+err.Error())
		return
	}
	k := h.vaultKey(sub, rg, name)
	existing, exists := h.store.Get(k)
	vault := buildVaultResponse(sub, rg, name, input, mapValue(existing))
	h.store.Set(k, vault)

	setOperationHeaders(w, r, sub, stringValue(vault["location"]))
	if exists {
		w.WriteHeader(http.StatusOK)
	} else {
		w.WriteHeader(http.StatusCreated)
	}
	json.NewEncoder(w).Encode(vault)
}

func (h *Handler) UpdateVault(w http.ResponseWriter, r *http.Request) {
	sub := chi.URLParam(r, "subscriptionId")
	rg := chi.URLParam(r, "resourceGroupName")
	name := chi.URLParam(r, "vaultName")
	k := h.vaultKey(sub, rg, name)
	existing, ok := h.store.Get(k)
	if !ok {
		azerr.NotFound(w, "Microsoft.KeyVault/vaults", name)
		return
	}
	var patch map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
		azerr.BadRequest(w, "The request content was invalid: "+err.Error())
		return
	}
	vault := buildVaultResponse(sub, rg, name, patch, mapValue(existing))
	h.store.Set(k, vault)
	json.NewEncoder(w).Encode(vault)
}

func (h *Handler) GetVault(w http.ResponseWriter, r *http.Request) {
	sub := chi.URLParam(r, "subscriptionId")
	rg := chi.URLParam(r, "resourceGroupName")
	name := chi.URLParam(r, "vaultName")
	v, ok := h.store.Get(h.vaultKey(sub, rg, name))
	if !ok {
		azerr.NotFound(w, "Microsoft.KeyVault/vaults", name)
		return
	}
	json.NewEncoder(w).Encode(v)
}

func (h *Handler) DeleteVault(w http.ResponseWriter, r *http.Request) {
	sub := chi.URLParam(r, "subscriptionId")
	rg := chi.URLParam(r, "resourceGroupName")
	name := chi.URLParam(r, "vaultName")
	k := h.vaultKey(sub, rg, name)
	v, ok := h.store.Get(k)
	if !ok {
		azerr.NotFound(w, "Microsoft.KeyVault/vaults", name)
		return
	}
	vault := mapValue(v)
	props := mapValue(vault["properties"])
	location := stringValue(vault["location"])
	if boolValue(props["enableSoftDelete"], true) {
		now := time.Now().UTC()
		deleted := map[string]interface{}{
			"id":       "/subscriptions/" + sub + "/providers/Microsoft.KeyVault/locations/" + location + "/deletedVaults/" + name,
			"name":     name,
			"type":     "Microsoft.KeyVault/locations/deletedVaults",
			"location": location,
			"properties": map[string]interface{}{
				"vaultId":                vault["id"],
				"location":               location,
				"deletionDate":           now.Format(time.RFC3339),
				"scheduledPurgeDate":     now.Add(time.Duration(intValue(props["softDeleteRetentionInDays"], 90)) * 24 * time.Hour).Format(time.RFC3339),
				"purgeProtectionEnabled": boolValue(props["enablePurgeProtection"], false),
				"tags":                   vault["tags"],
			},
		}
		h.store.Set(h.deletedVaultKey(sub, location, name), deleted)
	}
	h.store.Delete(k)
	setOperationHeaders(w, r, sub, location)
	w.WriteHeader(http.StatusAccepted)
}

func (h *Handler) ListVaultsByResourceGroup(w http.ResponseWriter, r *http.Request) {
	sub := chi.URLParam(r, "subscriptionId")
	rg := chi.URLParam(r, "resourceGroupName")
	items := h.store.ListByPrefix(h.vaultResourceGroupPrefix(sub, rg))
	json.NewEncoder(w).Encode(map[string]interface{}{"value": items})
}

func (h *Handler) ListVaultsBySubscription(w http.ResponseWriter, r *http.Request) {
	sub := chi.URLParam(r, "subscriptionId")
	items := h.store.ListByPrefix(h.vaultSubscriptionPrefix(sub))
	json.NewEncoder(w).Encode(map[string]interface{}{"value": items})
}

func accessPolicyID(policy map[string]interface{}) string {
	return stringValue(policy["tenantId"]) + "/" + stringValue(policy["objectId"]) + "/" + stringValue(policy["applicationId"])
}

func (h *Handler) UpdateAccessPolicies(w http.ResponseWriter, r *http.Request) {
	sub := chi.URLParam(r, "subscriptionId")
	rg := chi.URLParam(r, "resourceGroupName")
	name := chi.URLParam(r, "vaultName")
	operation := strings.ToLower(chi.URLParam(r, "operationKind"))
	if operation != "add" && operation != "remove" && operation != "replace" {
		azerr.BadRequest(w, "The access policy operation must be add, remove, or replace.")
		return
	}

	k := h.vaultKey(sub, rg, name)
	existing, ok := h.store.Get(k)
	if !ok {
		azerr.NotFound(w, "Microsoft.KeyVault/vaults", name)
		return
	}
	var input map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		azerr.BadRequest(w, "The request content was invalid: "+err.Error())
		return
	}
	incoming := sliceValue(mapValue(input["properties"])["accessPolicies"])
	vault := mapValue(existing)
	props := mapValue(vault["properties"])
	current := sliceValue(props["accessPolicies"])
	if operation == "replace" {
		props["accessPolicies"] = incoming
	} else {
		byID := map[string]interface{}{}
		for _, policy := range current {
			if m := mapValue(policy); m != nil {
				byID[accessPolicyID(m)] = policy
			}
		}
		for _, policy := range incoming {
			m := mapValue(policy)
			if m == nil {
				continue
			}
			id := accessPolicyID(m)
			if operation == "add" {
				byID[id] = policy
			} else {
				delete(byID, id)
			}
		}
		updated := make([]interface{}, 0, len(byID))
		for _, policy := range current {
			if m := mapValue(policy); m != nil {
				id := accessPolicyID(m)
				if v, ok := byID[id]; ok {
					updated = append(updated, v)
					delete(byID, id)
				}
			}
		}
		for _, policy := range byID {
			updated = append(updated, policy)
		}
		props["accessPolicies"] = updated
	}
	vault["properties"] = props
	h.store.Set(k, vault)
	json.NewEncoder(w).Encode(vault)
}

var vaultNamePattern = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9-]{1,22}[a-zA-Z0-9]$`)

func validVaultName(name string) bool {
	return vaultNamePattern.MatchString(name) && !strings.Contains(name, "--")
}

func (h *Handler) CheckNameAvailability(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Name string `json:"name"`
		Type string `json:"type"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		azerr.BadRequest(w, "The request content was invalid: "+err.Error())
		return
	}
	resp := map[string]interface{}{"nameAvailable": true}
	if !validVaultName(input.Name) {
		resp["nameAvailable"] = false
		resp["reason"] = "AccountNameInvalid"
		resp["message"] = "The vault name is invalid. Vault names must be 3-24 characters, start with a letter, end with a letter or digit, contain only letters, digits, and hyphens, and not contain consecutive hyphens."
		json.NewEncoder(w).Encode(resp)
		return
	}
	for _, item := range h.store.ListByPrefix("keyvault:vault:") {
		if vault := mapValue(item); strings.EqualFold(stringValue(vault["name"]), input.Name) {
			resp["nameAvailable"] = false
			resp["reason"] = "AlreadyExists"
			resp["message"] = "The vault name '" + input.Name + "' is already in use."
			break
		}
	}
	if resp["nameAvailable"] == true {
		for _, item := range h.store.ListByPrefix("keyvault:deletedVault:") {
			if vault := mapValue(item); strings.EqualFold(stringValue(vault["name"]), input.Name) {
				resp["nameAvailable"] = false
				resp["reason"] = "AlreadyExists"
				resp["message"] = "The vault name '" + input.Name + "' is currently in a deleted but recoverable state."
				break
			}
		}
	}
	json.NewEncoder(w).Encode(resp)
}

func (h *Handler) GetOperationResult(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode(map[string]interface{}{"status": "Succeeded"})
}

func (h *Handler) ListDeletedVaults(w http.ResponseWriter, r *http.Request) {
	sub := chi.URLParam(r, "subscriptionId")
	location := chi.URLParam(r, "location")
	items := h.store.ListByPrefix(h.deletedVaultPrefix(sub, location))
	json.NewEncoder(w).Encode(map[string]interface{}{"value": items})
}

func (h *Handler) GetDeletedVault(w http.ResponseWriter, r *http.Request) {
	sub := chi.URLParam(r, "subscriptionId")
	location := chi.URLParam(r, "location")
	name := chi.URLParam(r, "vaultName")
	v, ok := h.store.Get(h.deletedVaultKey(sub, location, name))
	if !ok {
		azerr.NotFound(w, "Microsoft.KeyVault/locations/deletedVaults", name)
		return
	}
	json.NewEncoder(w).Encode(v)
}

func (h *Handler) PurgeDeletedVault(w http.ResponseWriter, r *http.Request) {
	sub := chi.URLParam(r, "subscriptionId")
	location := chi.URLParam(r, "location")
	name := chi.URLParam(r, "vaultName")
	if !h.store.Delete(h.deletedVaultKey(sub, location, name)) {
		azerr.NotFound(w, "Microsoft.KeyVault/locations/deletedVaults", name)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func requestVaultName(r *http.Request) string {
	if vaultName := chi.URLParam(r, "vaultName"); vaultName != "" {
		return vaultName
	}
	host := r.Host
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	return strings.TrimSuffix(host, ".vault.azure.net")
}

func unixNow() int64 {
	return time.Now().UTC().Unix()
}

func deletedSecretResponse(vault, name string, secret interface{}) map[string]interface{} {
	resp := map[string]interface{}{
		"id":         "https://" + vault + ".vault.azure.net/secrets/" + name,
		"recoveryId": "https://" + vault + ".vault.azure.net/deletedsecrets/" + name,
		"attributes": map[string]interface{}{
			"enabled":            true,
			"deletedDate":        unixNow(),
			"scheduledPurgeDate": unixNow() + int64((90 * 24 * time.Hour).Seconds()),
		},
	}
	if s, ok := secret.(Secret); ok {
		resp["value"] = s.Value
		resp["contentType"] = s.ContentType
		resp["tags"] = s.Tags
	}
	return resp
}

func (h *Handler) SetSecret(w http.ResponseWriter, r *http.Request) {
	vault := requestVaultName(r)
	name := chi.URLParam(r, "secretName")

	var body struct {
		Value       string            `json:"value"`
		ContentType string            `json:"contentType"`
		Tags        map[string]string `json:"tags"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		azerr.BadRequest(w, "Invalid request body: "+err.Error())
		return
	}

	now := unixNow()
	version := strconv.FormatInt(time.Now().UTC().UnixNano(), 16)
	secret := Secret{
		ID:          "https://" + vault + ".vault.azure.net/secrets/" + name + "/" + version,
		Value:       body.Value,
		ContentType: body.ContentType,
		Tags:        body.Tags,
		Attributes: map[string]interface{}{
			"enabled": true,
			"created": now,
			"updated": now,
		},
	}

	h.store.Set(h.key(vault, name), secret)
	h.store.Set(h.versionKey(vault, name, version), secret)
	h.store.Delete(h.deletedKey(vault, name))
	json.NewEncoder(w).Encode(secret)
}

func (h *Handler) GetSecret(w http.ResponseWriter, r *http.Request) {
	vault := requestVaultName(r)
	name := chi.URLParam(r, "secretName")
	version := chi.URLParam(r, "secretVersion")

	key := h.key(vault, name)
	if version != "" {
		key = h.versionKey(vault, name, version)
	}
	v, ok := h.store.Get(key)
	if !ok {
		azerr.NotFound(w, "Microsoft.KeyVault/vaults/secrets", name)
		return
	}
	json.NewEncoder(w).Encode(v)
}

func (h *Handler) DeleteSecret(w http.ResponseWriter, r *http.Request) {
	vault := requestVaultName(r)
	name := chi.URLParam(r, "secretName")
	var deleted map[string]interface{}
	if v, ok := h.store.Get(h.key(vault, name)); ok {
		deleted = deletedSecretResponse(vault, name, v)
		h.store.Set(h.deletedKey(vault, name), deleted)
	} else {
		deleted = deletedSecretResponse(vault, name, nil)
	}
	h.store.Delete(h.key(vault, name))
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(deleted)
}

func (h *Handler) ListSecrets(w http.ResponseWriter, r *http.Request) {
	vault := requestVaultName(r)
	items := h.store.ListByPrefix("kv:" + vault + ":")
	// Azure Key Vault list returns metadata only, NOT the secret value
	redacted := make([]map[string]interface{}, 0, len(items))
	for _, item := range items {
		if s, ok := item.(Secret); ok {
			redacted = append(redacted, map[string]interface{}{
				"id":         s.ID,
				"attributes": s.Attributes,
			})
		}
	}
	json.NewEncoder(w).Encode(map[string]interface{}{"value": redacted})
}

func (h *Handler) GetDeletedSecret(w http.ResponseWriter, r *http.Request) {
	vault := requestVaultName(r)
	name := chi.URLParam(r, "secretName")
	v, ok := h.store.Get(h.deletedKey(vault, name))
	if !ok {
		azerr.NotFound(w, "Microsoft.KeyVault/vaults/deletedsecrets", name)
		return
	}
	json.NewEncoder(w).Encode(v)
}

func (h *Handler) PurgeDeletedSecret(w http.ResponseWriter, r *http.Request) {
	vault := requestVaultName(r)
	name := chi.URLParam(r, "secretName")
	h.store.Delete(h.deletedKey(vault, name))
	w.WriteHeader(http.StatusNoContent)
}

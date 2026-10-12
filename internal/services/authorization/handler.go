package authorization

import (
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/moabukar/miniblue/internal/azerr"
	"github.com/moabukar/miniblue/internal/store"
)

type Handler struct {
	store *store.Store
}

const authorizationProviderMarker = "/providers/microsoft.authorization/"

type builtinRole struct {
	ID          string
	Name        string
	Description string
	Actions     []interface{}
	DataActions []interface{}
}

var builtinRoles = []builtinRole{
	{ID: "acdd72a7-3385-48ef-bd42-f606fba81ae7", Name: "Reader", Description: "View all resources, but does not allow you to make any changes.", Actions: []interface{}{"*/read"}},
	{ID: "b24988ac-6180-42a0-ab88-20f7382dd24c", Name: "Contributor", Description: "Grants full access to manage all resources, but does not allow you to assign roles in Azure RBAC.", Actions: []interface{}{"*"}, DataActions: []interface{}{}},
	{ID: "8e3af657-a8ff-443c-a75c-2fe8c4bcb635", Name: "Owner", Description: "Grants full access to manage all resources, including the ability to assign roles in Azure RBAC.", Actions: []interface{}{"*"}},
	{ID: "2a2b9908-6ea1-4ae2-8e65-a410df84e7d1", Name: "Storage Blob Data Reader", Description: "Read and list Azure Storage blob containers and blobs.", DataActions: []interface{}{"Microsoft.Storage/storageAccounts/blobServices/containers/blobs/read"}},
	{ID: "ba92f5b4-2d11-453d-a403-e96b0029c9fe", Name: "Storage Blob Data Contributor", Description: "Read, write, and delete Azure Storage containers and blobs.", DataActions: []interface{}{"Microsoft.Storage/storageAccounts/blobServices/containers/blobs/*"}},
	{ID: "4633458b-17de-408a-b874-0445c86b69e6", Name: "Key Vault Secrets User", Description: "Read secret contents.", DataActions: []interface{}{"Microsoft.KeyVault/vaults/secrets/readSecret/action"}},
	{ID: "b86a8fe4-44ce-4948-aee5-eccb2c155cd7", Name: "Key Vault Secrets Officer", Description: "Perform any action on the secrets of a key vault, except manage permissions.", DataActions: []interface{}{"Microsoft.KeyVault/vaults/secrets/*"}},
	{ID: "516239f1-63e1-4d78-a4de-a74fb236a071", Name: "App Configuration Data Reader", Description: "Read App Configuration data.", DataActions: []interface{}{"Microsoft.AppConfiguration/configurationStores/keyValues/read"}},
	{ID: "fbdf93bf-df7d-467e-a4d2-9458aa1360c8", Name: "Cosmos DB Account Reader Role", Description: "Can read Azure Cosmos DB account data.", Actions: []interface{}{"Microsoft.DocumentDB/databaseAccounts/read"}},
}

func NewHandler(s *store.Store) *Handler {
	return &Handler{store: s}
}

func (h *Handler) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(strings.ToLower(r.URL.Path), authorizationProviderMarker) {
			h.Dispatch(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (h *Handler) Register(r chi.Router) {
	h.registerScope(r, "/subscriptions/{subscriptionId}/providers/Microsoft.Authorization")
	h.registerScope(r, "/subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/Microsoft.Authorization")
	h.registerScope(r, "/subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/Microsoft.KeyVault/vaults/{vaultName}/providers/Microsoft.Authorization")
	h.registerScope(r, "/subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/Microsoft.Storage/storageAccounts/{accountName}/providers/Microsoft.Authorization")
	for _, scope := range []string{
		"/subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/{resourceProvider}/{resourceType}/{resourceName}",
		"/subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/{resourceProvider}/{resourceType}/{resourceName}/{childType}/{childName}",
		"/subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/{resourceProvider}/{resourceType}/{resourceName}/{childType}/{childName}/{grandchildType}/{grandchildName}",
	} {
		r.HandleFunc(scope+"/providers/Microsoft.Authorization/*", h.Dispatch)
	}
}

func (h *Handler) registerScope(r chi.Router, base string) {
	r.HandleFunc(base+"/roleAssignments", h.Dispatch)
	r.HandleFunc(base+"/roleAssignments/{roleAssignmentName}", h.Dispatch)
	r.HandleFunc(base+"/roleDefinitions", h.Dispatch)
	r.HandleFunc(base+"/roleDefinitions/{roleDefinitionName}", h.Dispatch)
}

func (h *Handler) Dispatch(w http.ResponseWriter, r *http.Request) {
	scope, kind, name, ok := parseAuthorizationPath(r.URL.Path)
	if !ok {
		azerr.NotFound(w, "Microsoft.Authorization", strings.TrimPrefix(r.URL.Path, "/"))
		return
	}
	switch {
	case strings.EqualFold(kind, "roleAssignments"):
		h.handleRoleAssignments(w, r, scope, name)
	case strings.EqualFold(kind, "roleDefinitions"):
		h.handleRoleDefinitions(w, r, scope, name)
	default:
		azerr.NotFound(w, "Microsoft.Authorization", kind)
	}
}

func parseAuthorizationPath(path string) (scope, kind, name string, ok bool) {
	idx := strings.Index(strings.ToLower(path), authorizationProviderMarker)
	if idx <= 0 {
		return "", "", "", false
	}
	scope = path[:idx]
	parts := strings.Split(strings.Trim(path[idx+len(authorizationProviderMarker):], "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		return "", "", "", false
	}
	kind = parts[0]
	if len(parts) > 1 {
		name = parts[1]
	}
	return scope, kind, name, true
}

func (h *Handler) handleRoleAssignments(w http.ResponseWriter, r *http.Request, scope, name string) {
	switch r.Method {
	case http.MethodPut:
		if name == "" {
			azerr.BadRequest(w, "role assignment name is required")
			return
		}
		h.createOrUpdateRoleAssignment(w, r, scope, name)
	case http.MethodGet:
		if name == "" {
			h.listRoleAssignments(w, scope)
			return
		}
		h.getRoleAssignment(w, scope, name)
	case http.MethodDelete:
		if name == "" {
			azerr.BadRequest(w, "role assignment name is required")
			return
		}
		h.deleteRoleAssignment(w, scope, name)
	default:
		azerr.MethodNotAllowed(w, r.Method)
	}
}

func (h *Handler) handleRoleDefinitions(w http.ResponseWriter, r *http.Request, scope, name string) {
	switch r.Method {
	case http.MethodPut:
		if name == "" {
			azerr.BadRequest(w, "role definition name is required")
			return
		}
		h.createOrUpdateRoleDefinition(w, r, scope, name)
	case http.MethodGet:
		if name == "" {
			h.listRoleDefinitions(w, r, scope)
			return
		}
		h.getRoleDefinition(w, scope, name)
	case http.MethodDelete:
		if name == "" {
			azerr.BadRequest(w, "role definition name is required")
			return
		}
		h.deleteRoleDefinition(w, scope, name)
	default:
		azerr.MethodNotAllowed(w, r.Method)
	}
}

func (h *Handler) roleAssignmentKey(scope, name string) string {
	return "authorization:roleAssignment:" + scope + ":" + name
}

func (h *Handler) roleAssignmentPrefix(scope string) string {
	return "authorization:roleAssignment:" + scope + ":"
}

func (h *Handler) createOrUpdateRoleAssignment(w http.ResponseWriter, r *http.Request, scope, name string) {
	var input map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		azerr.BadRequest(w, "The request content was invalid: "+err.Error())
		return
	}
	props := mapValue(input["properties"])
	if props == nil {
		azerr.BadRequest(w, "properties are required")
		return
	}
	k := h.roleAssignmentKey(scope, name)
	_, exists := h.store.Get(k)
	assignment := buildRoleAssignment(scope, name, props)
	h.store.Set(k, assignment)
	if exists {
		w.WriteHeader(http.StatusOK)
	} else {
		w.WriteHeader(http.StatusCreated)
	}
	json.NewEncoder(w).Encode(assignment)
}

func buildRoleAssignment(scope, name string, props map[string]interface{}) map[string]interface{} {
	outProps := map[string]interface{}{
		"scope":            scope,
		"roleDefinitionId": stringValue(props["roleDefinitionId"]),
		"principalId":      stringValue(props["principalId"]),
		"principalType":    stringValue(props["principalType"]),
		"createdOn":        time.Now().UTC().Format(time.RFC3339),
		"updatedOn":        time.Now().UTC().Format(time.RFC3339),
	}
	for _, key := range []string{"condition", "conditionVersion", "delegatedManagedIdentityResourceId", "description"} {
		if v, ok := props[key]; ok {
			outProps[key] = v
		}
	}
	return map[string]interface{}{
		"id":         scope + "/providers/Microsoft.Authorization/roleAssignments/" + name,
		"name":       name,
		"type":       "Microsoft.Authorization/roleAssignments",
		"properties": outProps,
	}
}

func (h *Handler) getRoleAssignment(w http.ResponseWriter, scope, name string) {
	v, ok := h.store.Get(h.roleAssignmentKey(scope, name))
	if !ok {
		azerr.NotFound(w, "Microsoft.Authorization/roleAssignments", name)
		return
	}
	json.NewEncoder(w).Encode(v)
}

func (h *Handler) listRoleAssignments(w http.ResponseWriter, scope string) {
	items := h.store.ListByPrefix(h.roleAssignmentPrefix(scope))
	json.NewEncoder(w).Encode(map[string]interface{}{"value": items})
}

func (h *Handler) deleteRoleAssignment(w http.ResponseWriter, scope, name string) {
	v, ok := h.store.Get(h.roleAssignmentKey(scope, name))
	if !ok {
		azerr.NotFound(w, "Microsoft.Authorization/roleAssignments", name)
		return
	}
	h.store.Delete(h.roleAssignmentKey(scope, name))
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(v)
}

func (h *Handler) roleDefinitionKey(scope, name string) string {
	return "authorization:roleDefinition:" + scope + ":" + name
}

func (h *Handler) roleDefinitionPrefix(scope string) string {
	return "authorization:roleDefinition:" + scope + ":"
}

func (h *Handler) createOrUpdateRoleDefinition(w http.ResponseWriter, r *http.Request, scope, name string) {
	if roleByID(name) != nil {
		azerr.BadRequest(w, "built-in role definitions cannot be updated")
		return
	}
	var input map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		azerr.BadRequest(w, "The request content was invalid: "+err.Error())
		return
	}
	props := mapValue(input["properties"])
	if props == nil {
		props = map[string]interface{}{}
	}
	k := h.roleDefinitionKey(scope, name)
	_, exists := h.store.Get(k)
	definition := map[string]interface{}{
		"id":         scope + "/providers/Microsoft.Authorization/roleDefinitions/" + name,
		"name":       name,
		"type":       "Microsoft.Authorization/roleDefinitions",
		"properties": customRoleDefinitionProperties(scope, props),
	}
	h.store.Set(k, definition)
	if exists {
		w.WriteHeader(http.StatusOK)
	} else {
		w.WriteHeader(http.StatusCreated)
	}
	json.NewEncoder(w).Encode(definition)
}

func customRoleDefinitionProperties(scope string, input map[string]interface{}) map[string]interface{} {
	props := map[string]interface{}{
		"roleName":         stringValue(input["roleName"]),
		"type":             "CustomRole",
		"description":      stringValue(input["description"]),
		"assignableScopes": []interface{}{scope},
		"permissions":      []interface{}{},
	}
	for _, key := range []string{"roleName", "type", "description", "assignableScopes", "permissions"} {
		if v, ok := input[key]; ok {
			props[key] = v
		}
	}
	return props
}

func (h *Handler) getRoleDefinition(w http.ResponseWriter, scope, name string) {
	if role := roleByID(name); role != nil {
		json.NewEncoder(w).Encode(buildBuiltinRoleDefinition(scope, *role))
		return
	}
	v, ok := h.store.Get(h.roleDefinitionKey(scope, name))
	if !ok {
		azerr.NotFound(w, "Microsoft.Authorization/roleDefinitions", name)
		return
	}
	json.NewEncoder(w).Encode(v)
}

func (h *Handler) listRoleDefinitions(w http.ResponseWriter, r *http.Request, scope string) {
	filterName := roleNameFilter(r.URL.Query().Get("$filter"))
	items := make([]interface{}, 0, len(builtinRoles))
	for _, role := range builtinRoles {
		if filterName == "" || strings.EqualFold(role.Name, filterName) {
			items = append(items, buildBuiltinRoleDefinition(scope, role))
		}
	}
	for _, item := range h.store.ListByPrefix(h.roleDefinitionPrefix(scope)) {
		if filterName == "" || strings.EqualFold(roleNameFromDefinition(item), filterName) {
			items = append(items, item)
		}
	}
	sort.SliceStable(items, func(i, j int) bool {
		return roleNameFromDefinition(items[i]) < roleNameFromDefinition(items[j])
	})
	json.NewEncoder(w).Encode(map[string]interface{}{"value": items})
}

func (h *Handler) deleteRoleDefinition(w http.ResponseWriter, scope, name string) {
	if roleByID(name) != nil {
		azerr.BadRequest(w, "built-in role definitions cannot be deleted")
		return
	}
	if !h.store.Delete(h.roleDefinitionKey(scope, name)) {
		azerr.NotFound(w, "Microsoft.Authorization/roleDefinitions", name)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func buildBuiltinRoleDefinition(scope string, role builtinRole) map[string]interface{} {
	actions := role.Actions
	if actions == nil {
		actions = []interface{}{}
	}
	dataActions := role.DataActions
	if dataActions == nil {
		dataActions = []interface{}{}
	}
	return map[string]interface{}{
		"id":   scope + "/providers/Microsoft.Authorization/roleDefinitions/" + role.ID,
		"name": role.ID,
		"type": "Microsoft.Authorization/roleDefinitions",
		"properties": map[string]interface{}{
			"roleName":    role.Name,
			"type":        "BuiltInRole",
			"description": role.Description,
			"assignableScopes": []interface{}{
				"/",
			},
			"permissions": []interface{}{
				map[string]interface{}{
					"actions":        actions,
					"notActions":     []interface{}{},
					"dataActions":    dataActions,
					"notDataActions": []interface{}{},
				},
			},
			"createdOn": "2015-02-02T21:55:09.8806423Z",
			"updatedOn": "2015-02-02T21:55:09.8806423Z",
		},
	}
}

func roleByID(id string) *builtinRole {
	for i := range builtinRoles {
		if strings.EqualFold(builtinRoles[i].ID, id) {
			return &builtinRoles[i]
		}
	}
	return nil
}

func roleNameFilter(filter string) string {
	const needle = "rolename eq '"
	lower := strings.ToLower(filter)
	idx := strings.Index(lower, needle)
	if idx < 0 {
		return ""
	}
	start := idx + len(needle)
	end := strings.Index(filter[start:], "'")
	if end < 0 {
		return ""
	}
	return filter[start : start+end]
}

func roleNameFromDefinition(v interface{}) string {
	def := mapValue(v)
	props := mapValue(def["properties"])
	return stringValue(props["roleName"])
}

func mapValue(v interface{}) map[string]interface{} {
	m, _ := v.(map[string]interface{})
	return m
}

func stringValue(v interface{}) string {
	s, _ := v.(string)
	return s
}

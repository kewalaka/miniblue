package cosmosdb

import (
	"encoding/json"
	"github.com/moabukar/miniblue/internal/azerr"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/moabukar/miniblue/internal/store"
)

type Handler struct {
	store *store.Store
}

func NewHandler(s *store.Store) *Handler {
	return &Handler{store: s}
}

func (h *Handler) Register(r chi.Router) {
	// ARM-style paths: used by Azure SDKs to enumerate and manage Cosmos DB accounts
	r.Route("/subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/Microsoft.DocumentDB/databaseAccounts", func(r chi.Router) {
		r.Get("/", h.ListAccounts)
		r.Route("/{accountName}", func(r chi.Router) {
			r.Put("/", h.CreateOrUpdateAccount)
			r.Get("/", h.GetAccount)
			r.Delete("/", h.DeleteAccount)
			r.Post("/listKeys", h.ListKeys)
			r.Post("/readonlykeys", h.ListReadOnlyKeys)
			r.Post("/listConnectionStrings", h.ListConnectionStrings)
			r.Post("/regenerateKey", h.RegenerateKey)
			r.Route("/sqlDatabases", func(r chi.Router) {
				r.Get("/", h.ListSQLDatabases)
				r.Route("/{dbName}", func(r chi.Router) {
					r.Put("/", h.CreateOrUpdateSQLDatabase)
					r.Get("/", h.GetSQLDatabase)
					r.Delete("/", h.DeleteSQLDatabase)
					r.Route("/containers", func(r chi.Router) {
						r.Get("/", h.ListContainers)
						r.Route("/{containerName}", func(r chi.Router) {
							r.Put("/", h.CreateOrUpdateContainer)
							r.Get("/", h.GetContainer)
							r.Delete("/", h.DeleteContainer)
						})
					})
				})
			})
			r.Route("/tables", func(r chi.Router) {
				r.Get("/", h.ListTables)
				r.Route("/{tableName}", func(r chi.Router) {
					r.Put("/", h.CreateOrUpdateTable)
					r.Get("/", h.GetTable)
					r.Delete("/", h.DeleteTable)
					r.Route("/throughputSettings/default", func(r chi.Router) {
						r.Put("/", h.CreateOrUpdateTableThroughput)
						r.Get("/", h.GetTableThroughput)
					})
				})
			})
		})
	})

	// Data-plane paths: used for document operations
	r.Route("/cosmosdb/{accountName}/dbs/{dbName}/colls/{collName}/docs", func(r chi.Router) {
		r.Post("/", h.CreateDocument)
		r.Get("/", h.QueryDocuments)
		r.Route("/{docId}", func(r chi.Router) {
			r.Get("/", h.GetDocument)
			r.Put("/", h.ReplaceDocument)
			r.Delete("/", h.DeleteDocument)
		})
	})
}

func (h *Handler) key(account, db, coll, id string) string {
	return "cosmos:" + account + ":" + db + ":" + coll + ":" + id
}

func (h *Handler) CreateDocument(w http.ResponseWriter, r *http.Request) {
	account := chi.URLParam(r, "accountName")
	db := chi.URLParam(r, "dbName")
	coll := chi.URLParam(r, "collName")

	var doc map[string]interface{}
	json.NewDecoder(r.Body).Decode(&doc)

	id, _ := doc["id"].(string)
	if id == "" {
		azerr.BadRequest(w, "Document must contain an id property.")
		return
	}

	k := h.key(account, db, coll, id)
	if h.store.Exists(k) {
		azerr.Conflict(w, "Microsoft.DocumentDB/documents", id)
		return
	}

	h.store.Set(k, doc)
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(doc)
}

func (h *Handler) GetDocument(w http.ResponseWriter, r *http.Request) {
	account := chi.URLParam(r, "accountName")
	db := chi.URLParam(r, "dbName")
	coll := chi.URLParam(r, "collName")
	docId := chi.URLParam(r, "docId")

	v, ok := h.store.Get(h.key(account, db, coll, docId))
	if !ok {
		azerr.NotFound(w, "Microsoft.DocumentDB/documents", docId)
		return
	}
	json.NewEncoder(w).Encode(v)
}

func (h *Handler) ReplaceDocument(w http.ResponseWriter, r *http.Request) {
	account := chi.URLParam(r, "accountName")
	db := chi.URLParam(r, "dbName")
	coll := chi.URLParam(r, "collName")
	docId := chi.URLParam(r, "docId")

	var doc map[string]interface{}
	json.NewDecoder(r.Body).Decode(&doc)
	doc["id"] = docId

	h.store.Set(h.key(account, db, coll, docId), doc)
	json.NewEncoder(w).Encode(doc)
}

func (h *Handler) DeleteDocument(w http.ResponseWriter, r *http.Request) {
	account := chi.URLParam(r, "accountName")
	db := chi.URLParam(r, "dbName")
	coll := chi.URLParam(r, "collName")
	docId := chi.URLParam(r, "docId")

	h.store.Delete(h.key(account, db, coll, docId))
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) QueryDocuments(w http.ResponseWriter, r *http.Request) {
	account := chi.URLParam(r, "accountName")
	db := chi.URLParam(r, "dbName")
	coll := chi.URLParam(r, "collName")
	items := h.store.ListByPrefix("cosmos:" + account + ":" + db + ":" + coll + ":")
	json.NewEncoder(w).Encode(map[string]interface{}{"Documents": items, "_count": len(items)})
}

// --- ARM account handlers ---

func (h *Handler) accountKey(sub, rg, name string) string {
	return "cosmos:account:" + sub + ":" + rg + ":" + name
}

func accountArrayProperty(props map[string]interface{}, key string) interface{} {
	if v, ok := props[key].([]interface{}); ok {
		return v
	}
	return []interface{}{}
}

func accountLocationID(sub, rg, account, locationName string) string {
	return "/subscriptions/" + sub + "/resourceGroups/" + rg + "/providers/Microsoft.DocumentDB/databaseAccounts/" + account + "/locations/" + locationName
}

func accountLocationsProperty(sub, rg, name, defaultLocation string, props map[string]interface{}) []interface{} {
	raw, ok := props["locations"].([]interface{})
	if !ok || len(raw) == 0 {
		raw = []interface{}{
			map[string]interface{}{
				"locationName":     defaultLocation,
				"failoverPriority": float64(0),
				"isZoneRedundant":  false,
			},
		}
	}

	locations := make([]interface{}, 0, len(raw))
	for _, item := range raw {
		loc, ok := item.(map[string]interface{})
		if !ok {
			locations = append(locations, item)
			continue
		}

		copy := make(map[string]interface{}, len(loc)+1)
		for k, v := range loc {
			copy[k] = v
		}
		locationName, _ := copy["locationName"].(string)
		if locationName == "" {
			locationName = defaultLocation
			copy["locationName"] = locationName
		}
		if _, ok := copy["id"]; !ok {
			copy["id"] = accountLocationID(sub, rg, name, locationName)
		}
		if _, ok := copy["isZoneRedundant"]; !ok {
			copy["isZoneRedundant"] = false
		}
		locations = append(locations, copy)
	}
	return locations
}

func accountFailoverPolicies(locations []interface{}) []interface{} {
	policies := make([]interface{}, 0, len(locations))
	for _, item := range locations {
		loc, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		policy := map[string]interface{}{}
		for _, key := range []string{"id", "locationName", "failoverPriority"} {
			if v, ok := loc[key]; ok {
				policy[key] = v
			}
		}
		policies = append(policies, policy)
	}
	return policies
}

func (h *Handler) buildAccountResponse(sub, rg, name string, input map[string]interface{}) map[string]interface{} {
	props, _ := input["properties"].(map[string]interface{})
	location, _ := input["location"].(string)
	if location == "" {
		location = "eastus"
	}
	kind, _ := input["kind"].(string)
	if kind == "" {
		kind = "GlobalDocumentDB"
	}

	locations := accountLocationsProperty(sub, rg, name, location, props)
	responseProps := map[string]interface{}{
		"provisioningState":        "Succeeded",
		"documentEndpoint":         "https://" + name + ".documents.azure.com:443/",
		"databaseAccountOfferType": "Standard",
		"consistencyPolicy": map[string]interface{}{
			"defaultConsistencyLevel": "Session",
		},
		"locations":                   locations,
		"failoverPolicies":            accountFailoverPolicies(locations),
		"capabilities":                accountArrayProperty(props, "capabilities"),
		"ipRules":                     accountArrayProperty(props, "ipRules"),
		"virtualNetworkRules":         accountArrayProperty(props, "virtualNetworkRules"),
		"cors":                        accountArrayProperty(props, "cors"),
		"networkAclBypassResourceIds": accountArrayProperty(props, "networkAclBypassResourceIds"),
	}
	for _, key := range []string{
		"databaseAccountOfferType",
		"consistencyPolicy",
		"publicNetworkAccess",
		"minimalTlsVersion",
		"minimumTlsVersion",
	} {
		if v, ok := props[key]; ok {
			responseProps[key] = v
		}
	}

	acct := map[string]interface{}{
		"id":         "/subscriptions/" + sub + "/resourceGroups/" + rg + "/providers/Microsoft.DocumentDB/databaseAccounts/" + name,
		"name":       name,
		"type":       "Microsoft.DocumentDB/databaseAccounts",
		"location":   location,
		"kind":       kind,
		"properties": responseProps,
	}
	if tags, ok := input["tags"]; ok {
		acct["tags"] = tags
	}
	return acct
}

func (h *Handler) CreateOrUpdateAccount(w http.ResponseWriter, r *http.Request) {
	sub := chi.URLParam(r, "subscriptionId")
	rg := chi.URLParam(r, "resourceGroupName")
	name := chi.URLParam(r, "accountName")

	k := h.accountKey(sub, rg, name)

	input := map[string]interface{}{}
	json.NewDecoder(r.Body).Decode(&input)

	acct := h.buildAccountResponse(sub, rg, name, input)
	h.store.Set(k, acct)

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(acct)
}

func (h *Handler) GetAccount(w http.ResponseWriter, r *http.Request) {
	sub := chi.URLParam(r, "subscriptionId")
	rg := chi.URLParam(r, "resourceGroupName")
	name := chi.URLParam(r, "accountName")

	v, ok := h.store.Get(h.accountKey(sub, rg, name))
	if !ok {
		azerr.NotFound(w, "Microsoft.DocumentDB/databaseAccounts", name)
		return
	}
	json.NewEncoder(w).Encode(v)
}

func (h *Handler) DeleteAccount(w http.ResponseWriter, r *http.Request) {
	sub := chi.URLParam(r, "subscriptionId")
	rg := chi.URLParam(r, "resourceGroupName")
	name := chi.URLParam(r, "accountName")

	if !h.store.Delete(h.accountKey(sub, rg, name)) {
		azerr.NotFound(w, "Microsoft.DocumentDB/databaseAccounts", name)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

func (h *Handler) ListAccounts(w http.ResponseWriter, r *http.Request) {
	sub := chi.URLParam(r, "subscriptionId")
	rg := chi.URLParam(r, "resourceGroupName")
	items := h.store.ListByPrefix("cosmos:account:" + sub + ":" + rg + ":")
	json.NewEncoder(w).Encode(map[string]interface{}{"value": items})
}

func (h *Handler) accountOperationAccountName(w http.ResponseWriter, r *http.Request) (string, bool) {
	sub := chi.URLParam(r, "subscriptionId")
	rg := chi.URLParam(r, "resourceGroupName")
	name := chi.URLParam(r, "accountName")
	if !h.store.Exists(h.accountKey(sub, rg, name)) {
		azerr.NotFound(w, "Microsoft.DocumentDB/databaseAccounts", name)
		return "", false
	}
	return name, true
}

func accountKeysResponse() map[string]interface{} {
	return map[string]interface{}{
		"primaryMasterKey":           "miniblue-primary-master-key",
		"secondaryMasterKey":         "miniblue-secondary-master-key",
		"primaryReadonlyMasterKey":   "miniblue-primary-readonly-master-key",
		"secondaryReadonlyMasterKey": "miniblue-secondary-readonly-master-key",
	}
}

func (h *Handler) ListKeys(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.accountOperationAccountName(w, r); !ok {
		return
	}
	json.NewEncoder(w).Encode(accountKeysResponse())
}

func (h *Handler) ListReadOnlyKeys(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.accountOperationAccountName(w, r); !ok {
		return
	}
	json.NewEncoder(w).Encode(accountKeysResponse())
}

func (h *Handler) ListConnectionStrings(w http.ResponseWriter, r *http.Request) {
	name, ok := h.accountOperationAccountName(w, r)
	if !ok {
		return
	}
	json.NewEncoder(w).Encode(map[string]interface{}{
		"connectionStrings": []map[string]interface{}{
			{
				"connectionString": "AccountEndpoint=https://" + name + ".documents.azure.com:443/;AccountKey=miniblue-primary-master-key;",
				"description":      "Primary SQL Connection String",
			},
		},
	})
}

func (h *Handler) RegenerateKey(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.accountOperationAccountName(w, r); !ok {
		return
	}
	json.NewEncoder(w).Encode(accountKeysResponse())
}

// --- ARM SQL Database handlers ---

func (h *Handler) sqlDbKey(sub, rg, acct, dbName string) string {
	return "cosmos:sqldb:" + sub + ":" + rg + ":" + acct + ":" + dbName
}

func (h *Handler) CreateOrUpdateSQLDatabase(w http.ResponseWriter, r *http.Request) {
	sub := chi.URLParam(r, "subscriptionId")
	rg := chi.URLParam(r, "resourceGroupName")
	acct := chi.URLParam(r, "accountName")
	dbName := chi.URLParam(r, "dbName")

	k := h.sqlDbKey(sub, rg, acct, dbName)

	db := map[string]interface{}{
		"id":   "/subscriptions/" + sub + "/resourceGroups/" + rg + "/providers/Microsoft.DocumentDB/databaseAccounts/" + acct + "/sqlDatabases/" + dbName,
		"name": dbName,
		"type": "Microsoft.DocumentDB/databaseAccounts/sqlDatabases",
		"properties": map[string]interface{}{
			"resource": map[string]interface{}{"id": dbName},
		},
	}
	h.store.Set(k, db)

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(db)
}

func (h *Handler) GetSQLDatabase(w http.ResponseWriter, r *http.Request) {
	sub := chi.URLParam(r, "subscriptionId")
	rg := chi.URLParam(r, "resourceGroupName")
	acct := chi.URLParam(r, "accountName")
	dbName := chi.URLParam(r, "dbName")

	v, ok := h.store.Get(h.sqlDbKey(sub, rg, acct, dbName))
	if !ok {
		azerr.NotFound(w, "Microsoft.DocumentDB/databaseAccounts/sqlDatabases", dbName)
		return
	}
	json.NewEncoder(w).Encode(v)
}

func (h *Handler) DeleteSQLDatabase(w http.ResponseWriter, r *http.Request) {
	sub := chi.URLParam(r, "subscriptionId")
	rg := chi.URLParam(r, "resourceGroupName")
	acct := chi.URLParam(r, "accountName")
	dbName := chi.URLParam(r, "dbName")

	if !h.store.Delete(h.sqlDbKey(sub, rg, acct, dbName)) {
		azerr.NotFound(w, "Microsoft.DocumentDB/databaseAccounts/sqlDatabases", dbName)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

func (h *Handler) ListSQLDatabases(w http.ResponseWriter, r *http.Request) {
	sub := chi.URLParam(r, "subscriptionId")
	rg := chi.URLParam(r, "resourceGroupName")
	acct := chi.URLParam(r, "accountName")
	items := h.store.ListByPrefix("cosmos:sqldb:" + sub + ":" + rg + ":" + acct + ":")
	json.NewEncoder(w).Encode(map[string]interface{}{"value": items})
}

// --- ARM Container handlers ---

func (h *Handler) containerKey(sub, rg, acct, dbName, name string) string {
	return "cosmos:container:" + sub + ":" + rg + ":" + acct + ":" + dbName + ":" + name
}

func (h *Handler) CreateOrUpdateContainer(w http.ResponseWriter, r *http.Request) {
	sub := chi.URLParam(r, "subscriptionId")
	rg := chi.URLParam(r, "resourceGroupName")
	acct := chi.URLParam(r, "accountName")
	dbName := chi.URLParam(r, "dbName")
	name := chi.URLParam(r, "containerName")

	k := h.containerKey(sub, rg, acct, dbName, name)

	c := map[string]interface{}{
		"id":   "/subscriptions/" + sub + "/resourceGroups/" + rg + "/providers/Microsoft.DocumentDB/databaseAccounts/" + acct + "/sqlDatabases/" + dbName + "/containers/" + name,
		"name": name,
		"type": "Microsoft.DocumentDB/databaseAccounts/sqlDatabases/containers",
		"properties": map[string]interface{}{
			"resource": map[string]interface{}{
				"id": name,
				"partitionKey": map[string]interface{}{
					"paths": []string{"/id"},
					"kind":  "Hash",
				},
			},
		},
	}
	h.store.Set(k, c)

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(c)
}

func (h *Handler) GetContainer(w http.ResponseWriter, r *http.Request) {
	sub := chi.URLParam(r, "subscriptionId")
	rg := chi.URLParam(r, "resourceGroupName")
	acct := chi.URLParam(r, "accountName")
	dbName := chi.URLParam(r, "dbName")
	name := chi.URLParam(r, "containerName")

	v, ok := h.store.Get(h.containerKey(sub, rg, acct, dbName, name))
	if !ok {
		azerr.NotFound(w, "Microsoft.DocumentDB/databaseAccounts/sqlDatabases/containers", name)
		return
	}
	json.NewEncoder(w).Encode(v)
}

func (h *Handler) DeleteContainer(w http.ResponseWriter, r *http.Request) {
	sub := chi.URLParam(r, "subscriptionId")
	rg := chi.URLParam(r, "resourceGroupName")
	acct := chi.URLParam(r, "accountName")
	dbName := chi.URLParam(r, "dbName")
	name := chi.URLParam(r, "containerName")

	if !h.store.Delete(h.containerKey(sub, rg, acct, dbName, name)) {
		azerr.NotFound(w, "Microsoft.DocumentDB/databaseAccounts/sqlDatabases/containers", name)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

func (h *Handler) ListContainers(w http.ResponseWriter, r *http.Request) {
	sub := chi.URLParam(r, "subscriptionId")
	rg := chi.URLParam(r, "resourceGroupName")
	acct := chi.URLParam(r, "accountName")
	dbName := chi.URLParam(r, "dbName")
	items := h.store.ListByPrefix("cosmos:container:" + sub + ":" + rg + ":" + acct + ":" + dbName + ":")
	json.NewEncoder(w).Encode(map[string]interface{}{"value": items})
}

// --- ARM Table handlers ---

func (h *Handler) tableKey(sub, rg, acct, name string) string {
	return "cosmos:table:" + sub + ":" + rg + ":" + acct + ":" + name
}

func (h *Handler) tableThroughputKey(sub, rg, acct, name string) string {
	return "cosmos:tablethroughput:" + sub + ":" + rg + ":" + acct + ":" + name
}

func (h *Handler) CreateOrUpdateTable(w http.ResponseWriter, r *http.Request) {
	sub := chi.URLParam(r, "subscriptionId")
	rg := chi.URLParam(r, "resourceGroupName")
	acct := chi.URLParam(r, "accountName")
	name := chi.URLParam(r, "tableName")

	k := h.tableKey(sub, rg, acct, name)

	table := map[string]interface{}{}
	json.NewDecoder(r.Body).Decode(&table)
	table["id"] = "/subscriptions/" + sub + "/resourceGroups/" + rg + "/providers/Microsoft.DocumentDB/databaseAccounts/" + acct + "/tables/" + name
	table["name"] = name
	table["type"] = "Microsoft.DocumentDB/databaseAccounts/tables"
	h.store.Set(k, table)

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(table)
}

func (h *Handler) GetTable(w http.ResponseWriter, r *http.Request) {
	sub := chi.URLParam(r, "subscriptionId")
	rg := chi.URLParam(r, "resourceGroupName")
	acct := chi.URLParam(r, "accountName")
	name := chi.URLParam(r, "tableName")

	v, ok := h.store.Get(h.tableKey(sub, rg, acct, name))
	if !ok {
		azerr.NotFound(w, "Microsoft.DocumentDB/databaseAccounts/tables", name)
		return
	}
	json.NewEncoder(w).Encode(v)
}

func (h *Handler) DeleteTable(w http.ResponseWriter, r *http.Request) {
	sub := chi.URLParam(r, "subscriptionId")
	rg := chi.URLParam(r, "resourceGroupName")
	acct := chi.URLParam(r, "accountName")
	name := chi.URLParam(r, "tableName")

	if !h.store.Delete(h.tableKey(sub, rg, acct, name)) {
		azerr.NotFound(w, "Microsoft.DocumentDB/databaseAccounts/tables", name)
		return
	}
	h.store.Delete(h.tableThroughputKey(sub, rg, acct, name))
	w.WriteHeader(http.StatusAccepted)
}

func (h *Handler) ListTables(w http.ResponseWriter, r *http.Request) {
	sub := chi.URLParam(r, "subscriptionId")
	rg := chi.URLParam(r, "resourceGroupName")
	acct := chi.URLParam(r, "accountName")
	items := h.store.ListByPrefix("cosmos:table:" + sub + ":" + rg + ":" + acct + ":")
	json.NewEncoder(w).Encode(map[string]interface{}{"value": items})
}

func (h *Handler) CreateOrUpdateTableThroughput(w http.ResponseWriter, r *http.Request) {
	sub := chi.URLParam(r, "subscriptionId")
	rg := chi.URLParam(r, "resourceGroupName")
	acct := chi.URLParam(r, "accountName")
	tableName := chi.URLParam(r, "tableName")

	k := h.tableThroughputKey(sub, rg, acct, tableName)

	throughput := map[string]interface{}{}
	json.NewDecoder(r.Body).Decode(&throughput)
	throughput["id"] = "/subscriptions/" + sub + "/resourceGroups/" + rg + "/providers/Microsoft.DocumentDB/databaseAccounts/" + acct + "/tables/" + tableName + "/throughputSettings/default"
	throughput["name"] = "default"
	throughput["type"] = "Microsoft.DocumentDB/databaseAccounts/tables/throughputSettings"
	h.store.Set(k, throughput)

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(throughput)
}

func (h *Handler) GetTableThroughput(w http.ResponseWriter, r *http.Request) {
	sub := chi.URLParam(r, "subscriptionId")
	rg := chi.URLParam(r, "resourceGroupName")
	acct := chi.URLParam(r, "accountName")
	tableName := chi.URLParam(r, "tableName")

	v, ok := h.store.Get(h.tableThroughputKey(sub, rg, acct, tableName))
	if !ok {
		azerr.NotFound(w, "Microsoft.DocumentDB/databaseAccounts/tables/throughputSettings", "default")
		return
	}
	json.NewEncoder(w).Encode(v)
}

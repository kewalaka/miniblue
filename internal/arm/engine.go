package arm

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/moabukar/miniblue/internal/azerr"
)

// Engine serves registered resource types with generic ARM CRUD: PUT (201 on
// create, 200 on update), GET, DELETE (200 if it existed, 204 if not), list
// at the collection path and, for types that enable it, PATCH of tags.
// The stored body is whatever the client sent plus the server-owned id, name
// and type.
type Engine struct {
	reg   *Registry
	store *Store
}

// NewEngine returns an Engine serving the types in reg from s.
func NewEngine(reg *Registry, s *Store) *Engine { return &Engine{reg: reg, store: s} }

// Middleware serves requests whose path is a registered type at an allowed
// scope and passes everything else to next, so it coexists with handlers
// that have not migrated to the engine yet. Register it before them.
func (e *Engine) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, err := ParseID(r.URL.Path)
		if err == nil {
			if t, ok := e.reg.lookup(id); ok && t.Scopes.Has(id.ScopeKind()) {
				e.serve(w, r, t, id)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (e *Engine) serve(w http.ResponseWriter, r *http.Request, t *Type, id ID) {
	if v := r.URL.Query().Get("api-version"); v != "" && !t.supports(v) {
		azerr.WriteError(w, http.StatusBadRequest, "InvalidApiVersionParameter",
			fmt.Sprintf("The api-version '%s' is invalid. The supported versions are '%s'.", v, strings.Join(t.APIVersions, ",")))
		return
	}
	switch {
	case id.IsCollection() && r.Method == http.MethodGet:
		writeJSON(w, http.StatusOK, map[string]interface{}{"value": e.store.List(id)})
	case id.IsCollection():
		azerr.MethodNotAllowed(w, r.Method)
	case r.Method == http.MethodGet:
		e.get(w, id)
	case r.Method == http.MethodPut:
		e.put(w, r, t, id)
	case r.Method == http.MethodDelete:
		e.delete(w, t, id)
	case r.Method == http.MethodPatch && t.PatchTags:
		e.patchTags(w, r, id)
	default:
		azerr.MethodNotAllowed(w, r.Method)
	}
}

func (e *Engine) get(w http.ResponseWriter, id ID) {
	doc, ok := e.store.Get(id)
	if !ok {
		notFound(w, id)
		return
	}
	writeJSON(w, http.StatusOK, doc)
}

func (e *Engine) put(w http.ResponseWriter, r *http.Request, t *Type, id ID) {
	body, ok := readObject(w, r)
	if !ok {
		return
	}
	if n, _ := body["name"].(string); n != "" && !strings.EqualFold(n, id.Name()) {
		azerr.BadRequest(w, fmt.Sprintf("The resource name '%s' in the body does not match '%s' in the URL.", n, id.Name()))
		return
	}
	if s, _ := body["id"].(string); s != "" && !strings.EqualFold(s, id.String()) {
		azerr.BadRequest(w, fmt.Sprintf("The resource id '%s' in the body does not match the URL.", s))
		return
	}
	if t.Hooks.Validate != nil {
		if err := t.Hooks.Validate(id, body); err != nil {
			azerr.BadRequest(w, err.Error())
			return
		}
	}
	if t.Hooks.Defaults != nil {
		t.Hooks.Defaults(id, body)
	}

	// The id and name keep the casing of the PUT that created the resource,
	// so responses do not change with the casing of later requests.
	body["id"], body["name"] = id.WithTypeName(t.Name).String(), id.Name()
	if cur, ok := e.store.Get(id); ok {
		body["id"], body["name"] = cur["id"], cur["name"]
	}
	body["type"] = t.Name

	status := http.StatusOK
	if e.store.Put(id, body) {
		status = http.StatusCreated
	}
	writeJSON(w, status, body)
}

func (e *Engine) delete(w http.ResponseWriter, t *Type, id ID) {
	var existed bool
	if len(t.Children) > 0 {
		existed = e.store.DeleteTree(id)
	} else {
		existed = e.store.Delete(id)
	}
	if existed {
		w.WriteHeader(http.StatusOK)
	} else {
		w.WriteHeader(http.StatusNoContent)
	}
}

// patchTags replaces the tags of an existing resource. Other properties in
// the PATCH body are ignored.
func (e *Engine) patchTags(w http.ResponseWriter, r *http.Request, id ID) {
	body, ok := readObject(w, r)
	if !ok {
		return
	}
	tags, has := body["tags"]
	if _, isMap := tags.(map[string]interface{}); !has || (tags != nil && !isMap) {
		azerr.BadRequest(w, "The PATCH body must contain a 'tags' object.")
		return
	}
	doc, found := e.store.Get(id)
	if !found {
		notFound(w, id)
		return
	}
	if tags == nil {
		delete(doc, "tags")
	} else {
		doc["tags"] = tags
	}
	e.store.Put(id, doc)
	writeJSON(w, http.StatusOK, doc)
}

func readObject(w http.ResponseWriter, r *http.Request) (map[string]interface{}, bool) {
	body, err := decodeObject(r.Body)
	if err != nil || body == nil {
		azerr.BadRequest(w, "The request content is not a valid JSON object.")
		return nil, false
	}
	return body, true
}

func notFound(w http.ResponseWriter, id ID) {
	azerr.WriteError(w, http.StatusNotFound, "ResourceNotFound",
		fmt.Sprintf("The resource '%s' could not be found.", id.String()))
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

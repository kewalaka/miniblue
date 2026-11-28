package arm

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/moabukar/miniblue/internal/azerr"
)

// Op identifies the ARM operation performed by Engine.Do.
type Op uint8

const (
	OpGet Op = iota
	OpList
	OpPut
	OpPatch
	OpDelete
	OpAction
)

// Request is the transport-independent input and working state for Engine.Do.
type Request struct {
	Op         Op
	ID         ID // instance id; for OpList the parent scope
	Type       *Type
	APIVersion string
	Action     string // OpAction only
	Query      url.Values
	Principal  Principal
	Body       map[string]any // decoded request (UseNumber)
	Current    map[string]any // stored doc; nil on create
	Doc        map[string]any // proposed doc; hooks mutate this
	Store      Reader

	ifNoneMatch bool
}

// Option configures an Engine.
type Option func(*Engine)

// WithScopeExists sets the callback used to verify that a resource's scope
// (such as a resource group) exists before writing a resource in it. When nil,
// every scope is assumed to exist.
func WithScopeExists(f func(ctx context.Context, scope ID) (bool, error)) Option {
	return func(e *Engine) { e.scopeExists = f }
}

// WithAuthorizer sets the Authorizer checked before every Engine.Do operation.
// A nil Authorizer allows all requests.
func WithAuthorizer(a Authorizer) Option {
	return func(e *Engine) { e.authorizer = a }
}

// WithAdmitter sets the Admitter checked on PUT and PATCH proposed documents.
// A nil Admitter allows all requests.
func WithAdmitter(a Admitter) Option {
	return func(e *Engine) { e.admitter = a }
}

// Engine serves registered resource types with generic ARM CRUD: PUT (201 on
// create, 200 on update), GET, DELETE (200 if it existed, 204 if not), list
// at the collection path and PATCH according to Type.Patch (default RFC 7386
// JSON Merge Patch).
// The stored body is whatever the client sent plus the server-owned id, name
// and type.
type Engine struct {
	reg         *Registry
	store       *Store
	scopeExists func(ctx context.Context, scope ID) (bool, error)
	authorizer  Authorizer
	admitter    Admitter
}

// NewEngine returns an Engine serving the types in reg from s.
func NewEngine(reg *Registry, s *Store, opts ...Option) *Engine {
	e := &Engine{reg: reg, store: s}
	for _, opt := range opts {
		if opt != nil {
			opt(e)
		}
	}
	return e
}

// DeleteScope removes every engine resource stored beneath scope.
func (e *Engine) DeleteScope(scope ID) error {
	return e.store.DeleteScope(scope)
}

// Resolve routes an ARM request and validates api-version.
// ok=false means the path is not owned by any registered type at its scope.
func (e *Engine) Resolve(method, path string, q url.Values) (*Request, bool, error) {
	id, err := ParseID(path)
	if err != nil {
		return nil, false, nil
	}
	t, ok := e.reg.Lookup(id)
	if !ok {
		return nil, false, nil
	}
	subWideList := id.IsCollection() && id.ScopeKind() == ScopeSubscription && len(id.Types) == 1 && t.Scopes.Has(ScopeResourceGroup)
	if !t.Scopes.Has(id.ScopeKind()) && !subWideList {
		return nil, false, nil
	}
	apiVersion := q.Get("api-version")
	if apiVersion != "" && !t.supports(apiVersion) {
		return nil, true, azerr.New(http.StatusBadRequest, "InvalidApiVersionParameter",
			"The api-version '%s' is invalid. The supported versions are '%s'.",
			apiVersion, strings.Join(t.APIVersions, ","))
	}
	req := &Request{
		ID:         id,
		Type:       t,
		APIVersion: apiVersion,
		Query:      q,
		Store:      e.store,
	}
	if id.IsCollection() {
		if method != http.MethodGet {
			return nil, true, azerr.New(http.StatusMethodNotAllowed, "MethodNotAllowed",
				"The request method '%s' is not allowed.", method)
		}
		req.Op = OpList
		req.ID = id.Parent()
		return req, true, nil
	}
	switch method {
	case http.MethodGet:
		req.Op = OpGet
	case http.MethodPut:
		req.Op = OpPut
	case http.MethodPatch:
		if t.Patch == PatchNone {
			return nil, true, azerr.New(http.StatusMethodNotAllowed, "MethodNotAllowed",
				"The request method '%s' is not allowed.", method)
		}
		req.Op = OpPatch
	case http.MethodDelete:
		req.Op = OpDelete
	default:
		return nil, true, azerr.New(http.StatusMethodNotAllowed, "MethodNotAllowed",
			"The request method '%s' is not allowed.", method)
	}
	return req, true, nil
}

// Do executes a resolved ARM request and returns the HTTP status, response
// payload (if any), and error. Errors from the engine or hooks are *azerr.Error;
// store errors are returned unwrapped so HTTP adapters render 500.
func (e *Engine) Do(ctx context.Context, r *Request) (int, any, error) {
	if r.Store == nil {
		r.Store = e.store
	}
	if r.Type == nil {
		t, ok := e.reg.Lookup(r.ID)
		if !ok {
			return 0, nil, azerr.New(http.StatusNotFound, "InvalidResourceType",
				"The resource type '%s' could not be found.", r.ID.TypeName())
		}
		r.Type = t
	}
	if r.Principal == (Principal{}) {
		r.Principal = PrincipalFrom(ctx)
	}
	action, ok := actionFor(r)
	if !ok {
		return 0, nil, azerr.New(http.StatusMethodNotAllowed, "MethodNotAllowed",
			"The requested operation is not allowed.")
	}
	if e.authorizer != nil {
		if err := e.authorizer.Authorize(ctx, r.Principal, action, r.ID); err != nil {
			return 0, nil, err
		}
	}
	switch r.Op {
	case OpGet:
		return e.doGet(r)
	case OpList:
		return e.doList(r)
	case OpPut:
		return e.doPut(ctx, r)
	case OpPatch:
		return e.doPatch(ctx, r)
	case OpDelete:
		return e.doDelete(r)
	default:
		return 0, nil, azerr.New(http.StatusMethodNotAllowed, "MethodNotAllowed",
			"The requested operation is not allowed.")
	}
}

func actionFor(r *Request) (string, bool) {
	switch r.Op {
	case OpGet, OpList:
		return r.Type.Name + "/read", true
	case OpPut, OpPatch:
		return r.Type.Name + "/write", true
	case OpDelete:
		return r.Type.Name + "/delete", true
	case OpAction:
		if r.Action == "" {
			return "", false
		}
		return r.Type.Name + "/" + r.Action + "/action", true
	default:
		return "", false
	}
}

// ServeHTTP adapts HTTP requests to Resolve and Do.
func (e *Engine) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	req, ok, err := e.Resolve(r.Method, r.URL.Path, r.URL.Query())
	if !ok {
		azerr.Write(w, azerr.New(http.StatusNotFound, "ResourceNotFound",
			"The resource '%s' could not be found.", r.URL.Path))
		return
	}
	e.serveResolved(w, r, req, err)
}

// Middleware serves requests whose path is a registered type at an allowed
// scope and passes everything else to next, so it coexists with handlers
// that have not migrated to the engine yet. Register it before them.
func (e *Engine) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req, ok, err := e.Resolve(r.Method, r.URL.Path, r.URL.Query())
		if !ok {
			next.ServeHTTP(w, r)
			return
		}
		e.serveResolved(w, r, req, err)
	})
}

func (e *Engine) serveResolved(w http.ResponseWriter, r *http.Request, req *Request, err error) {
	if err != nil {
		azerr.Write(w, err)
		return
	}
	if req.Op == OpPut || req.Op == OpPatch {
		body, err := decodeObject(r.Body)
		if err != nil || body == nil {
			azerr.Write(w, azerr.New(http.StatusBadRequest, "InvalidRequestContent",
				"The request content is not a valid JSON object."))
			return
		}
		req.Body = body
	}
	if strings.TrimSpace(r.Header.Get("If-None-Match")) == "*" {
		req.ifNoneMatch = true
	}
	status, body, err := e.Do(r.Context(), req)
	if err != nil {
		azerr.Write(w, err)
		return
	}
	if body == nil {
		w.WriteHeader(status)
		return
	}
	writeJSON(w, status, body)
}

func (e *Engine) doGet(r *Request) (int, any, error) {
	doc, ok, err := e.store.Get(r.ID)
	if err != nil {
		return 0, nil, err
	}
	if !ok {
		return 0, nil, notFoundErr(r.ID)
	}
	return http.StatusOK, doc, nil
}

func (e *Engine) doList(r *Request) (int, any, error) {
	var (
		items []map[string]any
		err   error
	)
	if r.ID.is(nsResources, typeSubscriptions) && !r.Type.Scopes.Has(ScopeSubscription) && r.Type.Scopes.Has(ScopeResourceGroup) {
		items, err = e.store.Under(r.ID, r.Type.Name)
	} else {
		items, err = e.store.Children(r.ID, r.Type.Name)
	}
	if err != nil {
		return 0, nil, err
	}
	return http.StatusOK, map[string]any{"value": items}, nil
}

func (e *Engine) doPut(ctx context.Context, r *Request) (int, any, error) {
	if r.Body == nil {
		return 0, nil, azerr.New(http.StatusBadRequest, "InvalidRequestContent",
			"The request content is not a valid JSON object.")
	}
	if err := checkBodyIdentity(r); err != nil {
		return 0, nil, err
	}

	// The id and name keep the casing of the PUT that created the resource,
	// so responses do not change with the casing of later requests. Deciding
	// that, running Validate/Defaults against Current, and persisting happen
	// in one Update so concurrent PUTs cannot race.
	canonID, canonName := r.ID.WithTypeName(r.Type.Name).String(), r.ID.Name()
	doc, created, err := e.store.Update(r.ID, func(cur map[string]any) (map[string]any, error) {
		r.Current = cur
		if r.ifNoneMatch && cur != nil {
			return nil, azerr.New(http.StatusPreconditionFailed, "PreconditionFailed",
				"The resource '%s' already exists.", r.ID.String())
		}
		if e.scopeExists != nil && r.ID.Scope != nil {
			switch kind := r.ID.ScopeKind(); kind {
			case ScopeResourceGroup, ScopeSubscription:
				exists, err := e.scopeExists(ctx, *r.ID.Scope)
				if err != nil {
					return nil, wrapStoreErr(err)
				}
				if !exists {
					if kind == ScopeResourceGroup {
						return nil, azerr.New(http.StatusNotFound, "ResourceGroupNotFound",
							"Resource group '%s' could not be found.", r.ID.Scope.Name())
					}
					return nil, azerr.New(http.StatusNotFound, "SubscriptionNotFound",
						"The subscription '%s' could not be found.", r.ID.Scope.Name())
				}
			}
		}
		if len(r.ID.Types) > 1 {
			_, parentOK, err := r.Store.Get(r.ID.Parent())
			if err != nil {
				return nil, err
			}
			if !parentOK {
				return nil, azerr.New(http.StatusNotFound, "ParentResourceNotFound",
					"Can not perform requested operation on nested resource. Parent resource '%s' not found.",
					r.ID.Parent().Name())
			}
		}
		r.Doc = cloneMap(r.Body)
		r.Doc["id"], r.Doc["name"] = canonID, canonName
		if cur != nil {
			r.Doc["id"], r.Doc["name"] = cur["id"], cur["name"]
		}
		r.Doc["type"] = r.Type.Name

		if e.admitter != nil {
			if err := e.admitter.Admit(ctx, r); err != nil {
				return nil, err
			}
		}
		if r.Type.Hooks.Validate != nil {
			if err := r.Type.Hooks.Validate(ctx, r); err != nil {
				return nil, normalizeValidateErr(err)
			}
		}
		if r.Type.Hooks.Defaults != nil {
			if err := r.Type.Hooks.Defaults(ctx, r); err != nil {
				return nil, err
			}
		}
		return r.Doc, nil
	})
	if err != nil {
		return 0, nil, err
	}
	if r.Type.Hooks.After != nil {
		r.Type.Hooks.After(ctx, r)
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	return status, doc, nil
}

func (e *Engine) doDelete(r *Request) (int, any, error) {
	existed, err := e.store.DeleteTree(r.ID)
	if err != nil {
		return 0, nil, err
	}
	if existed {
		return http.StatusOK, nil, nil
	}
	return http.StatusNoContent, nil, nil
}

// doPatch updates an existing resource according to r.Type.Patch: PatchMerge
// applies an RFC 7386 JSON Merge Patch, PatchTags replaces only tags, and
// PatchNone returns 405. The read, hooks, and write run in one Update, so a
// concurrent DELETE or PUT cannot be lost or undone by the PATCH.
func (e *Engine) doPatch(ctx context.Context, r *Request) (int, any, error) {
	switch r.Type.Patch {
	case PatchMerge, PatchTags:
	default:
		return 0, nil, azerr.New(http.StatusMethodNotAllowed, "MethodNotAllowed",
			"The request method 'PATCH' is not allowed.")
	}
	if r.Body == nil {
		return 0, nil, azerr.New(http.StatusBadRequest, "InvalidRequestContent",
			"The request content is not a valid JSON object.")
	}

	var tags any
	switch r.Type.Patch {
	case PatchMerge:
		if err := checkBodyIdentity(r); err != nil {
			return 0, nil, err
		}
	case PatchTags:
		var has bool
		tags, has = r.Body["tags"]
		if _, isMap := tags.(map[string]any); !has || (tags != nil && !isMap) {
			return 0, nil, azerr.New(http.StatusBadRequest, "InvalidRequestContent",
				"The PATCH body must contain a 'tags' object.")
		}
	}

	doc, _, err := e.store.Update(r.ID, func(cur map[string]any) (map[string]any, error) {
		if cur == nil {
			return nil, notFoundErr(r.ID)
		}
		r.Current = cur
		switch r.Type.Patch {
		case PatchMerge:
			r.Doc = mergePatch(cloneMap(cur), r.Body)
			r.Doc["id"], r.Doc["name"] = cur["id"], cur["name"]
			r.Doc["type"] = r.Type.Name
		case PatchTags:
			r.Doc = cloneMap(cur)
			if tags == nil {
				delete(r.Doc, "tags")
			} else {
				r.Doc["tags"] = cloneValue(tags)
			}
		}
		if e.admitter != nil {
			if err := e.admitter.Admit(ctx, r); err != nil {
				return nil, err
			}
		}
		if r.Type.Hooks.Validate != nil {
			if err := r.Type.Hooks.Validate(ctx, r); err != nil {
				return nil, normalizeValidateErr(err)
			}
		}
		if r.Type.Hooks.Defaults != nil {
			if err := r.Type.Hooks.Defaults(ctx, r); err != nil {
				return nil, err
			}
		}
		return r.Doc, nil
	})
	if err != nil {
		return 0, nil, err
	}
	if r.Type.Hooks.After != nil {
		r.Type.Hooks.After(ctx, r)
	}
	return http.StatusOK, doc, nil
}

func checkBodyIdentity(r *Request) error {
	if n, _ := r.Body["name"].(string); n != "" && !strings.EqualFold(n, r.ID.Name()) {
		return azerr.New(http.StatusBadRequest, "InvalidRequestContent",
			"The resource name '%s' in the body does not match '%s' in the URL.", n, r.ID.Name())
	}
	if s, _ := r.Body["id"].(string); s != "" && !strings.EqualFold(s, r.ID.String()) {
		return azerr.New(http.StatusBadRequest, "InvalidRequestContent",
			"The resource id '%s' in the body does not match the URL.", s)
	}
	return nil
}

// mergePatch applies an RFC 7386 JSON Merge Patch to target in place and
// returns it. Callers must pass a fresh target map; inserted values are cloned
// so the result shares no mutable state with patch.
func mergePatch(target, patch map[string]any) map[string]any {
	for k, v := range patch {
		if v == nil {
			delete(target, k)
			continue
		}
		if pm, ok := v.(map[string]any); ok {
			tm, ok := target[k].(map[string]any)
			if !ok {
				tm = make(map[string]any, len(pm))
			}
			target[k] = mergePatch(tm, pm)
			continue
		}
		target[k] = cloneValue(v)
	}
	return target
}

func normalizeValidateErr(err error) error {
	var se *storeError
	if errors.As(err, &se) {
		return err
	}
	var ae *azerr.Error
	if errors.As(err, &ae) && ae != nil {
		return err
	}
	return azerr.New(http.StatusBadRequest, "InvalidRequestContent", "%s", err.Error())
}

func notFoundErr(id ID) *azerr.Error {
	if id.ScopeKind() == ScopeResourceGroup && id.Scope != nil {
		// (verify) exact ARM ResourceNotFound wording at RG scope
		return azerr.New(http.StatusNotFound, "ResourceNotFound",
			"The Resource '%s/%s' under resource group '%s' was not found.",
			id.TypeName(), id.Name(), id.Scope.Name())
	}
	return azerr.New(http.StatusNotFound, "ResourceNotFound",
		"The resource '%s' could not be found.", id.String())
}

func cloneMap(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = cloneValue(v)
	}
	return out
}

func cloneValue(v any) any {
	switch x := v.(type) {
	case map[string]any:
		return cloneMap(x)
	case []any:
		out := make([]any, len(x))
		for i, elem := range x {
			out[i] = cloneValue(elem)
		}
		return out
	default:
		return v
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

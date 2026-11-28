package arm

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/moabukar/miniblue/internal/azerr"
	"github.com/moabukar/miniblue/internal/store"
)

type authorizerFunc func(ctx context.Context, p Principal, action string, scope ID) error

func (f authorizerFunc) Authorize(ctx context.Context, p Principal, action string, scope ID) error {
	return f(ctx, p, action, scope)
}

type admitterFunc func(ctx context.Context, r *Request) error

func (f admitterFunc) Admit(ctx context.Context, r *Request) error {
	return f(ctx, r)
}

func TestPrincipalContext(t *testing.T) {
	if got := PrincipalFrom(nil); got != (Principal{}) {
		t.Fatalf("PrincipalFrom(nil) = %+v, want zero", got)
	}
	if got := PrincipalFrom(context.Background()); got != (Principal{}) {
		t.Fatalf("PrincipalFrom(empty) = %+v, want zero", got)
	}

	p1 := Principal{ObjectID: "oid-1", TenantID: "tid-1", AppID: "app-1"}
	ctx1 := WithPrincipal(context.Background(), p1)
	if got := PrincipalFrom(ctx1); got != p1 {
		t.Fatalf("PrincipalFrom(ctx1) = %+v, want %+v", got, p1)
	}

	p2 := Principal{ObjectID: "oid-2", TenantID: "tid-2", AppID: "app-2"}
	ctx2 := WithPrincipal(ctx1, p2)
	if got := PrincipalFrom(ctx2); got != p2 {
		t.Fatalf("PrincipalFrom(ctx2) = %+v, want %+v", got, p2)
	}
	if got := PrincipalFrom(ctx1); got != p1 {
		t.Fatalf("parent ctx mutated: %+v, want %+v", got, p1)
	}
}

func TestEngineNilAuthorizerAndAdmitterAllow(t *testing.T) {
	reg := NewRegistry()
	reg.Register(Type{
		Name:        "Microsoft.Test/widgets",
		APIVersions: []string{"2024-01-01"},
		Scopes:      ScopeSubscription,
		Patch:       PatchTags,
	})
	e := NewEngine(reg, NewStore(store.New()), WithAuthorizer(nil), WithAdmitter(nil))
	path := subScope + "/providers/Microsoft.Test/widgets/w1"

	want(t, do(e, http.MethodPut, path+apiV, `{"properties":{}}`), http.StatusCreated)
	want(t, do(e, http.MethodGet, path+apiV, ""), http.StatusOK)
	want(t, do(e, http.MethodGet, subScope+"/providers/Microsoft.Test/widgets"+apiV, ""), http.StatusOK)
	want(t, do(e, http.MethodPatch, path+apiV, `{"tags":{"env":"dev"}}`), http.StatusOK)
	want(t, do(e, http.MethodDelete, path+apiV, ""), http.StatusOK)
}

func TestEngineAuthorizerActionsScopesAndPrincipal(t *testing.T) {
	type call struct {
		principal Principal
		action    string
		scope     string
	}
	var calls []call

	reg := NewRegistry()
	reg.Register(Type{
		Name:        "Microsoft.Test/widgets",
		APIVersions: []string{"2024-01-01"},
		Scopes:      ScopeSubscription,
		Patch:       PatchTags,
	})
	e := NewEngine(reg, NewStore(store.New()), WithAuthorizer(authorizerFunc(func(_ context.Context, p Principal, action string, scope ID) error {
		calls = append(calls, call{principal: p, action: action, scope: scope.String()})
		return nil
	})))

	p := Principal{ObjectID: "user-oid", TenantID: "tenant-1", AppID: "app-1"}
	path := subScope + "/providers/Microsoft.Test/widgets/w1"
	listPath := subScope + "/providers/Microsoft.Test/widgets"

	send := func(method, target, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, target+apiV, strings.NewReader(body))
		req = req.WithContext(WithPrincipal(req.Context(), p))
		w := httptest.NewRecorder()
		e.ServeHTTP(w, req)
		return w
	}

	want(t, send(http.MethodPut, path, `{"properties":{}}`), http.StatusCreated)
	want(t, send(http.MethodGet, path, ""), http.StatusOK)
	want(t, send(http.MethodGet, listPath, ""), http.StatusOK)
	want(t, send(http.MethodPatch, path, `{"tags":{"a":"1"}}`), http.StatusOK)
	want(t, send(http.MethodDelete, path, ""), http.StatusOK)

	wantCalls := []call{
		{p, "Microsoft.Test/widgets/write", path},
		{p, "Microsoft.Test/widgets/read", path},
		{p, "Microsoft.Test/widgets/read", subScope},
		{p, "Microsoft.Test/widgets/write", path},
		{p, "Microsoft.Test/widgets/delete", path},
	}
	if len(calls) != len(wantCalls) {
		t.Fatalf("got %d authorize calls, want %d: %+v", len(calls), len(wantCalls), calls)
	}
	for i, wc := range wantCalls {
		if calls[i] != wc {
			t.Errorf("call[%d] = %+v, want %+v", i, calls[i], wc)
		}
	}

	// Explicit Request.Principal is preserved when ctx carries none.
	explicit := Principal{ObjectID: "explicit-oid"}
	req, ok, err := e.Resolve(http.MethodGet, listPath, url.Values{"api-version": {"2024-01-01"}})
	if !ok || err != nil {
		t.Fatalf("Resolve: ok=%v err=%v", ok, err)
	}
	req.Principal = explicit
	if status, _, err := e.Do(context.Background(), req); err != nil || status != http.StatusOK {
		t.Fatalf("Do: status=%d err=%v", status, err)
	}
	if got := calls[len(calls)-1].principal; got != explicit {
		t.Fatalf("explicit principal = %+v, want %+v", got, explicit)
	}
}

func TestEngineAuthorizerDeniesAndFailsClosed(t *testing.T) {
	admitCalled := false
	validateCalled := false
	reg := NewRegistry()
	reg.Register(Type{
		Name:        "Microsoft.Test/widgets",
		APIVersions: []string{"2024-01-01"},
		Scopes:      ScopeSubscription,
		Patch:       PatchTags,
		Hooks: Hooks{
			Validate: func(_ context.Context, _ *Request) error {
				validateCalled = true
				return nil
			},
		},
	})

	var authErr error
	st := NewStore(store.New())
	e := NewEngine(reg, st,
		WithAuthorizer(authorizerFunc(func(_ context.Context, _ Principal, _ string, _ ID) error {
			return authErr
		})),
		WithAdmitter(admitterFunc(func(_ context.Context, _ *Request) error {
			admitCalled = true
			return nil
		})),
	)

	path := subScope + "/providers/Microsoft.Test/widgets/w1"
	// Seed a resource while allowed.
	want(t, do(e, http.MethodPut, path+apiV, `{}`), http.StatusCreated)
	admitCalled, validateCalled = false, false

	// 403 AuthorizationFailed stops all operations before phase 5/7/8/10.
	authErr = azerr.New(http.StatusForbidden, "AuthorizationFailed", "denied")
	for _, tc := range []struct {
		name, method, target, body string
	}{
		{"get-missing", http.MethodGet, subScope + "/providers/Microsoft.Test/widgets/missing" + apiV, ""},
		{"list", http.MethodGet, subScope + "/providers/Microsoft.Test/widgets" + apiV, ""},
		{"put", http.MethodPut, path + apiV, `{"properties":{"changed":true}}`},
		{"patch", http.MethodPatch, path + apiV, `{"tags":{"k":"v"}}`},
		{"delete", http.MethodDelete, path + apiV, ""},
	} {
		t.Run("deny-"+tc.name, func(t *testing.T) {
			w := do(e, tc.method, tc.target, tc.body)
			want(t, w, http.StatusForbidden)
			if code := decode(t, w)["error"].(map[string]any)["code"]; code != "AuthorizationFailed" {
				t.Fatalf("code = %v, want AuthorizationFailed", code)
			}
		})
	}
	if admitCalled || validateCalled {
		t.Fatalf("admitCalled=%v validateCalled=%v, want both false", admitCalled, validateCalled)
	}

	// Non-azerr error fails closed as 500 InternalServerError.
	authErr = errors.New("backend read failed")
	w := do(e, http.MethodGet, path+apiV, "")
	want(t, w, http.StatusInternalServerError)
	if code := decode(t, w)["error"].(map[string]any)["code"]; code != "InternalServerError" {
		t.Fatalf("code = %v, want InternalServerError", code)
	}

	// Seeded resource was not deleted or modified while denied.
	authErr = nil
	w = do(e, http.MethodGet, path+apiV, "")
	want(t, w, http.StatusOK)
	if _, hasProps := decode(t, w)["properties"]; hasProps {
		t.Fatalf("denied PUT modified stored resource")
	}
}

func TestEngineAdmitterEnforcement(t *testing.T) {
	var order []string
	var admitErr error
	var seenCurrent bool
	var seenDocType string
	var seenPrincipal Principal

	reg := NewRegistry()
	reg.Register(Type{
		Name:        "Microsoft.Test/widgets",
		APIVersions: []string{"2024-01-01"},
		Scopes:      ScopeSubscription,
		Patch:       PatchTags,
		Hooks: Hooks{
			Validate: func(_ context.Context, _ *Request) error {
				order = append(order, "validate")
				return nil
			},
			Defaults: func(_ context.Context, _ *Request) error {
				order = append(order, "defaults")
				return nil
			},
			After: func(_ context.Context, _ *Request) {
				order = append(order, "after")
			},
		},
	})

	e := NewEngine(reg, NewStore(store.New()), WithAdmitter(admitterFunc(func(_ context.Context, r *Request) error {
		order = append(order, "admit")
		// Lock-free store read inside Admit must not deadlock.
		if _, _, err := r.Store.Get(r.ID); err != nil {
			return err
		}
		seenCurrent = r.Current != nil
		seenDocType, _ = r.Doc["type"].(string)
		seenPrincipal = r.Principal
		return admitErr
	})))

	path := subScope + "/providers/Microsoft.Test/widgets/w1"
	p := Principal{ObjectID: "caller-oid", TenantID: "t1"}
	send := func(method, target, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, target+apiV, strings.NewReader(body))
		req = req.WithContext(WithPrincipal(req.Context(), p))
		w := httptest.NewRecorder()
		e.ServeHTTP(w, req)
		return w
	}

	// Create: Admit runs after shape and before Validate/Defaults/After.
	want(t, send(http.MethodPut, path, `{"tags":{"a":"1"}}`), http.StatusCreated)
	if strings.Join(order, ",") != "admit,validate,defaults,after" {
		t.Fatalf("phase order = %v, want [admit validate defaults after]", order)
	}
	if seenCurrent {
		t.Fatal("Admit saw non-nil Current on create")
	}
	if seenDocType != "Microsoft.Test/widgets" {
		t.Fatalf("Admit saw type = %q, want Microsoft.Test/widgets", seenDocType)
	}
	if seenPrincipal != p {
		t.Fatalf("Admit saw principal = %+v, want %+v", seenPrincipal, p)
	}

	// GET and LIST do not invoke Admitter.
	order = nil
	want(t, send(http.MethodGet, path, ""), http.StatusOK)
	want(t, send(http.MethodGet, subScope+"/providers/Microsoft.Test/widgets", ""), http.StatusOK)
	if len(order) != 0 {
		t.Fatalf("GET/LIST invoked hooks/admitter: %v", order)
	}

	// PATCH denial (403 RequestDisallowedByPolicy): runs before Validate and does not persist.
	order = nil
	admitErr = azerr.New(http.StatusForbidden, "RequestDisallowedByPolicy", "disallowed")
	w := send(http.MethodPatch, path, `{"tags":{"a":"blocked"}}`)
	want(t, w, http.StatusForbidden)
	if code := decode(t, w)["error"].(map[string]any)["code"]; code != "RequestDisallowedByPolicy" {
		t.Fatalf("code = %v, want RequestDisallowedByPolicy", code)
	}
	if !seenCurrent {
		t.Fatal("Admit saw nil Current on PATCH")
	}
	if strings.Join(order, ",") != "admit" {
		t.Fatalf("denied PATCH ran later phases: %v", order)
	}

	// Non-azerr error from Admitter fails closed as 500 (never downgraded to 400).
	order = nil
	admitErr = errors.New("policy store read failed")
	w = send(http.MethodPut, path, `{"tags":{"a":"err"}}`)
	want(t, w, http.StatusInternalServerError)
	if code := decode(t, w)["error"].(map[string]any)["code"]; code != "InternalServerError" {
		t.Fatalf("code = %v, want InternalServerError", code)
	}

	// Stored tags remain untouched after denied PATCH and failed PUT.
	admitErr = nil
	w = send(http.MethodGet, path, "")
	want(t, w, http.StatusOK)
	if got := decode(t, w)["tags"].(map[string]any)["a"]; got != "1" {
		t.Fatalf("stored tag a = %v, want 1", got)
	}

	// DELETE does not invoke Admitter even if admitErr is set.
	order = nil
	admitErr = azerr.New(http.StatusForbidden, "RequestDisallowedByPolicy", "disallowed")
	want(t, send(http.MethodDelete, path, ""), http.StatusOK)
	if len(order) != 0 {
		t.Fatalf("DELETE invoked admitter/hooks: %v", order)
	}
}

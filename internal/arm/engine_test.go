package arm

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/moabukar/miniblue/internal/store"
)

const (
	subScope = "/subscriptions/00000000-0000-4000-8000-000000000001"
	apiV     = "?api-version=2024-01-01"
)

// newHandler registers synthetic types: widgets (all scopes, a child type and
// tag PATCH) and gadgets (subscription only, with hooks).
func newHandler(t *testing.T) http.Handler {
	t.Helper()
	reg := NewRegistry()
	reg.Register(Type{
		Name:        "Microsoft.Test/widgets",
		APIVersions: []string{"2024-01-01"},
		Scopes:      ScopeTenant | ScopeManagementGroup | ScopeSubscription | ScopeResourceGroup | ScopeResource,
		PatchTags:   true,
		Children:    []Type{{Name: "parts"}},
	})
	reg.Register(Type{
		Name:        "Microsoft.Test/gadgets",
		APIVersions: []string{"2024-01-01"},
		Scopes:      ScopeSubscription,
		Hooks: Hooks{
			Validate: func(_ ID, body map[string]interface{}) error {
				if _, ok := body["properties"]; !ok {
					return errors.New("properties is required")
				}
				return nil
			},
			Defaults: func(_ ID, body map[string]interface{}) {
				props := body["properties"].(map[string]interface{})
				if _, ok := props["mode"]; !ok {
					props["mode"] = "Indexed"
				}
			},
		},
	})
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTeapot) })
	return NewEngine(reg, NewStore(store.New())).Middleware(next)
}

func do(h http.Handler, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

func decode(t *testing.T, w *httptest.ResponseRecorder) map[string]interface{} {
	t.Helper()
	var m map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &m); err != nil {
		t.Fatalf("body %q is not a JSON object: %v", w.Body.String(), err)
	}
	return m
}

func want(t *testing.T, w *httptest.ResponseRecorder, status int) {
	t.Helper()
	if w.Code != status {
		t.Fatalf("status = %d, want %d (body %s)", w.Code, status, w.Body.String())
	}
}

func TestEngineCRUDAtEveryScope(t *testing.T) {
	scopes := map[string]string{
		"tenant":      "",
		"mg":          "/providers/Microsoft.Management/managementGroups/mg1",
		"sub":         subScope,
		"rg":          subScope + "/resourceGroups/rg1",
		"extension":   subScope + "/resourceGroups/rg1/providers/Microsoft.Other/things/t1",
		"ext-on-mg":   "/providers/Microsoft.Management/managementGroups/mg1/providers/Microsoft.Other/things/t1",
		"child-scope": subScope + "/providers/Microsoft.Other/things/t1/subthings/s1",
	}
	for name, scope := range scopes {
		t.Run(name, func(t *testing.T) {
			h := newHandler(t)
			path := scope + "/providers/Microsoft.Test/widgets/w1"
			want(t, do(h, http.MethodGet, path+apiV, ""), http.StatusNotFound)

			w := do(h, http.MethodPut, path+apiV, `{"properties":{"a":1}}`)
			want(t, w, http.StatusCreated)
			created := decode(t, w)
			if created["id"] != path || created["name"] != "w1" || created["type"] != "Microsoft.Test/widgets" {
				t.Fatalf("created = %v", created)
			}

			w = do(h, http.MethodPut, path+apiV, `{"properties":{"a":2}}`)
			want(t, w, http.StatusOK)

			w = do(h, http.MethodGet, strings.ToUpper(path)+apiV, "")
			want(t, w, http.StatusOK)
			got := decode(t, w)
			if got["id"] != path || got["properties"].(map[string]interface{})["a"] != float64(2) {
				t.Fatalf("got = %v", got)
			}

			want(t, do(h, http.MethodDelete, path+apiV, ""), http.StatusOK)
			want(t, do(h, http.MethodDelete, path+apiV, ""), http.StatusNoContent)
			want(t, do(h, http.MethodGet, path+apiV, ""), http.StatusNotFound)
		})
	}
}

func TestEngineKeepsCreatingCasing(t *testing.T) {
	h := newHandler(t)
	mangled := "/SUBSCRIPTIONS/00000000-0000-4000-8000-000000000001/PROVIDERS/MICROSOFT.TEST/WIDGETS/MyWidget"
	want(t, do(h, http.MethodPut, mangled+apiV, `{}`), http.StatusCreated)

	w := do(h, http.MethodPut, strings.ToLower(mangled)+apiV, `{}`)
	want(t, w, http.StatusOK)
	const canonical = subScope + "/providers/Microsoft.Test/widgets/MyWidget"
	if m := decode(t, w); m["id"] != canonical || m["name"] != "MyWidget" {
		t.Fatalf("update = %v, want id %s", m, canonical)
	}
	if m := decode(t, do(h, http.MethodGet, strings.ToUpper(mangled)+apiV, "")); m["id"] != canonical {
		t.Fatalf("get id = %v, want %s", m["id"], canonical)
	}
}

func TestEnginePassthroughWithoutHooks(t *testing.T) {
	h := newHandler(t)
	body := `{"properties":{"policyRule":{"if":{"field":"location","in":["a"]},"then":{"effect":"deny"}},"n":null},"extra":[1,"x"]}`
	path := subScope + "/providers/Microsoft.Test/widgets/w"
	want(t, do(h, http.MethodPut, path+apiV, body), http.StatusCreated)

	got := decode(t, do(h, http.MethodGet, path+apiV, ""))
	delete(got, "id")
	delete(got, "name")
	delete(got, "type")
	gotJSON, _ := json.Marshal(got)
	var wantMap map[string]interface{}
	_ = json.Unmarshal([]byte(body), &wantMap)
	wantJSON, _ := json.Marshal(wantMap)
	if string(gotJSON) != string(wantJSON) {
		t.Fatalf("body changed:\n got %s\nwant %s", gotJSON, wantJSON)
	}
}

func TestEngineKeepsNumbersExact(t *testing.T) {
	h := newHandler(t)
	path := subScope + "/providers/Microsoft.Test/widgets/w"
	want(t, do(h, http.MethodPut, path+apiV, `{"tags":{"a":"1"},"properties":{"big":9007199254740993,"dec":1.10}}`), http.StatusCreated)

	for name, w := range map[string]*httptest.ResponseRecorder{
		"get":   do(h, http.MethodGet, path+apiV, ""),
		"list":  do(h, http.MethodGet, subScope+"/providers/Microsoft.Test/widgets"+apiV, ""),
		"patch": do(h, http.MethodPatch, path+apiV, `{"tags":{"b":"2"}}`),
	} {
		want(t, w, http.StatusOK)
		for _, num := range []string{`"big":9007199254740993`, `"dec":1.10`} {
			if !strings.Contains(w.Body.String(), num) {
				t.Errorf("%s: %s not preserved in %s", name, num, w.Body.String())
			}
		}
	}
}

func TestEngineList(t *testing.T) {
	h := newHandler(t)
	rg := subScope + "/resourceGroups/rg1"
	for _, p := range []string{
		subScope + "/providers/Microsoft.Test/widgets/b",
		subScope + "/providers/Microsoft.Test/widgets/a",
		rg + "/providers/Microsoft.Test/widgets/c",
		subScope + "/providers/Microsoft.Test/gadgets/g",
	} {
		body := `{"properties":{}}`
		want(t, do(h, http.MethodPut, p+apiV, body), http.StatusCreated)
	}

	list := func(path string) []string {
		w := do(h, http.MethodGet, path+apiV, "")
		want(t, w, http.StatusOK)
		var names []string
		for _, v := range decode(t, w)["value"].([]interface{}) {
			names = append(names, v.(map[string]interface{})["name"].(string))
		}
		return names
	}
	if got := strings.Join(list(strings.ToUpper(subScope)+"/providers/microsoft.test/WIDGETS"), ","); got != "a,b" {
		t.Fatalf("sub list = %s, want a,b", got)
	}
	if got := strings.Join(list(rg+"/providers/Microsoft.Test/widgets"), ","); got != "c" {
		t.Fatalf("rg list = %s, want c", got)
	}
	w := do(h, http.MethodGet, "/providers/Microsoft.Test/widgets"+apiV, "")
	if w.Body.String() != "{\"value\":[]}\n" {
		t.Fatalf("empty list = %q", w.Body.String())
	}
}

func TestEngineChildTypes(t *testing.T) {
	h := newHandler(t)
	parent := subScope + "/providers/Microsoft.Test/widgets/w"
	want(t, do(h, http.MethodPut, parent+apiV, `{}`), http.StatusCreated)
	want(t, do(h, http.MethodPut, parent+"/parts/p1"+apiV, `{}`), http.StatusCreated)
	want(t, do(h, http.MethodPut, parent+"/parts/p2"+apiV, `{}`), http.StatusCreated)
	want(t, do(h, http.MethodPut, parent+"x/parts/p1"+apiV, `{}`), http.StatusCreated)

	w := do(h, http.MethodGet, parent+"/PARTS"+apiV, "")
	want(t, w, http.StatusOK)
	if n := len(decode(t, w)["value"].([]interface{})); n != 2 {
		t.Fatalf("parts = %d, want 2", n)
	}
	if m := decode(t, do(h, http.MethodGet, parent+"/parts/p1"+apiV, "")); m["type"] != "Microsoft.Test/widgets/parts" {
		t.Fatalf("child = %v", m)
	}

	want(t, do(h, http.MethodDelete, parent+apiV, ""), http.StatusOK)
	want(t, do(h, http.MethodGet, parent+"/parts/p1"+apiV, ""), http.StatusNotFound)
	want(t, do(h, http.MethodGet, parent+"x/parts/p1"+apiV, ""), http.StatusOK)
}

func TestEngineParentNamedProviders(t *testing.T) {
	h := newHandler(t)
	parent := subScope + "/providers/Microsoft.Test/widgets/providers"
	want(t, do(h, http.MethodPut, parent+apiV, `{}`), http.StatusCreated)
	want(t, do(h, http.MethodPut, parent+"/parts/p1"+apiV, `{}`), http.StatusCreated)
	if m := decode(t, do(h, http.MethodGet, strings.ToUpper(parent)+"/PARTS/p1"+apiV, "")); m["name"] != "p1" || m["type"] != "Microsoft.Test/widgets/parts" {
		t.Fatalf("child = %v", m)
	}
	w := do(h, http.MethodGet, parent+"/parts"+apiV, "")
	want(t, w, http.StatusOK)
	if n := len(decode(t, w)["value"].([]interface{})); n != 1 {
		t.Fatalf("parts = %d, want 1", n)
	}
}

func TestEngineHooks(t *testing.T) {
	h := newHandler(t)
	path := subScope + "/providers/Microsoft.Test/gadgets/g"
	want(t, do(h, http.MethodPut, path+apiV, `{}`), http.StatusBadRequest)

	want(t, do(h, http.MethodPut, path+apiV, `{"properties":{}}`), http.StatusCreated)
	props := decode(t, do(h, http.MethodGet, path+apiV, ""))["properties"].(map[string]interface{})
	if props["mode"] != "Indexed" {
		t.Fatalf("default not applied: %v", props)
	}
	want(t, do(h, http.MethodPut, path+apiV, `{"properties":{"mode":"All"}}`), http.StatusOK)
	props = decode(t, do(h, http.MethodGet, path+apiV, ""))["properties"].(map[string]interface{})
	if props["mode"] != "All" {
		t.Fatalf("explicit value overwritten: %v", props)
	}
}

func TestEngineRequestErrors(t *testing.T) {
	h := newHandler(t)
	path := subScope + "/providers/Microsoft.Test/widgets/w"
	cases := []struct {
		name, method, path, body string
		status                   int
	}{
		{"bad api-version", http.MethodGet, path + "?api-version=1999-01-01", "", http.StatusBadRequest},
		{"missing api-version is accepted", http.MethodGet, path, "", http.StatusNotFound},
		{"empty body", http.MethodPut, path + apiV, "", http.StatusBadRequest},
		{"non-object body", http.MethodPut, path + apiV, `[1]`, http.StatusBadRequest},
		{"null body", http.MethodPut, path + apiV, `null`, http.StatusBadRequest},
		{"name mismatch", http.MethodPut, path + apiV, `{"name":"other"}`, http.StatusBadRequest},
		{"id mismatch", http.MethodPut, path + apiV, `{"id":"/subscriptions/x"}`, http.StatusBadRequest},
		{"post item", http.MethodPost, path + apiV, `{}`, http.StatusMethodNotAllowed},
		{"post collection", http.MethodPost, subScope + "/providers/Microsoft.Test/widgets" + apiV, `{}`, http.StatusMethodNotAllowed},
		{"delete collection", http.MethodDelete, subScope + "/providers/Microsoft.Test/widgets" + apiV, "", http.StatusMethodNotAllowed},
		{"patch without PatchTags", http.MethodPatch, subScope + "/providers/Microsoft.Test/gadgets/g" + apiV, `{"tags":{}}`, http.StatusMethodNotAllowed},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			want(t, do(h, c.method, c.path, c.body), c.status)
		})
	}
}

func TestEnginePatchDoesNotResurrectDeleted(t *testing.T) {
	h := newHandler(t)
	path := subScope + "/providers/Microsoft.Test/widgets/w"
	want(t, do(h, http.MethodPut, path+apiV, `{"properties":{"p":1}}`), http.StatusCreated)
	want(t, do(h, http.MethodDelete, path+apiV, ""), http.StatusOK)
	want(t, do(h, http.MethodPatch, path+apiV, `{"tags":{"a":"1"}}`), http.StatusNotFound)
	want(t, do(h, http.MethodGet, path+apiV, ""), http.StatusNotFound)
	if got := decode(t, do(h, http.MethodGet, subScope+"/providers/Microsoft.Test/widgets"+apiV, "")); len(got["value"].([]interface{})) != 0 {
		t.Fatalf("deleted resource listed: %v", got)
	}
}

// PATCH must never write after a concurrent DELETE has removed the resource.
func TestEnginePatchRacingDelete(t *testing.T) {
	h := newHandler(t)
	path := subScope + "/providers/Microsoft.Test/widgets/w"
	for i := 0; i < 200; i++ {
		want(t, do(h, http.MethodPut, path+apiV, `{"properties":{"p":1}}`), http.StatusCreated)
		var wg sync.WaitGroup
		for j := 0; j < 4; j++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				do(h, http.MethodPatch, path+apiV, `{"tags":{"a":"1"}}`)
			}()
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			do(h, http.MethodDelete, path+apiV, "")
		}()
		wg.Wait()
		if w := do(h, http.MethodGet, path+apiV, ""); w.Code != http.StatusNotFound {
			t.Fatalf("iteration %d: resource resurrected by PATCH: %d %s", i, w.Code, w.Body)
		}
	}
}

// Concurrent first PUTs that differ only in URL casing must agree on one
// creator: a single 201, and every response carrying the committed id.
func TestEngineConcurrentCreateKeepsOneIdentity(t *testing.T) {
	h := newHandler(t)
	for i := 0; i < 50; i++ {
		base := fmt.Sprintf("Res%d", i)
		variants := []string{base, strings.ToUpper(base), strings.ToLower(base), "r" + strings.ToUpper(base[1:])}
		var (
			wg  sync.WaitGroup
			mu  sync.Mutex
			got []*httptest.ResponseRecorder
		)
		for _, name := range variants {
			wg.Add(1)
			go func(name string) {
				defer wg.Done()
				w := do(h, http.MethodPut, subScope+"/providers/Microsoft.Test/widgets/"+name+apiV, `{}`)
				mu.Lock()
				got = append(got, w)
				mu.Unlock()
			}(name)
		}
		wg.Wait()

		stored := decode(t, do(h, http.MethodGet, subScope+"/providers/Microsoft.Test/widgets/"+base+apiV, ""))
		creates := 0
		for _, w := range got {
			if w.Code == http.StatusCreated {
				creates++
			} else if w.Code != http.StatusOK {
				t.Fatalf("PUT status = %d: %s", w.Code, w.Body)
			}
			m := decode(t, w)
			if m["id"] != stored["id"] || m["name"] != stored["name"] {
				t.Fatalf("iteration %d: response id/name %v/%v differs from stored %v/%v",
					i, m["id"], m["name"], stored["id"], stored["name"])
			}
		}
		if creates != 1 {
			t.Fatalf("iteration %d: %d creates, want 1", i, creates)
		}
	}
}

func TestEnginePatchTags(t *testing.T) {
	h := newHandler(t)
	path := subScope + "/providers/Microsoft.Test/widgets/w"
	want(t, do(h, http.MethodPatch, path+apiV, `{"tags":{"a":"1"}}`), http.StatusNotFound)
	want(t, do(h, http.MethodPut, path+apiV, `{"properties":{"keep":true},"tags":{"old":"x"}}`), http.StatusCreated)

	w := do(h, http.MethodPatch, path+apiV, `{"tags":{"a":"1"}}`)
	want(t, w, http.StatusOK)
	m := decode(t, w)
	tags := m["tags"].(map[string]interface{})
	if len(tags) != 1 || tags["a"] != "1" || m["properties"] == nil {
		t.Fatalf("patched = %v", m)
	}
	want(t, do(h, http.MethodPatch, path+apiV, `{"properties":{}}`), http.StatusBadRequest)
	want(t, do(h, http.MethodPatch, path+apiV, `{"tags":"x"}`), http.StatusBadRequest)

	want(t, do(h, http.MethodPatch, path+apiV, `{"tags":null}`), http.StatusOK)
	if _, ok := decode(t, do(h, http.MethodGet, path+apiV, ""))["tags"]; ok {
		t.Fatal("tags not cleared")
	}
}

func TestEngineDoesNotClaimUnregistered(t *testing.T) {
	h := newHandler(t)
	for _, p := range []string{
		subScope + "/providers/Microsoft.Other/things/t",                    // unregistered type
		subScope + "/providers/Microsoft.Test/gadgets/g/extra/e",            // child type not registered
		subScope + "/resourceGroups/rg1/providers/Microsoft.Test/gadgets/g", // type not allowed at RG scope
		"/providers/Microsoft.Test/gadgets/g",                               // type not allowed at tenant scope
		subScope + "/resourceGroups/rg1",                                    // not a registered resource
		"/health",
		"/subscriptions/x//providers/Microsoft.Test/widgets/w", // malformed
	} {
		if w := do(h, http.MethodGet, p+apiV, ""); w.Code != http.StatusTeapot {
			t.Errorf("%s: status %d, want passthrough", p, w.Code)
		}
	}
}

func TestRegistryRejectsBadRegistrations(t *testing.T) {
	ok := Type{Name: "Microsoft.Test/widgets", APIVersions: []string{"1"}, Scopes: ScopeSubscription}
	cases := map[string]func(*Registry){
		"no slash": func(r *Registry) {
			r.Register(Type{Name: "widgets", APIVersions: []string{"1"}, Scopes: ScopeSubscription})
		},
		"no versions": func(r *Registry) { r.Register(Type{Name: "Microsoft.Test/w", Scopes: ScopeSubscription}) },
		"no scopes":   func(r *Registry) { r.Register(Type{Name: "Microsoft.Test/w", APIVersions: []string{"1"}}) },
		"duplicate":   func(r *Registry) { r.Register(ok); r.Register(ok) },
		"duplicate ci": func(r *Registry) {
			r.Register(ok)
			r.Register(Type{Name: "microsoft.test/WIDGETS", APIVersions: []string{"1"}, Scopes: ScopeSubscription})
		},
	}
	for name, f := range cases {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("expected panic")
				}
			}()
			f(NewRegistry())
		})
	}
}

package arm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/moabukar/miniblue/internal/azerr"
	"github.com/moabukar/miniblue/internal/store"
)

const (
	subScope = "/subscriptions/00000000-0000-4000-8000-000000000001"
	apiV     = "?api-version=2024-01-01"
)

func newTestEngine(t *testing.T, st *store.Store) *Engine {
	t.Helper()
	reg := NewRegistry()
	reg.Register(Type{
		Name:        "Microsoft.Test/widgets",
		APIVersions: []string{"2024-01-01"},
		Scopes:      ScopeTenant | ScopeManagementGroup | ScopeSubscription | ScopeResourceGroup | ScopeResource,
		Patch:       PatchTags,
		Children:    []Type{{Name: "parts"}},
	})
	reg.Register(Type{
		Name:        "Microsoft.Test/gadgets",
		APIVersions: []string{"2024-01-01"},
		Scopes:      ScopeSubscription,
		Patch:       PatchNone,
		Hooks: Hooks{
			Validate: func(_ context.Context, r *Request) error {
				if _, ok := r.Doc["properties"]; !ok {
					return errors.New("properties is required")
				}
				return nil
			},
			Defaults: func(_ context.Context, r *Request) error {
				props := r.Doc["properties"].(map[string]any)
				if _, ok := props["mode"]; !ok {
					props["mode"] = "Indexed"
				}
				return nil
			},
		},
	})
	return NewEngine(reg, NewStore(st))
}

// newHandler registers synthetic types: widgets (all scopes, a child type and
// tag PATCH) and gadgets (subscription only, with hooks).
func newHandler(t *testing.T) http.Handler {
	t.Helper()
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTeapot) })
	return newTestEngine(t, store.New()).Middleware(next)
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
	want(t, do(h, http.MethodPut, parent+"x"+apiV, `{}`), http.StatusCreated)
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
		{"patch with PatchNone", http.MethodPatch, subScope + "/providers/Microsoft.Test/gadgets/g" + apiV, `{"tags":{}}`, http.StatusMethodNotAllowed},
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

	want(t, do(h, http.MethodPatch, path+apiV, `{"tags":{}}`), http.StatusOK)
	if tags := decode(t, do(h, http.MethodGet, path+apiV, ""))["tags"].(map[string]interface{}); len(tags) != 0 {
		t.Fatalf("empty tags patch did not clear tags: %v", tags)
	}

	want(t, do(h, http.MethodPatch, path+apiV, `{"tags":null}`), http.StatusOK)
	if _, ok := decode(t, do(h, http.MethodGet, path+apiV, ""))["tags"]; ok {
		t.Fatal("tags not cleared")
	}
}

func TestEnginePatchMerge(t *testing.T) {
	reg := NewRegistry()
	var (
		validatedCur bool
		afterCalled  bool
	)
	// Omit Patch to verify the zero value is PatchMerge.
	reg.Register(Type{
		Name:        "Microsoft.Test/merges",
		APIVersions: []string{"2024-01-01"},
		Scopes:      ScopeSubscription,
		Hooks: Hooks{
			Validate: func(_ context.Context, r *Request) error {
				if r.Op == OpPatch {
					validatedCur = r.Current != nil
				}
				if props, ok := r.Doc["properties"].(map[string]any); ok {
					if props["bad"] == true {
						return azerr.New(http.StatusConflict, "InvalidMerge", "bad property")
					}
				}
				return nil
			},
			Defaults: func(_ context.Context, r *Request) error {
				if props, ok := r.Doc["properties"].(map[string]any); ok {
					if _, has := props["defaulted"]; !has {
						props["defaulted"] = "yes"
					}
				}
				return nil
			},
			After: func(_ context.Context, r *Request) {
				if r.Op == OpPatch {
					afterCalled = true
				}
			},
		},
	})
	e := NewEngine(reg, NewStore(store.New()))
	path := subScope + "/providers/Microsoft.Test/merges/m1"

	// Missing resource returns 404.
	want(t, do(e, http.MethodPatch, path+apiV, `{"properties":{"a":1}}`), http.StatusNotFound)

	// Create resource with nested objects, arrays, numbers, and tags.
	initial := `{"location":"eastus","tags":{"env":"dev","drop":"x"},"properties":{"keep":"ok","nested":{"a":1,"b":2},"arr":[1,2],"big":9007199254740993,"scalar":"s","obj":{"x":1}}}`
	want(t, do(e, http.MethodPut, path+apiV, initial), http.StatusCreated)

	// RFC 7386 merge:
	// - nested object merges recursively (keeps a, updates b, deletes nothing not null)
	// - null deletes top-level (location) and nested keys (tags.drop)
	// - null inside a newly created nested object (fresh.gone) is stripped
	// - arrays replace wholesale
	// - scalar -> object and object -> scalar replacements work
	// - server-owned id/name/type cannot be deleted or overwritten
	patch := `{"id":null,"name":null,"type":"Other/type","location":null,"tags":{"drop":null,"added":"y"},"properties":{"nested":{"b":3,"c":4},"arr":[9],"fresh":{"ok":1,"gone":null},"scalar":{"now":"obj"},"obj":"now-scalar","patchedBig":9007199254740995}}`
	w := do(e, http.MethodPatch, path+apiV, patch)
	want(t, w, http.StatusOK)

	if !validatedCur || !afterCalled {
		t.Fatalf("hooks on patch: validatedCur=%v afterCalled=%v", validatedCur, afterCalled)
	}
	for _, num := range []string{`"big":9007199254740993`, `"patchedBig":9007199254740995`} {
		if !strings.Contains(w.Body.String(), num) {
			t.Fatalf("%s not preserved in %s", num, w.Body.String())
		}
	}

	got := decode(t, w)
	if got["id"] != path || got["name"] != "m1" || got["type"] != "Microsoft.Test/merges" {
		t.Fatalf("server-owned identity corrupted: %v", got)
	}
	if _, has := got["location"]; has {
		t.Fatalf("location was not deleted: %v", got)
	}
	tags := got["tags"].(map[string]any)
	if len(tags) != 2 || tags["env"] != "dev" || tags["added"] != "y" {
		t.Fatalf("tags = %v, want env=dev added=y", tags)
	}
	props := got["properties"].(map[string]any)
	if props["keep"] != "ok" || props["defaulted"] != "yes" || props["obj"] != "now-scalar" {
		t.Fatalf("props = %v", props)
	}
	nested := props["nested"].(map[string]any)
	if nested["a"] != float64(1) || nested["b"] != float64(3) || nested["c"] != float64(4) {
		t.Fatalf("nested = %v", nested)
	}
	arr := props["arr"].([]any)
	if len(arr) != 1 || arr[0] != float64(9) {
		t.Fatalf("arr = %v, want [9]", arr)
	}
	fresh := props["fresh"].(map[string]any)
	if len(fresh) != 1 || fresh["ok"] != float64(1) {
		t.Fatalf("fresh = %v, want only ok=1 (null stripped)", fresh)
	}
	if props["scalar"].(map[string]any)["now"] != "obj" {
		t.Fatalf("scalar->obj = %v", props["scalar"])
	}

	// Body name/id mismatch returns 400.
	want(t, do(e, http.MethodPatch, path+apiV, `{"name":"other"}`), http.StatusBadRequest)
	want(t, do(e, http.MethodPatch, path+apiV, `{"id":"/subscriptions/other"}`), http.StatusBadRequest)

	// Hook validation error is preserved.
	w = do(e, http.MethodPatch, path+apiV, `{"properties":{"bad":true}}`)
	want(t, w, http.StatusConflict)
	if code := decode(t, w)["error"].(map[string]any)["code"]; code != "InvalidMerge" {
		t.Fatalf("code = %v, want InvalidMerge", code)
	}
}

func TestEnginePatchNoneDirectDo(t *testing.T) {
	e := newTestEngine(t, store.New())
	id, err := ParseID(subScope + "/providers/Microsoft.Test/gadgets/g1")
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = e.Do(context.Background(), &Request{
		Op:   OpPatch,
		ID:   id,
		Body: map[string]any{"tags": map[string]any{}},
	})
	var ae *azerr.Error
	if !errors.As(err, &ae) || ae.Status != http.StatusMethodNotAllowed {
		t.Fatalf("Do(OpPatch) on PatchNone err = %v, want 405", err)
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

func TestEngineResolveAndDo(t *testing.T) {
	e := newTestEngine(t, store.New())
	q := url.Values{"api-version": {"2024-01-01"}}
	itemPath := subScope + "/providers/Microsoft.Test/widgets/w1"
	collPath := subScope + "/providers/Microsoft.Test/widgets"

	if _, ok, err := e.Resolve(http.MethodGet, "/health", q); ok || err != nil {
		t.Fatalf("unregistered: ok=%v err=%v", ok, err)
	}

	req, ok, err := e.Resolve(http.MethodPut, itemPath, q)
	if !ok || err != nil || req.Op != OpPut {
		t.Fatalf("Resolve PUT: ok=%v err=%v req=%+v", ok, err, req)
	}
	req.Body = map[string]any{"properties": map[string]any{"k": "v"}}
	status, body, err := e.Do(context.Background(), req)
	if err != nil || status != http.StatusCreated {
		t.Fatalf("Do PUT: status=%d err=%v", status, err)
	}
	if doc := body.(map[string]any); doc["id"] != itemPath || doc["name"] != "w1" {
		t.Fatalf("Do PUT doc = %v", doc)
	}

	listReq, ok, err := e.Resolve(http.MethodGet, collPath, q)
	if !ok || err != nil || listReq.Op != OpList || listReq.ID.String() != subScope {
		t.Fatalf("Resolve LIST: ok=%v err=%v req=%+v", ok, err, listReq)
	}
	status, body, err = e.Do(context.Background(), listReq)
	if err != nil || status != http.StatusOK {
		t.Fatalf("Do LIST: status=%d err=%v", status, err)
	}
	if items := body.(map[string]any)["value"].([]map[string]any); len(items) != 1 {
		t.Fatalf("Do LIST items = %v", items)
	}

	delReq, ok, err := e.Resolve(http.MethodDelete, itemPath, q)
	if !ok || err != nil || delReq.Op != OpDelete {
		t.Fatalf("Resolve DELETE: ok=%v err=%v", ok, err)
	}
	status, body, err = e.Do(context.Background(), delReq)
	if err != nil || status != http.StatusOK || body != nil {
		t.Fatalf("Do DELETE: status=%d body=%v err=%v", status, body, err)
	}
}

func TestEngineServeHTTP(t *testing.T) {
	e := newTestEngine(t, store.New())
	want(t, do(e, http.MethodGet, "/health", ""), http.StatusNotFound)
	path := subScope + "/providers/Microsoft.Test/widgets/w1"
	want(t, do(e, http.MethodPut, path+apiV, `{}`), http.StatusCreated)
	want(t, do(e, http.MethodGet, path+apiV, ""), http.StatusOK)
}

func TestEngineHooksUnderLockWithCurrent(t *testing.T) {
	reg := NewRegistry()
	var afterSawCurrent bool
	var engine *Engine
	genCount := 0

	reg.Register(Type{
		Name:        "Microsoft.Test/items",
		APIVersions: []string{"2024-01-01"},
		Scopes:      ScopeSubscription,
		Hooks: Hooks{
			Validate: func(_ context.Context, r *Request) error {
				// Lock-free read inside Validate must not deadlock.
				if _, _, err := r.Store.Get(r.ID); err != nil {
					return err
				}
				props, _ := r.Doc["properties"].(map[string]any)
				if r.Current != nil {
					curProps, _ := r.Current["properties"].(map[string]any)
					if props["immutable"] != curProps["immutable"] {
						return azerr.New(http.StatusConflict, "ImmutableProperty", "immutable cannot change")
					}
				}
				return nil
			},
			Defaults: func(_ context.Context, r *Request) error {
				props, _ := r.Doc["properties"].(map[string]any)
				if r.Current != nil {
					curProps, _ := r.Current["properties"].(map[string]any)
					props["generated"] = curProps["generated"]
					return nil
				}
				genCount++
				props["generated"] = fmt.Sprintf("gen-%d", genCount)
				return nil
			},
			After: func(ctx context.Context, r *Request) {
				afterSawCurrent = r.Current != nil
				// After runs outside the write lock, so calling Do must not deadlock.
				getReq, ok, err := engine.Resolve(http.MethodGet, r.ID.String(), url.Values{"api-version": {"2024-01-01"}})
				if !ok || err != nil {
					t.Errorf("Resolve in After: ok=%v err=%v", ok, err)
					return
				}
				if status, _, err := engine.Do(ctx, getReq); err != nil || status != http.StatusOK {
					t.Errorf("Do in After: status=%d err=%v", status, err)
				}
			},
		},
	})
	engine = NewEngine(reg, NewStore(store.New()))
	path := subScope + "/providers/Microsoft.Test/items/i1"

	w := do(engine, http.MethodPut, path+apiV, `{"properties":{"immutable":"v1"}}`)
	want(t, w, http.StatusCreated)
	if afterSawCurrent {
		t.Fatal("After should see nil Current on create")
	}
	if gen := decode(t, w)["properties"].(map[string]any)["generated"]; gen != "gen-1" {
		t.Fatalf("generated = %v, want gen-1", gen)
	}

	// Update preserving immutable keeps stable generated value.
	w = do(engine, http.MethodPut, path+apiV, `{"properties":{"immutable":"v1","other":2}}`)
	want(t, w, http.StatusOK)
	if !afterSawCurrent {
		t.Fatal("After should see non-nil Current on update")
	}
	if gen := decode(t, w)["properties"].(map[string]any)["generated"]; gen != "gen-1" {
		t.Fatalf("generated changed across PUT: %v", gen)
	}

	// Changing immutable returns the typed *azerr.Error (409 ImmutableProperty).
	w = do(engine, http.MethodPut, path+apiV, `{"properties":{"immutable":"v2"}}`)
	want(t, w, http.StatusConflict)
	errObj := decode(t, w)["error"].(map[string]any)
	if errObj["code"] != "ImmutableProperty" {
		t.Fatalf("error code = %v, want ImmutableProperty", errObj["code"])
	}
}

func TestEngineChildRequiresParent(t *testing.T) {
	h := newHandler(t)
	child := subScope + "/providers/Microsoft.Test/widgets/missing/parts/p1"
	w := do(h, http.MethodPut, child+apiV, `{}`)
	want(t, w, http.StatusNotFound)
	if code := decode(t, w)["error"].(map[string]any)["code"]; code != "ParentResourceNotFound" {
		t.Fatalf("code = %v, want ParentResourceNotFound", code)
	}
}

func TestEngineIfNoneMatch(t *testing.T) {
	h := newHandler(t)
	path := subScope + "/providers/Microsoft.Test/widgets/w1"

	req := httptest.NewRequest(http.MethodPut, path+apiV, strings.NewReader(`{}`))
	req.Header.Set("If-None-Match", "*")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	want(t, w, http.StatusCreated)

	req = httptest.NewRequest(http.MethodPut, path+apiV, strings.NewReader(`{}`))
	req.Header.Set("If-None-Match", "*")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	want(t, w, http.StatusPreconditionFailed)
	if code := decode(t, w)["error"].(map[string]any)["code"]; code != "PreconditionFailed" {
		t.Fatalf("code = %v, want PreconditionFailed", code)
	}

	// If-Match is ignored.
	req = httptest.NewRequest(http.MethodPut, path+apiV, strings.NewReader(`{}`))
	req.Header.Set("If-Match", "*")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	want(t, w, http.StatusOK)
}

func TestEngineStoreErrorsReturn500(t *testing.T) {
	boom := errors.New("db down")
	fb := &failingBackend{MemoryBackend: store.NewMemoryBackend(), getErr: boom}
	e := newTestEngine(t, store.NewWithBackend(fb))
	path := subScope + "/providers/Microsoft.Test/widgets/w1"

	for _, tc := range []struct {
		name, method, url, body string
	}{
		{"get", http.MethodGet, path + apiV, ""},
		{"put", http.MethodPut, path + apiV, `{}`},
		{"patch", http.MethodPatch, path + apiV, `{"tags":{}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := do(e, tc.method, tc.url, tc.body)
			want(t, w, http.StatusInternalServerError)
			if code := decode(t, w)["error"].(map[string]any)["code"]; code != "InternalServerError" {
				t.Fatalf("code = %v, want InternalServerError", code)
			}
		})
	}

	fb = &failingBackend{MemoryBackend: store.NewMemoryBackend(), scanErr: boom}
	e = newTestEngine(t, store.NewWithBackend(fb))
	want(t, do(e, http.MethodGet, subScope+"/providers/Microsoft.Test/widgets"+apiV, ""), http.StatusInternalServerError)

	fb = &failingBackend{MemoryBackend: store.NewMemoryBackend(), deleteErr: boom}
	e = newTestEngine(t, store.NewWithBackend(fb))
	want(t, do(e, http.MethodDelete, path+apiV, ""), http.StatusInternalServerError)
}

func TestEngineSubscriptionWideList(t *testing.T) {
	reg := NewRegistry()
	reg.Register(Type{
		Name:        "Microsoft.Test/rgwidgets",
		APIVersions: []string{"2024-01-01"},
		Scopes:      ScopeResourceGroup,
		Children:    []Type{{Name: "parts"}},
	})
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTeapot) })
	h := NewEngine(reg, NewStore(store.New())).Middleware(next)

	const otherSub = "/subscriptions/00000000-0000-4000-8000-000000000002"
	for _, p := range []string{
		subScope + "/resourceGroups/rg1/providers/Microsoft.Test/rgwidgets/w1",
		subScope + "/resourceGroups/rg2/providers/Microsoft.Test/rgwidgets/w2",
		subScope + "/resourceGroups/rg1/providers/Microsoft.Test/rgwidgets/w1/parts/p1",
		otherSub + "/resourceGroups/rg1/providers/Microsoft.Test/rgwidgets/w3",
	} {
		want(t, do(h, http.MethodPut, p+apiV, `{}`), http.StatusCreated)
	}

	// Subscription-wide list returns RG resources in subScope, excluding otherSub and children.
	w := do(h, http.MethodGet, subScope+"/providers/Microsoft.Test/rgwidgets"+apiV, "")
	want(t, w, http.StatusOK)
	var names []string
	for _, v := range decode(t, w)["value"].([]any) {
		names = append(names, v.(map[string]any)["name"].(string))
	}
	if got := strings.Join(names, ","); got != "w1,w2" {
		t.Fatalf("sub-wide list = %q, want w1,w2", got)
	}

	// Non-GET on the subscription-wide collection returns 405.
	want(t, do(h, http.MethodPost, subScope+"/providers/Microsoft.Test/rgwidgets"+apiV, `{}`), http.StatusMethodNotAllowed)

	// Item PUT at subscription scope is not allowed for an RG-only type (falls through).
	want(t, do(h, http.MethodPut, subScope+"/providers/Microsoft.Test/rgwidgets/w1"+apiV, `{}`), http.StatusTeapot)
}

func TestEngineDeleteAlwaysRemovesExtensions(t *testing.T) {
	reg := NewRegistry()
	reg.Register(Type{
		Name:        "Microsoft.Test/leaves",
		APIVersions: []string{"2024-01-01"},
		Scopes:      ScopeResourceGroup,
	})
	reg.Register(Type{
		Name:        "Microsoft.Test/locks",
		APIVersions: []string{"2024-01-01"},
		Scopes:      ScopeResource,
	})
	e := NewEngine(reg, NewStore(store.New()))
	leaf := subScope + "/resourceGroups/rg1/providers/Microsoft.Test/leaves/l1"
	lock := leaf + "/providers/Microsoft.Test/locks/k1"

	want(t, do(e, http.MethodPut, leaf+apiV, `{}`), http.StatusCreated)
	want(t, do(e, http.MethodPut, lock+apiV, `{}`), http.StatusCreated)
	want(t, do(e, http.MethodDelete, leaf+apiV, ""), http.StatusOK)
	want(t, do(e, http.MethodGet, lock+apiV, ""), http.StatusNotFound)
}

func TestEngineWithScopeExists(t *testing.T) {
	boom := errors.New("rg lookup failed")
	reg := NewRegistry()
	reg.Register(Type{
		Name:        "Microsoft.Test/rgitems",
		APIVersions: []string{"2024-01-01"},
		Scopes:      ScopeResourceGroup | ScopeResource,
	})
	existingRGs := map[string]bool{"rg-ok": true}
	var checked []string
	e := NewEngine(reg, NewStore(store.New()), WithScopeExists(func(_ context.Context, scope ID) (bool, error) {
		checked = append(checked, scope.String())
		if scope.Name() == "rg-err" {
			return false, boom
		}
		return existingRGs[strings.ToLower(scope.Name())], nil
	}))

	// Missing RG -> 404 ResourceGroupNotFound.
	w := do(e, http.MethodPut, subScope+"/resourceGroups/rg-missing/providers/Microsoft.Test/rgitems/i1"+apiV, `{}`)
	want(t, w, http.StatusNotFound)
	if code := decode(t, w)["error"].(map[string]any)["code"]; code != "ResourceGroupNotFound" {
		t.Fatalf("code = %v, want ResourceGroupNotFound", code)
	}

	// Existing RG -> 201 Created.
	want(t, do(e, http.MethodPut, subScope+"/resourceGroups/rg-ok/providers/Microsoft.Test/rgitems/i1"+apiV, `{}`), http.StatusCreated)

	// Store error from scopeExists -> 500 InternalServerError.
	w = do(e, http.MethodPut, subScope+"/resourceGroups/rg-err/providers/Microsoft.Test/rgitems/i1"+apiV, `{}`)
	want(t, w, http.StatusInternalServerError)

	// Extension resource at resource scope does not call scopeExists.
	before := len(checked)
	extPath := subScope + "/resourceGroups/rg-missing/providers/Microsoft.Other/things/t1/providers/Microsoft.Test/rgitems/ext1"
	want(t, do(e, http.MethodPut, extPath+apiV, `{}`), http.StatusCreated)
	if len(checked) != before {
		t.Errorf("scopeExists should not be called for resource-scoped extension, got %v", checked[before:])
	}
}

func TestEngineScopeAwareNotFound(t *testing.T) {
	h := newHandler(t)
	rgPath := subScope + "/resourceGroups/rg1/providers/Microsoft.Test/widgets/missing"
	w := do(h, http.MethodGet, rgPath+apiV, "")
	want(t, w, http.StatusNotFound)
	msg := decode(t, w)["error"].(map[string]any)["message"].(string)
	if !strings.Contains(msg, "under resource group 'rg1'") {
		t.Fatalf("RG 404 message = %q, want 'under resource group'", msg)
	}

	subPath := subScope + "/providers/Microsoft.Test/widgets/missing"
	w = do(h, http.MethodGet, subPath+apiV, "")
	want(t, w, http.StatusNotFound)
	msg = decode(t, w)["error"].(map[string]any)["message"].(string)
	if strings.Contains(msg, "under resource group") {
		t.Fatalf("sub 404 message = %q, should not mention resource group", msg)
	}
}

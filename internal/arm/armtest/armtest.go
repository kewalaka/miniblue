// Package armtest is the ARM conformance test suite for resource types
// registered with internal/arm (ADR-0005). Every check derives its expectations
// from the registered *arm.Type metadata and exercises the full http.Handler.
package armtest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/moabukar/miniblue/internal/arm"
)

// Sample describes one registered ARM resource type for conformance testing.
type Sample struct {
	Type    string            // full type name, e.g. "Microsoft.ApiManagement/service/apis"
	Scope   arm.ScopeKind     // zero = first declared scope
	Body    map[string]any    // valid create
	Update  map[string]any    // valid replace, differs from Body
	Invalid map[string]any    // optional; Validate must reject it with 400
	Skip    map[string]string // check name -> reason (reason required)
}

var knownChecks = map[string]bool{
	"PutCreate":          true,
	"PutReplace":         true,
	"Get":                true,
	"GetMissing":         true,
	"UrlCasing":          true,
	"List":               true,
	"ListSubscription":   true,
	"Delete":             true,
	"DeleteMissing":      true,
	"MissingAPIVersion":  true,
	"NameIDMismatch":     true,
	"BadBody":            true,
	"Patch":              true,
	"ParentMissing":      true,
	"ChildCascade":       true,
	"RGCascade":          true,
	"PrefixSibling":      true,
	"DefaultsIdempotent": true,
	"ProvisioningState":  true,
	"ErrorEnvelope":      true,
}

var scopeOrder = []arm.ScopeKind{
	arm.ScopeTenant,
	arm.ScopeManagementGroup,
	arm.ScopeSubscription,
	arm.ScopeResourceGroup,
	arm.ScopeResource,
}

// testRunner abstracts *testing.T so armtest's own unit tests can verify that
// conformance violations fail the expected checks.
type testRunner interface {
	Helper()
	Errorf(format string, args ...any)
	Fatalf(format string, args ...any)
	Skip(args ...any)
	Parallel()
	runSub(name string, fn func(testRunner))
}

type realT struct{ t *testing.T }

func (r realT) Helper()                         { r.t.Helper() }
func (r realT) Errorf(f string, args ...any)    { r.t.Helper(); r.t.Errorf(f, args...) }
func (r realT) Fatalf(f string, args ...any)    { r.t.Helper(); r.t.Fatalf(f, args...) }
func (r realT) Skip(args ...any)                { r.t.Helper(); r.t.Skip(args...) }
func (r realT) Parallel()                       { r.t.Parallel() }
func (r realT) runSub(n string, fn func(testRunner)) {
	r.t.Helper()
	r.t.Run(n, func(sub *testing.T) { fn(realT{sub}) })
}

// Coverage verifies that every type registered in reg has a matching Sample.
func Coverage(t *testing.T, reg *arm.Registry, samples []Sample) {
	t.Helper()
	coverage(realT{t}, reg, samples)
}

func coverage(t testRunner, reg *arm.Registry, samples []Sample) {
	t.Helper()
	have := make(map[string]bool, len(samples))
	for _, s := range samples {
		have[strings.ToLower(s.Type)] = true
	}
	for _, typ := range reg.Types() {
		if !have[strings.ToLower(typ.Name)] {
			t.Errorf("armtest: registered type %q has no Sample", typ.Name)
		}
	}
}

// Run executes the ARM conformance suite against h for every Sample in samples.
// h should be the full server handler so middleware, casing, and headers are
// exercised. Checks use unique names and scopes, so subtests run in parallel.
func Run(t *testing.T, h http.Handler, reg *arm.Registry, samples []Sample) {
	t.Helper()
	run(realT{t}, h, reg, samples)
}

func run(t testRunner, h http.Handler, reg *arm.Registry, samples []Sample) {
	t.Helper()
	typesByName := make(map[string]*arm.Type)
	for _, typ := range reg.Types() {
		typesByName[strings.ToLower(typ.Name)] = typ
	}
	samplesByName := make(map[string]Sample, len(samples))
	for _, s := range samples {
		key := strings.ToLower(s.Type)
		if _, dup := samplesByName[key]; dup {
			t.Fatalf("armtest: duplicate Sample for type %q", s.Type)
		}
		samplesByName[key] = s
	}

	r := &suite{
		h:       h,
		types:   typesByName,
		samples: samplesByName,
	}

	for _, sample := range samples {
		typ, ok := typesByName[strings.ToLower(sample.Type)]
		if !ok {
			t.Fatalf("armtest: Sample type %q is not registered", sample.Type)
		}
		scope, err := resolveScope(typ, sample.Scope)
		if err != nil {
			t.Fatalf("armtest: %s: %v", typ.Name, err)
		}
		if sample.Body == nil {
			t.Fatalf("armtest: %s: Sample.Body is required", typ.Name)
		}
		for check, reason := range sample.Skip {
			if !knownChecks[check] {
				t.Fatalf("armtest: %s: unknown check %q in Skip", typ.Name, check)
			}
			if strings.TrimSpace(reason) == "" {
				t.Fatalf("armtest: %s: Skip[%q] requires a non-empty reason", typ.Name, check)
			}
		}

		tc := typeCase{
			suite:  r,
			typ:    typ,
			sample: sample,
			scope:  scope,
			apiVer: typ.APIVersions[0],
		}
		t.runSub(typ.Name, func(sub testRunner) {
			sub.Parallel()
			tc.runAll(sub)
		})
	}
}

func resolveScope(typ *arm.Type, want arm.ScopeKind) (arm.ScopeKind, error) {
	if want != 0 {
		if !typ.Scopes.Has(want) {
			return 0, fmt.Errorf("requested scope %d not in declared scopes %d", want, typ.Scopes)
		}
		return want, nil
	}
	for _, s := range scopeOrder {
		if typ.Scopes.Has(s) {
			return s, nil
		}
	}
	return 0, fmt.Errorf("type has no declared scopes")
}

type suite struct {
	h       http.Handler
	types   map[string]*arm.Type
	samples map[string]Sample
	seq     atomic.Uint64
}

func (s *suite) nextID() uint64 { return s.seq.Add(1) }

func (s *suite) nextSub() string {
	return fmt.Sprintf("00000000-0000-4000-8000-%012x", s.nextID())
}

func (s *suite) nextName(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, s.nextID())
}

type typeCase struct {
	suite  *suite
	typ    *arm.Type
	sample Sample
	scope  arm.ScopeKind
	apiVer string
}

func (tc *typeCase) isChild() bool {
	return strings.Count(tc.typ.Name, "/") > 1
}

func (tc *typeCase) runCheck(t testRunner, name string, fn func(testRunner)) {
	t.Helper()
	t.runSub(name, func(sub testRunner) {
		sub.Parallel()
		if reason, skip := tc.sample.Skip[name]; skip {
			sub.Skip(reason)
			return
		}
		fn(sub)
	})
}

func (tc *typeCase) runAll(t testRunner) {
	tc.runCheck(t, "PutCreate", tc.checkPutCreate)
	tc.runCheck(t, "PutReplace", tc.checkPutReplace)
	tc.runCheck(t, "Get", tc.checkGet)
	tc.runCheck(t, "GetMissing", tc.checkGetMissing)
	tc.runCheck(t, "UrlCasing", tc.checkURLCasing)
	tc.runCheck(t, "List", tc.checkList)
	if tc.scope == arm.ScopeResourceGroup && !tc.isChild() {
		tc.runCheck(t, "ListSubscription", tc.checkListSubscription)
	}
	tc.runCheck(t, "Delete", tc.checkDelete)
	tc.runCheck(t, "DeleteMissing", tc.checkDeleteMissing)
	tc.runCheck(t, "MissingAPIVersion", tc.checkMissingAPIVersion)
	tc.runCheck(t, "NameIDMismatch", tc.checkNameIDMismatch)
	tc.runCheck(t, "BadBody", tc.checkBadBody)
	tc.runCheck(t, "Patch", tc.checkPatch)
	if tc.isChild() {
		tc.runCheck(t, "ParentMissing", tc.checkParentMissing)
	}
	if len(tc.typ.Children) > 0 {
		tc.runCheck(t, "ChildCascade", tc.checkChildCascade)
	}
	if tc.scope == arm.ScopeResourceGroup {
		tc.runCheck(t, "RGCascade", tc.checkRGCascade)
	}
	tc.runCheck(t, "PrefixSibling", tc.checkPrefixSibling)
	tc.runCheck(t, "DefaultsIdempotent", tc.checkDefaultsIdempotent)
	if tc.typ.Provisioned {
		tc.runCheck(t, "ProvisioningState", tc.checkProvisioningState)
	}
	tc.runCheck(t, "ErrorEnvelope", tc.checkErrorEnvelope)
}

// ---------- path & parent helpers ----------

func (tc *typeCase) apiQuery() string { return "?api-version=" + tc.apiVer }

// prepareScope creates a fresh scope (including RG PUT when RG-scoped) and
// returns the scope path prefix, subscription ID, and RG name.
func (tc *typeCase) prepareScope(t testRunner) (scopePrefix, sub, rg string) {
	t.Helper()
	sub = tc.suite.nextSub()
	rg = tc.suite.nextName("rg")
	return tc.ensureScope(t, sub, rg), sub, rg
}

func (tc *typeCase) ensureScope(t testRunner, sub, rg string) string {
	t.Helper()
	switch tc.scope {
	case arm.ScopeTenant:
		return ""
	case arm.ScopeManagementGroup:
		return "/providers/Microsoft.Management/managementGroups/" + tc.suite.nextName("mg")
	case arm.ScopeSubscription:
		return "/subscriptions/" + sub
	case arm.ScopeResourceGroup:
		rgPath := "/subscriptions/" + sub + "/resourceGroups/" + rg
		w := tc.doJSON(t, http.MethodPut, rgPath+"?api-version=2021-04-01", map[string]any{"location": "eastus"})
		if w.Code != http.StatusCreated && w.Code != http.StatusOK {
			t.Fatalf("create resource group %s: status = %d, want 200 or 201 (body %s)", rgPath, w.Code, w.Body.String())
		}
		return rgPath
	case arm.ScopeResource:
		rgPath := "/subscriptions/" + sub + "/resourceGroups/" + rg
		return rgPath + "/providers/Microsoft.Test/targets/" + tc.suite.nextName("target")
	default:
		t.Fatalf("unsupported scope %d", tc.scope)
		return ""
	}
}

// prepareCollection ensures the scope and any parent chain exist and returns
// the collection path for tc.typ.
func (tc *typeCase) prepareCollection(t testRunner) (collPath, sub, rg string) {
	t.Helper()
	scopePrefix, sub, rg := tc.prepareScope(t)
	return tc.ensureCollectionInScope(t, scopePrefix, tc.typ), sub, rg
}

func (tc *typeCase) ensureCollectionInScope(t testRunner, scopePrefix string, typ *arm.Type) string {
	t.Helper()
	if strings.Count(typ.Name, "/") == 1 {
		return scopePrefix + "/providers/" + typ.Name
	}
	parentName, childSeg := splitTypeName(typ.Name)
	parentType, ok := tc.suite.types[strings.ToLower(parentName)]
	if !ok {
		t.Fatalf("parent type %q of %q is not registered", parentName, typ.Name)
	}
	parentSample, ok := tc.suite.samples[strings.ToLower(parentName)]
	if !ok {
		t.Fatalf("parent type %q of %q has no Sample", parentName, typ.Name)
	}
	parentColl := tc.ensureCollectionInScope(t, scopePrefix, parentType)
	parentRes := parentColl + "/" + tc.suite.nextName("parent")
	w := tc.doJSON(t, http.MethodPut, parentRes+"?api-version="+parentType.APIVersions[0], parentSample.Body)
	if w.Code != http.StatusCreated && w.Code != http.StatusOK {
		t.Fatalf("create parent %s: status = %d, want 201 (body %s)", parentRes, w.Code, w.Body.String())
	}
	return parentRes + "/" + childSeg
}

// buildUncreatedPath builds a resource path for typ under scopePrefix without
// creating any parent resources.
func (tc *typeCase) buildUncreatedPath(scopePrefix string, typ *arm.Type, name string) string {
	parts := strings.Split(typ.Name, "/")
	var b strings.Builder
	b.WriteString(scopePrefix)
	b.WriteString("/providers/")
	b.WriteString(parts[0])
	for i := 1; i < len(parts)-1; i++ {
		b.WriteString("/")
		b.WriteString(parts[i])
		b.WriteString("/")
		b.WriteString(tc.suite.nextName("missing-parent"))
	}
	b.WriteString("/")
	b.WriteString(parts[len(parts)-1])
	b.WriteString("/")
	b.WriteString(name)
	return b.String()
}

func splitTypeName(full string) (parent, childSeg string) {
	i := strings.LastIndex(full, "/")
	return full[:i], full[i+1:]
}

// ---------- HTTP & assertion helpers ----------

func (tc *typeCase) doRaw(method, target, body string) *httptest.ResponseRecorder {
	var r *http.Request
	if body != "" {
		r = httptest.NewRequest(method, target, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
	} else {
		r = httptest.NewRequest(method, target, nil)
	}
	w := httptest.NewRecorder()
	tc.suite.h.ServeHTTP(w, r)
	return w
}

func (tc *typeCase) doJSON(t testRunner, method, target string, payload any) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	return tc.doRaw(method, target, string(raw))
}

func decodeObject(t testRunner, body []byte) map[string]any {
	t.Helper()
	var m map[string]any
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	if err := dec.Decode(&m); err != nil {
		t.Fatalf("response %q is not a JSON object: %v", string(body), err)
	}
	if m == nil {
		t.Fatalf("response %q decoded to nil object", string(body))
	}
	return m
}

func normalizeMap(t testRunner, m map[string]any) map[string]any {
	t.Helper()
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal map: %v", err)
	}
	return decodeObject(t, raw)
}

func assertStatus(t testRunner, w *httptest.ResponseRecorder, want int) {
	t.Helper()
	if w.Code != want {
		t.Fatalf("status = %d, want %d (body %s)", w.Code, want, w.Body.String())
	}
}

func assertError(t testRunner, w *httptest.ResponseRecorder, wantStatus int, wantCode string) {
	t.Helper()
	assertStatus(t, w, wantStatus)
	if w.Header().Get("x-ms-request-id") == "" {
		t.Errorf("4xx response missing x-ms-request-id header")
	}
	env := decodeObject(t, w.Body.Bytes())
	errObj, ok := env["error"].(map[string]any)
	if !ok {
		t.Fatalf("error envelope missing 'error' object: %s", w.Body.String())
	}
	code, _ := errObj["code"].(string)
	msg, _ := errObj["message"].(string)
	if code == "" {
		t.Errorf("error envelope missing non-empty 'error.code': %s", w.Body.String())
	}
	if msg == "" {
		t.Errorf("error envelope missing non-empty 'error.message': %s", w.Body.String())
	}
	if wantCode != "" && code != wantCode {
		t.Errorf("error.code = %q, want %q (body %s)", code, wantCode, w.Body.String())
	}
}

func assertIdentity(t testRunner, doc map[string]any, wantID, wantName, wantType string) {
	t.Helper()
	if doc["id"] != wantID {
		t.Errorf("id = %v, want %q", doc["id"], wantID)
	}
	if doc["name"] != wantName {
		t.Errorf("name = %v, want %q", doc["name"], wantName)
	}
	if doc["type"] != wantType {
		t.Errorf("type = %v, want %q", doc["type"], wantType)
	}
}

func requireString(t testRunner, doc map[string]any, key string) string {
	t.Helper()
	s, ok := doc[key].(string)
	if !ok || s == "" {
		t.Fatalf("field %q = %#v, want non-empty string", key, doc[key])
	}
	return s
}

// assertContains verifies every key/value in want is present in got (recursively for maps).
func assertContains(t testRunner, got, want map[string]any) {
	t.Helper()
	for k, wv := range want {
		gv, ok := got[k]
		if !ok {
			t.Errorf("missing expected field %q", k)
			continue
		}
		if wm, ok := wv.(map[string]any); ok {
			gm, ok := gv.(map[string]any)
			if !ok {
				t.Errorf("field %q = %T, want object", k, gv)
				continue
			}
			assertContains(t, gm, wm)
			continue
		}
		if !reflect.DeepEqual(gv, wv) {
			t.Errorf("field %q = %#v, want %#v", k, gv, wv)
		}
	}
}

// ---------- individual checks ----------

func (tc *typeCase) checkPutCreate(t testRunner) {
	coll, _, _ := tc.prepareCollection(t)
	name := tc.suite.nextName("create")
	path := coll + "/" + name

	w := tc.doJSON(t, http.MethodPut, path+tc.apiQuery(), tc.sample.Body)
	assertStatus(t, w, http.StatusCreated)
	doc := decodeObject(t, w.Body.Bytes())
	assertIdentity(t, doc, path, name, tc.typ.Name)
	assertContains(t, doc, normalizeMap(t, tc.sample.Body))
}

func (tc *typeCase) checkPutReplace(t testRunner) {
	if tc.sample.Update == nil {
		t.Fatalf("Sample.Update is required for PutReplace")
	}
	coll, _, _ := tc.prepareCollection(t)
	name := tc.suite.nextName("replace")
	path := coll + "/" + name

	w := tc.doJSON(t, http.MethodPut, path+tc.apiQuery(), tc.sample.Body)
	assertStatus(t, w, http.StatusCreated)
	created := decodeObject(t, w.Body.Bytes())

	w = tc.doJSON(t, http.MethodPut, path+tc.apiQuery(), tc.sample.Update)
	assertStatus(t, w, http.StatusOK)
	replaced := decodeObject(t, w.Body.Bytes())
	assertIdentity(t, replaced, requireString(t, created, "id"), requireString(t, created, "name"), tc.typ.Name)

	normBody := normalizeMap(t, tc.sample.Body)
	normUpdate := normalizeMap(t, tc.sample.Update)
	assertContains(t, replaced, normUpdate)

	for k := range normBody {
		if k == "id" || k == "name" || k == "type" {
			continue
		}
		if _, inUpdate := normUpdate[k]; !inUpdate {
			if _, stillPresent := replaced[k]; stillPresent {
				t.Errorf("PutReplace did not drop top-level field %q absent from Update", k)
			}
		}
	}
	if bp, ok := normBody["properties"].(map[string]any); ok {
		if up, ok := normUpdate["properties"].(map[string]any); ok {
			rp, ok := replaced["properties"].(map[string]any)
			if !ok {
				t.Fatalf("replaced properties is not an object: %#v", replaced["properties"])
			}
			for k := range bp {
				if tc.typ.Provisioned && k == "provisioningState" {
					continue
				}
				if _, inUpdate := up[k]; !inUpdate {
					if _, stillPresent := rp[k]; stillPresent {
						t.Errorf("PutReplace did not drop properties.%s absent from Update", k)
					}
				}
			}
		}
	}

	w = tc.doRaw(http.MethodGet, path+tc.apiQuery(), "")
	assertStatus(t, w, http.StatusOK)
	got := decodeObject(t, w.Body.Bytes())
	if !reflect.DeepEqual(got, replaced) {
		t.Errorf("GET after PutReplace = %#v, want %#v", got, replaced)
	}
}

func (tc *typeCase) checkGet(t testRunner) {
	coll, _, _ := tc.prepareCollection(t)
	name := tc.suite.nextName("get")
	path := coll + "/" + name

	w := tc.doJSON(t, http.MethodPut, path+tc.apiQuery(), tc.sample.Body)
	assertStatus(t, w, http.StatusCreated)
	created := decodeObject(t, w.Body.Bytes())

	w = tc.doRaw(http.MethodGet, path+tc.apiQuery(), "")
	assertStatus(t, w, http.StatusOK)
	got := decodeObject(t, w.Body.Bytes())
	if !reflect.DeepEqual(got, created) {
		t.Errorf("GET = %#v, want %#v", got, created)
	}
}

func (tc *typeCase) checkGetMissing(t testRunner) {
	coll, _, _ := tc.prepareCollection(t)
	path := coll + "/" + tc.suite.nextName("missing")

	w := tc.doRaw(http.MethodGet, path+tc.apiQuery(), "")
	assertError(t, w, http.StatusNotFound, "ResourceNotFound")
}

func (tc *typeCase) checkURLCasing(t testRunner) {
	coll, _, _ := tc.prepareCollection(t)
	name := tc.suite.nextName("Case-Test")
	path := coll + "/" + name

	w := tc.doJSON(t, http.MethodPut, path+tc.apiQuery(), tc.sample.Body)
	assertStatus(t, w, http.StatusCreated)
	created := decodeObject(t, w.Body.Bytes())

	createdID := requireString(t, created, "id")
	createdName := requireString(t, created, "name")

	upperPath := strings.ToUpper(path)
	w = tc.doRaw(http.MethodGet, upperPath+tc.apiQuery(), "")
	assertStatus(t, w, http.StatusOK)
	got := decodeObject(t, w.Body.Bytes())
	assertIdentity(t, got, createdID, createdName, tc.typ.Name)

	if tc.sample.Update != nil {
		w = tc.doJSON(t, http.MethodPut, upperPath+tc.apiQuery(), tc.sample.Update)
		assertStatus(t, w, http.StatusOK)
		updated := decodeObject(t, w.Body.Bytes())
		assertIdentity(t, updated, createdID, createdName, tc.typ.Name)
	}

	w = tc.doRaw(http.MethodDelete, upperPath+tc.apiQuery(), "")
	assertStatus(t, w, http.StatusOK)
}

func (tc *typeCase) checkList(t testRunner) {
	coll, _, _ := tc.prepareCollection(t)
	idA := coll + "/" + tc.suite.nextName("list-a")
	idB := coll + "/" + tc.suite.nextName("list-b")

	assertStatus(t, tc.doJSON(t, http.MethodPut, idA+tc.apiQuery(), tc.sample.Body), http.StatusCreated)
	assertStatus(t, tc.doJSON(t, http.MethodPut, idB+tc.apiQuery(), tc.sample.Body), http.StatusCreated)

	w := tc.doRaw(http.MethodGet, coll+tc.apiQuery(), "")
	assertStatus(t, w, http.StatusOK)
	assertListContains(t, w.Body.Bytes(), idA, idB)
}

func (tc *typeCase) checkListSubscription(t testRunner) {
	sub := tc.suite.nextSub()
	rg1 := tc.suite.nextName("rg")
	rg2 := tc.suite.nextName("rg")
	scope1 := tc.ensureScope(t, sub, rg1)
	scope2 := tc.ensureScope(t, sub, rg2)

	idA := scope1 + "/providers/" + tc.typ.Name + "/" + tc.suite.nextName("sublist-a")
	idB := scope2 + "/providers/" + tc.typ.Name + "/" + tc.suite.nextName("sublist-b")

	assertStatus(t, tc.doJSON(t, http.MethodPut, idA+tc.apiQuery(), tc.sample.Body), http.StatusCreated)
	assertStatus(t, tc.doJSON(t, http.MethodPut, idB+tc.apiQuery(), tc.sample.Body), http.StatusCreated)

	subColl := "/subscriptions/" + sub + "/providers/" + tc.typ.Name
	w := tc.doRaw(http.MethodGet, subColl+tc.apiQuery(), "")
	assertStatus(t, w, http.StatusOK)
	assertListContains(t, w.Body.Bytes(), idA, idB)
}

func assertListContains(t testRunner, body []byte, wantIDs ...string) {
	t.Helper()
	obj := decodeObject(t, body)
	vals, ok := obj["value"].([]any)
	if !ok {
		t.Fatalf("list response missing 'value' array: %s", string(body))
	}
	found := make(map[string]bool, len(vals))
	for _, item := range vals {
		if m, ok := item.(map[string]any); ok {
			if id, ok := m["id"].(string); ok {
				found[id] = true
			}
		}
	}
	for _, want := range wantIDs {
		if !found[want] {
			t.Errorf("list missing resource %q (got %d items)", want, len(vals))
		}
	}
}

func (tc *typeCase) checkDelete(t testRunner) {
	coll, _, _ := tc.prepareCollection(t)
	path := coll + "/" + tc.suite.nextName("del")

	assertStatus(t, tc.doJSON(t, http.MethodPut, path+tc.apiQuery(), tc.sample.Body), http.StatusCreated)
	assertStatus(t, tc.doRaw(http.MethodDelete, path+tc.apiQuery(), ""), http.StatusOK)
	assertError(t, tc.doRaw(http.MethodGet, path+tc.apiQuery(), ""), http.StatusNotFound, "ResourceNotFound")
}

func (tc *typeCase) checkDeleteMissing(t testRunner) {
	coll, _, _ := tc.prepareCollection(t)
	path := coll + "/" + tc.suite.nextName("del-missing")

	w := tc.doRaw(http.MethodDelete, path+tc.apiQuery(), "")
	if w.Code != http.StatusNoContent && w.Code != http.StatusOK {
		t.Fatalf("DELETE missing status = %d, want 204 or 200 (body %s)", w.Code, w.Body.String())
	}
}

func (tc *typeCase) checkMissingAPIVersion(t testRunner) {
	coll, _, _ := tc.prepareCollection(t)
	path := coll + "/" + tc.suite.nextName("no-ver")

	w := tc.doRaw(http.MethodGet, path, "")
	assertError(t, w, http.StatusBadRequest, "MissingApiVersionParameter")
}

func (tc *typeCase) checkNameIDMismatch(t testRunner) {
	coll, _, _ := tc.prepareCollection(t)
	path := coll + "/" + tc.suite.nextName("mismatch")

	badName := normalizeMap(t, tc.sample.Body)
	badName["name"] = "wrong-name-mismatch"
	assertError(t, tc.doJSON(t, http.MethodPut, path+tc.apiQuery(), badName), http.StatusBadRequest, "")

	badID := normalizeMap(t, tc.sample.Body)
	badID["id"] = "/subscriptions/00000000-0000-0000-0000-000000000000/providers/Microsoft.Other/things/wrong"
	assertError(t, tc.doJSON(t, http.MethodPut, path+tc.apiQuery(), badID), http.StatusBadRequest, "")
}

func (tc *typeCase) checkBadBody(t testRunner) {
	coll, _, _ := tc.prepareCollection(t)
	path := coll + "/" + tc.suite.nextName("badbody")

	assertError(t, tc.doRaw(http.MethodPut, path+tc.apiQuery(), "{not json"), http.StatusBadRequest, "InvalidRequestContent")

	if tc.sample.Invalid != nil {
		assertError(t, tc.doJSON(t, http.MethodPut, path+tc.apiQuery(), tc.sample.Invalid), http.StatusBadRequest, "")
	}
}

func (tc *typeCase) checkPatch(t testRunner) {
	coll, _, _ := tc.prepareCollection(t)
	path := coll + "/" + tc.suite.nextName("patch")

	w := tc.doJSON(t, http.MethodPut, path+tc.apiQuery(), tc.sample.Body)
	assertStatus(t, w, http.StatusCreated)
	created := decodeObject(t, w.Body.Bytes())

	switch tc.typ.Patch {
	case arm.PatchNone:
		assertError(t, tc.doJSON(t, http.MethodPatch, path+tc.apiQuery(), map[string]any{"tags": map[string]any{"env": "test"}}),
			http.StatusMethodNotAllowed, "MethodNotAllowed")

	case arm.PatchTags:
		w = tc.doJSON(t, http.MethodPatch, path+tc.apiQuery(), map[string]any{"tags": map[string]any{"armtest": "tagged"}})
		assertStatus(t, w, http.StatusOK)
		patched := decodeObject(t, w.Body.Bytes())
		assertIdentity(t, patched, requireString(t, created, "id"), requireString(t, created, "name"), tc.typ.Name)
		tags, _ := patched["tags"].(map[string]any)
		if tags["armtest"] != "tagged" {
			t.Errorf("PatchTags did not apply tags: got %#v", patched["tags"])
		}
		if cp, ok := created["properties"]; ok && !reflect.DeepEqual(patched["properties"], cp) {
			t.Errorf("PatchTags modified properties: got %#v, want %#v", patched["properties"], cp)
		}

		w = tc.doJSON(t, http.MethodPatch, path+tc.apiQuery(), map[string]any{"tags": nil})
		assertStatus(t, w, http.StatusOK)
		cleared := decodeObject(t, w.Body.Bytes())
		if _, hasTags := cleared["tags"]; hasTags {
			t.Errorf("PatchTags with null did not remove tags: %#v", cleared["tags"])
		}

		assertError(t, tc.doJSON(t, http.MethodPatch, path+tc.apiQuery(), map[string]any{"notTags": true}),
			http.StatusBadRequest, "InvalidRequestContent")

		missingPath := coll + "/" + tc.suite.nextName("patch-missing")
		assertError(t, tc.doJSON(t, http.MethodPatch, missingPath+tc.apiQuery(), map[string]any{"tags": map[string]any{"a": "b"}}),
			http.StatusNotFound, "ResourceNotFound")

	case arm.PatchMerge:
		w = tc.doJSON(t, http.MethodPatch, path+tc.apiQuery(), map[string]any{"tags": map[string]any{"armtest": "merged"}})
		assertStatus(t, w, http.StatusOK)
		patched := decodeObject(t, w.Body.Bytes())
		assertIdentity(t, patched, requireString(t, created, "id"), requireString(t, created, "name"), tc.typ.Name)
		tags, _ := patched["tags"].(map[string]any)
		if tags["armtest"] != "merged" {
			t.Errorf("PatchMerge did not merge tags: got %#v", patched["tags"])
		}
		if cp, ok := created["properties"]; ok && !reflect.DeepEqual(patched["properties"], cp) {
			t.Errorf("PatchMerge lost existing properties: got %#v, want %#v", patched["properties"], cp)
		}

		w = tc.doJSON(t, http.MethodPatch, path+tc.apiQuery(), map[string]any{"tags": map[string]any{"armtest": nil}})
		assertStatus(t, w, http.StatusOK)
		cleared := decodeObject(t, w.Body.Bytes())
		if ct, ok := cleared["tags"].(map[string]any); ok {
			if _, exists := ct["armtest"]; exists {
				t.Errorf("PatchMerge with null did not remove key 'armtest': %#v", ct)
			}
		}

		missingPath := coll + "/" + tc.suite.nextName("patch-missing")
		assertError(t, tc.doJSON(t, http.MethodPatch, missingPath+tc.apiQuery(), map[string]any{"tags": map[string]any{"a": "b"}}),
			http.StatusNotFound, "ResourceNotFound")
	}
}

func (tc *typeCase) checkParentMissing(t testRunner) {
	scopePrefix, _, _ := tc.prepareScope(t)
	path := tc.buildUncreatedPath(scopePrefix, tc.typ, tc.suite.nextName("orphan"))

	w := tc.doJSON(t, http.MethodPut, path+tc.apiQuery(), tc.sample.Body)
	assertError(t, w, http.StatusNotFound, "ParentResourceNotFound")
}

func (tc *typeCase) checkChildCascade(t testRunner) {
	coll, _, _ := tc.prepareCollection(t)
	parentPath := coll + "/" + tc.suite.nextName("cascade-parent")
	assertStatus(t, tc.doJSON(t, http.MethodPut, parentPath+tc.apiQuery(), tc.sample.Body), http.StatusCreated)

	type childRef struct {
		path   string
		apiVer string
	}
	var children []childRef
	for _, c := range tc.typ.Children {
		childFull := tc.typ.Name + "/" + c.Name
		childType, ok := tc.suite.types[strings.ToLower(childFull)]
		if !ok {
			continue
		}
		childSample, ok := tc.suite.samples[strings.ToLower(childFull)]
		if !ok {
			t.Fatalf("child type %q has no Sample for ChildCascade", childFull)
		}
		childPath := parentPath + "/" + c.Name + "/" + tc.suite.nextName("child")
		childVer := childType.APIVersions[0]
		assertStatus(t, tc.doJSON(t, http.MethodPut, childPath+"?api-version="+childVer, childSample.Body), http.StatusCreated)
		children = append(children, childRef{path: childPath, apiVer: childVer})
	}

	assertStatus(t, tc.doRaw(http.MethodDelete, parentPath+tc.apiQuery(), ""), http.StatusOK)

	for _, c := range children {
		assertError(t, tc.doRaw(http.MethodGet, c.path+"?api-version="+c.apiVer, ""), http.StatusNotFound, "ResourceNotFound")
	}
}

func (tc *typeCase) checkRGCascade(t testRunner) {
	coll, sub, rg := tc.prepareCollection(t)
	path := coll + "/" + tc.suite.nextName("rg-cascade")
	assertStatus(t, tc.doJSON(t, http.MethodPut, path+tc.apiQuery(), tc.sample.Body), http.StatusCreated)

	rgPath := "/subscriptions/" + sub + "/resourceGroups/" + rg + "?api-version=2021-04-01"
	w := tc.doRaw(http.MethodDelete, rgPath, "")
	if w.Code != http.StatusOK && w.Code != http.StatusAccepted {
		t.Fatalf("DELETE resource group %s: status = %d, want 200 or 202 (body %s)", rgPath, w.Code, w.Body.String())
	}

	assertError(t, tc.doRaw(http.MethodGet, path+tc.apiQuery(), ""), http.StatusNotFound, "ResourceNotFound")
}

func (tc *typeCase) checkPrefixSibling(t testRunner) {
	coll, _, _ := tc.prepareCollection(t)
	base := tc.suite.nextName("sib")
	pathX := coll + "/" + base
	pathX2 := coll + "/" + base + "2"

	assertStatus(t, tc.doJSON(t, http.MethodPut, pathX+tc.apiQuery(), tc.sample.Body), http.StatusCreated)
	assertStatus(t, tc.doJSON(t, http.MethodPut, pathX2+tc.apiQuery(), tc.sample.Body), http.StatusCreated)

	assertStatus(t, tc.doRaw(http.MethodDelete, pathX+tc.apiQuery(), ""), http.StatusOK)
	assertError(t, tc.doRaw(http.MethodGet, pathX+tc.apiQuery(), ""), http.StatusNotFound, "ResourceNotFound")
	assertStatus(t, tc.doRaw(http.MethodGet, pathX2+tc.apiQuery(), ""), http.StatusOK)
}

func (tc *typeCase) checkDefaultsIdempotent(t testRunner) {
	coll, _, _ := tc.prepareCollection(t)
	path := coll + "/" + tc.suite.nextName("idemp")

	assertStatus(t, tc.doJSON(t, http.MethodPut, path+tc.apiQuery(), tc.sample.Body), http.StatusCreated)
	w1 := tc.doRaw(http.MethodGet, path+tc.apiQuery(), "")
	assertStatus(t, w1, http.StatusOK)
	first := decodeObject(t, w1.Body.Bytes())

	assertStatus(t, tc.doJSON(t, http.MethodPut, path+tc.apiQuery(), tc.sample.Body), http.StatusOK)
	w2 := tc.doRaw(http.MethodGet, path+tc.apiQuery(), "")
	assertStatus(t, w2, http.StatusOK)
	second := decodeObject(t, w2.Body.Bytes())

	if !reflect.DeepEqual(first, second) {
		t.Errorf("second PUT changed resource:\nfirst:  %#v\nsecond: %#v", first, second)
	}
}

func (tc *typeCase) checkProvisioningState(t testRunner) {
	coll, _, _ := tc.prepareCollection(t)
	path := coll + "/" + tc.suite.nextName("prov")

	w := tc.doJSON(t, http.MethodPut, path+tc.apiQuery(), tc.sample.Body)
	assertStatus(t, w, http.StatusCreated)
	checkProv := func(label string, body []byte) {
		doc := decodeObject(t, body)
		props, _ := doc["properties"].(map[string]any)
		if props["provisioningState"] != "Succeeded" {
			t.Errorf("%s: properties.provisioningState = %v, want \"Succeeded\"", label, props["provisioningState"])
		}
	}
	checkProv("PUT", w.Body.Bytes())

	w = tc.doRaw(http.MethodGet, path+tc.apiQuery(), "")
	assertStatus(t, w, http.StatusOK)
	checkProv("GET", w.Body.Bytes())
}

func (tc *typeCase) checkErrorEnvelope(t testRunner) {
	coll, _, _ := tc.prepareCollection(t)
	path := coll + "/" + tc.suite.nextName("env")

	assertError(t, tc.doRaw(http.MethodGet, path, ""), http.StatusBadRequest, "MissingApiVersionParameter")
	assertError(t, tc.doRaw(http.MethodGet, path+tc.apiQuery(), ""), http.StatusNotFound, "ResourceNotFound")
}

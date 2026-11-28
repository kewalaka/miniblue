package armtest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/moabukar/miniblue/internal/arm"
	"github.com/moabukar/miniblue/internal/services/resourcegroups"
	"github.com/moabukar/miniblue/internal/store"
)

// spyT records failures and skips from internal run/coverage without failing
// the outer *testing.T.
type spyT struct {
	mu       sync.Mutex
	path     string
	root     *spyT
	failures map[string][]string
	skipped  map[string]string
}

type fatalSentinel struct{}
type skipSentinel struct{}

func newSpyT() *spyT {
	s := &spyT{
		failures: map[string][]string{},
		skipped:  map[string]string{},
	}
	s.root = s
	return s
}

func (s *spyT) Helper()   {}
func (s *spyT) Parallel() {}

func (s *spyT) Errorf(format string, args ...any) {
	s.root.mu.Lock()
	defer s.root.mu.Unlock()
	s.root.failures[s.path] = append(s.root.failures[s.path], fmt.Sprintf(format, args...))
}

func (s *spyT) Fatalf(format string, args ...any) {
	s.Errorf(format, args...)
	panic(fatalSentinel{})
}

func (s *spyT) Skip(args ...any) {
	s.root.mu.Lock()
	s.root.skipped[s.path] = fmt.Sprint(args...)
	s.root.mu.Unlock()
	panic(skipSentinel{})
}

func (s *spyT) runSub(name string, fn func(testRunner)) {
	childPath := name
	if s.path != "" {
		childPath = s.path + "/" + name
	}
	child := &spyT{path: childPath, root: s.root}
	defer func() {
		if r := recover(); r != nil {
			switch r.(type) {
			case fatalSentinel, skipSentinel:
			default:
				panic(r)
			}
		}
	}()
	fn(child)
}

func (s *spyT) failed(path string) bool {
	s.root.mu.Lock()
	defer s.root.mu.Unlock()
	return len(s.root.failures[path]) > 0
}

func (s *spyT) allFailures() map[string][]string {
	s.root.mu.Lock()
	defer s.root.mu.Unlock()
	out := make(map[string][]string, len(s.root.failures))
	for k, v := range s.root.failures {
		out[k] = append([]string(nil), v...)
	}
	return out
}

func (s *spyT) runSafe(fn func(testRunner)) {
	defer func() {
		if r := recover(); r != nil {
			switch r.(type) {
			case fatalSentinel, skipSentinel:
			default:
				panic(r)
			}
		}
	}()
	fn(s)
}

// buildTestHandler wires an arm.Engine + resourcegroups handler + x-ms-request-id
// header middleware, matching the full server contract for synthetic types.
func buildTestHandler(reg *arm.Registry) http.Handler {
	st := store.New()
	scopeExists := func(_ context.Context, scope arm.ID) (bool, error) {
		if !strings.EqualFold(scope.TypeName(), "Microsoft.Resources/resourceGroups") {
			return true, nil
		}
		want := "rg:" + scope.Parent().Name() + ":" + scope.Name()
		if _, ok, err := st.Lookup(want); err != nil || ok {
			return ok, err
		}
		kvs, err := st.Scan("rg:")
		if err != nil {
			return false, err
		}
		for _, kv := range kvs {
			if strings.EqualFold(kv.Key, want) {
				return true, nil
			}
		}
		return false, nil
	}
	eng := arm.NewEngine(reg, arm.NewStore(st), arm.WithScopeExists(scopeExists))

	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			w.Header().Set("x-ms-request-id", "test-request-id")
			next.ServeHTTP(w, req)
		})
	})
	r.Use(eng.Middleware)
	// Case-insensitive RG route matching for armtest's /resourceGroups/ paths.
	rgHandler := resourcegroups.NewHandler(st, eng)
	r.Route("/subscriptions/{subscriptionId}/resourceGroups", func(r chi.Router) {
		r.Route("/{resourceGroupName}", func(r chi.Router) {
			r.Put("/", rgHandler.CreateOrUpdate)
			r.Delete("/", rgHandler.Delete)
		})
	})
	return r
}

func TestConformanceHappyPathAllFeatures(t *testing.T) {
	reg := arm.NewRegistry()
	reg.Register(arm.Type{
		Name:        "Microsoft.Test/services",
		APIVersions: []string{"2024-05-01"},
		Scopes:      arm.ScopeResourceGroup,
		Provisioned: true,
		Patch:       arm.PatchMerge,
		Hooks: arm.Hooks{
			Validate: func(_ context.Context, r *arm.Request) error {
				props, _ := r.Doc["properties"].(map[string]any)
				if props["tier"] == "Invalid" {
					return errors.New("tier cannot be Invalid")
				}
				return nil
			},
			Defaults: func(_ context.Context, r *arm.Request) error {
				props, _ := r.Doc["properties"].(map[string]any)
				if props == nil {
					props = map[string]any{}
					r.Doc["properties"] = props
				}
				if _, ok := props["sku"]; !ok {
					props["sku"] = "Standard"
				}
				return nil
			},
		},
		Children: []arm.Type{
			{
				Name:        "apis",
				Provisioned: true,
				Patch:       arm.PatchTags,
			},
		},
	})
	reg.Register(arm.Type{
		Name:        "Microsoft.Test/policies",
		APIVersions: []string{"2024-01-01"},
		Scopes:      arm.ScopeSubscription,
		Patch:       arm.PatchNone,
	})

	samples := []Sample{
		{
			Type: "Microsoft.Test/services",
			Body: map[string]any{
				"location": "eastus",
				"properties": map[string]any{
					"tier":        "Basic",
					"description": "initial",
				},
			},
			Update: map[string]any{
				"location": "eastus",
				"properties": map[string]any{
					"tier": "Premium",
				},
			},
			Invalid: map[string]any{
				"location":   "eastus",
				"properties": map[string]any{"tier": "Invalid"},
			},
		},
		{
			Type: "Microsoft.Test/services/apis",
			Body: map[string]any{
				"properties": map[string]any{
					"displayName": "Echo",
					"extra":       "drop-me",
				},
			},
			Update: map[string]any{
				"properties": map[string]any{
					"displayName": "Echo v2",
				},
			},
		},
		{
			Type: "Microsoft.Test/policies",
			Body: map[string]any{
				"properties": map[string]any{"mode": "All", "note": "initial"},
			},
			Update: map[string]any{
				"properties": map[string]any{"mode": "Indexed"},
			},
			Skip: map[string]string{
				"DefaultsIdempotent": "exercising skip with reason",
			},
		},
	}

	h := buildTestHandler(reg)
	Coverage(t, reg, samples)
	Run(t, h, reg, samples)
}

func TestCoverageCatchesMissingSample(t *testing.T) {
	reg := arm.NewRegistry()
	reg.Register(arm.Type{
		Name:        "Microsoft.Test/widgets",
		APIVersions: []string{"2024-01-01"},
		Scopes:      arm.ScopeSubscription,
	})
	reg.Register(arm.Type{
		Name:        "Microsoft.Test/gadgets",
		APIVersions: []string{"2024-01-01"},
		Scopes:      arm.ScopeSubscription,
	})

	spy := newSpyT()
	coverage(spy, reg, []Sample{{Type: "Microsoft.Test/widgets"}})
	if !spy.failed("") {
		t.Fatalf("expected Coverage to fail for missing Microsoft.Test/gadgets sample")
	}
	msg := strings.Join(spy.allFailures()[""], "\n")
	if !strings.Contains(msg, "Microsoft.Test/gadgets") {
		t.Fatalf("failure message %q does not name missing type", msg)
	}
}

func TestSkipValidation(t *testing.T) {
	reg := arm.NewRegistry()
	reg.Register(arm.Type{
		Name:        "Microsoft.Test/widgets",
		APIVersions: []string{"2024-01-01"},
		Scopes:      arm.ScopeSubscription,
	})
	h := buildTestHandler(reg)

	t.Run("empty reason rejected", func(t *testing.T) {
		spy := newSpyT()
		spy.runSafe(func(tr testRunner) {
			run(tr, h, reg, []Sample{{
				Type:   "Microsoft.Test/widgets",
				Body:   map[string]any{"properties": map[string]any{}},
				Update: map[string]any{"properties": map[string]any{}},
				Skip:   map[string]string{"PutCreate": "   "},
			}})
		})
		if !spy.failed("") {
			t.Fatalf("expected empty Skip reason to fail")
		}
	})

	t.Run("unknown check rejected", func(t *testing.T) {
		spy := newSpyT()
		spy.runSafe(func(tr testRunner) {
			run(tr, h, reg, []Sample{{
				Type:   "Microsoft.Test/widgets",
				Body:   map[string]any{"properties": map[string]any{}},
				Update: map[string]any{"properties": map[string]any{}},
				Skip:   map[string]string{"NotARealCheck": "because"},
			}})
		})
		if !spy.failed("") {
			t.Fatalf("expected unknown Skip check to fail")
		}
	})
}

func TestConformanceCatchesViolations(t *testing.T) {
	baseType := func() (*arm.Registry, []Sample) {
		reg := arm.NewRegistry()
		reg.Register(arm.Type{
			Name:        "Microsoft.Test/items",
			APIVersions: []string{"2024-01-01"},
			Scopes:      arm.ScopeSubscription,
			Provisioned: true,
			Patch:       arm.PatchNone,
		})
		samples := []Sample{{
			Type:   "Microsoft.Test/items",
			Body:   map[string]any{"properties": map[string]any{"a": "1", "dropMe": "yes"}},
			Update: map[string]any{"properties": map[string]any{"a": "2"}},
		}}
		return reg, samples
	}

	t.Run("missing x-ms-request-id fails ErrorEnvelope", func(t *testing.T) {
		reg, samples := baseType()
		eng := arm.NewEngine(reg, arm.NewStore(store.New())) // bare engine without x-ms-request-id
		spy := newSpyT()
		run(spy, eng, reg, samples)
		if !spy.failed("Microsoft.Test/items/ErrorEnvelope") {
			t.Fatalf("expected ErrorEnvelope to fail without x-ms-request-id, failures=%v", spy.allFailures())
		}
	})

	t.Run("non-idempotent Defaults fails DefaultsIdempotent", func(t *testing.T) {
		var counter atomic.Int64
		reg := arm.NewRegistry()
		reg.Register(arm.Type{
			Name:        "Microsoft.Test/flaky",
			APIVersions: []string{"2024-01-01"},
			Scopes:      arm.ScopeSubscription,
			Hooks: arm.Hooks{
				Defaults: func(_ context.Context, r *arm.Request) error {
					props, _ := r.Doc["properties"].(map[string]any)
					props["nonce"] = fmt.Sprintf("n-%d", counter.Add(1))
					return nil
				},
			},
		})
		samples := []Sample{{
			Type:   "Microsoft.Test/flaky",
			Body:   map[string]any{"properties": map[string]any{"a": "1"}},
			Update: map[string]any{"properties": map[string]any{"a": "2"}},
		}}
		spy := newSpyT()
		run(spy, buildTestHandler(reg), reg, samples)
		if !spy.failed("Microsoft.Test/flaky/DefaultsIdempotent") {
			t.Fatalf("expected DefaultsIdempotent to fail, failures=%v", spy.allFailures())
		}
	})

	t.Run("wrong status on PUT create fails PutCreate", func(t *testing.T) {
		reg, samples := baseType()
		base := buildTestHandler(reg)
		broken := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rec := httptest.NewRecorder()
			base.ServeHTTP(rec, r)
			copyHeaders(w.Header(), rec.Header())
			code := rec.Code
			if r.Method == http.MethodPut && code == http.StatusCreated {
				code = http.StatusOK
			}
			w.WriteHeader(code)
			w.Write(rec.Body.Bytes())
		})
		spy := newSpyT()
		run(spy, broken, reg, samples)
		if !spy.failed("Microsoft.Test/items/PutCreate") {
			t.Fatalf("expected PutCreate to fail on 200 instead of 201, failures=%v", spy.allFailures())
		}
	})

	t.Run("stale field kept on replace fails PutReplace", func(t *testing.T) {
		reg, samples := baseType()
		base := buildTestHandler(reg)
		broken := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rec := httptest.NewRecorder()
			base.ServeHTTP(rec, r)
			copyHeaders(w.Header(), rec.Header())
			body := rec.Body.Bytes()
			if r.Method == http.MethodPut && rec.Code == http.StatusOK {
				var doc map[string]any
				if json.Unmarshal(body, &doc) == nil {
					if props, ok := doc["properties"].(map[string]any); ok {
						props["dropMe"] = "still-here"
						body, _ = json.Marshal(doc)
					}
				}
			}
			w.WriteHeader(rec.Code)
			w.Write(body)
		})
		spy := newSpyT()
		run(spy, broken, reg, samples)
		if !spy.failed("Microsoft.Test/items/PutReplace") {
			t.Fatalf("expected PutReplace to fail when stale field remains, failures=%v", spy.allFailures())
		}
	})

	t.Run("missing provisioningState fails ProvisioningState", func(t *testing.T) {
		reg, samples := baseType()
		base := buildTestHandler(reg)
		broken := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rec := httptest.NewRecorder()
			base.ServeHTTP(rec, r)
			copyHeaders(w.Header(), rec.Header())
			body := rec.Body.Bytes()
			if rec.Code == http.StatusCreated || rec.Code == http.StatusOK {
				var doc map[string]any
				if json.Unmarshal(body, &doc) == nil {
					if props, ok := doc["properties"].(map[string]any); ok {
						delete(props, "provisioningState")
						body, _ = json.Marshal(doc)
					}
				}
			}
			w.WriteHeader(rec.Code)
			w.Write(body)
		})
		spy := newSpyT()
		run(spy, broken, reg, samples)
		if !spy.failed("Microsoft.Test/items/ProvisioningState") {
			t.Fatalf("expected ProvisioningState to fail, failures=%v", spy.allFailures())
		}
	})

	t.Run("accepting missing api-version fails MissingAPIVersion", func(t *testing.T) {
		reg, samples := baseType()
		base := buildTestHandler(reg)
		broken := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Query().Get("api-version") == "" {
				q := r.URL.Query()
				q.Set("api-version", "2024-01-01")
				r.URL.RawQuery = q.Encode()
			}
			base.ServeHTTP(w, r)
		})
		spy := newSpyT()
		run(spy, broken, reg, samples)
		if !spy.failed("Microsoft.Test/items/MissingAPIVersion") {
			t.Fatalf("expected MissingAPIVersion to fail, failures=%v", spy.allFailures())
		}
	})

	t.Run("mangled casing on GET fails UrlCasing", func(t *testing.T) {
		reg, samples := baseType()
		base := buildTestHandler(reg)
		broken := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rec := httptest.NewRecorder()
			base.ServeHTTP(rec, r)
			copyHeaders(w.Header(), rec.Header())
			body := rec.Body.Bytes()
			if r.Method == http.MethodGet && rec.Code == http.StatusOK {
				var doc map[string]any
				if json.Unmarshal(body, &doc) == nil {
					if id, ok := doc["id"].(string); ok {
						doc["id"] = strings.ToUpper(id)
						body, _ = json.Marshal(doc)
					}
				}
			}
			w.WriteHeader(rec.Code)
			w.Write(body)
		})
		spy := newSpyT()
		run(spy, broken, reg, samples)
		if !spy.failed("Microsoft.Test/items/UrlCasing") {
			t.Fatalf("expected UrlCasing to fail when response id casing changes, failures=%v", spy.allFailures())
		}
	})

	t.Run("PATCH succeeding on PatchNone fails Patch", func(t *testing.T) {
		reg, samples := baseType()
		base := buildTestHandler(reg)
		broken := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodPatch {
				w.Header().Set("x-ms-request-id", "test-request-id")
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(`{}`))
				return
			}
			base.ServeHTTP(w, r)
		})
		spy := newSpyT()
		run(spy, broken, reg, samples)
		if !spy.failed("Microsoft.Test/items/Patch") {
			t.Fatalf("expected Patch to fail when PatchNone returns 200, failures=%v", spy.allFailures())
		}
	})

	t.Run("RG delete not cascading fails RGCascade", func(t *testing.T) {
		reg := arm.NewRegistry()
		reg.Register(arm.Type{
			Name:        "Microsoft.Test/rgitems",
			APIVersions: []string{"2024-01-01"},
			Scopes:      arm.ScopeResourceGroup,
		})
		samples := []Sample{{
			Type:   "Microsoft.Test/rgitems",
			Body:   map[string]any{"properties": map[string]any{"a": "1"}},
			Update: map[string]any{"properties": map[string]any{"a": "2"}},
		}}
		// Wire resourcegroups WITHOUT the engine so RG delete does not cascade.
		st := store.New()
		scopeExists := func(_ context.Context, scope arm.ID) (bool, error) {
			if !strings.EqualFold(scope.TypeName(), "Microsoft.Resources/resourceGroups") {
				return true, nil
			}
			_, ok, err := st.Lookup("rg:" + scope.Parent().Name() + ":" + scope.Name())
			return ok, err
		}
		eng := arm.NewEngine(reg, arm.NewStore(st), arm.WithScopeExists(scopeExists))
		r := chi.NewRouter()
		r.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				w.Header().Set("x-ms-request-id", "test-request-id")
				next.ServeHTTP(w, req)
			})
		})
		r.Use(eng.Middleware)
		rgHandler := resourcegroups.NewHandler(st, nil) // nil engine -> no cascade
		r.Route("/subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}", func(r chi.Router) {
			r.Put("/", rgHandler.CreateOrUpdate)
			r.Delete("/", rgHandler.Delete)
		})

		spy := newSpyT()
		run(spy, r, reg, samples)
		if !spy.failed("Microsoft.Test/rgitems/RGCascade") {
			t.Fatalf("expected RGCascade to fail when RG delete does not cascade, failures=%v", spy.allFailures())
		}
	})
}

func copyHeaders(dst, src http.Header) {
	for k, vals := range src {
		for _, v := range vals {
			dst.Add(k, v)
		}
	}
}

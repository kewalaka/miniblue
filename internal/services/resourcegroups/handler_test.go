package resourcegroups

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/moabukar/miniblue/internal/arm"
	"github.com/moabukar/miniblue/internal/store"
)

func TestDeleteRunsCleanersInOrder(t *testing.T) {
	var calls []string
	record := func(name string) Cleaner {
		return func(sub, rg string) { calls = append(calls, name+":"+sub+":"+rg) }
	}
	r := chi.NewRouter()
	NewHandler(store.New(), nil, record("a"), record("b")).Register(r)

	put := httptest.NewRequest(http.MethodPut, "/subscriptions/sub1/resourcegroups/rg1", strings.NewReader(`{"location":"eastus"}`))
	r.ServeHTTP(httptest.NewRecorder(), put)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/subscriptions/sub1/resourcegroups/rg1", nil))

	if w.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d", w.Code)
	}
	if !strings.Contains(w.Header().Get("Location"), "/subscriptions/sub1/operationresults/delete-rg-rg1") {
		t.Errorf("unexpected Location header %q", w.Header().Get("Location"))
	}
	if got, want := strings.Join(calls, ","), "a:sub1:rg1,b:sub1:rg1"; got != want {
		t.Errorf("cleaner calls = %q, want %q", got, want)
	}
}

func TestDeleteRunsEngineDeleteScopeAheadOfCleaners(t *testing.T) {
	st := store.New()
	reg := arm.NewRegistry()
	reg.Register(arm.Type{
		Name:        "Microsoft.Test/widgets",
		APIVersions: []string{"2024-01-01"},
		Scopes:      arm.ScopeResourceGroup,
	})
	armStore := arm.NewStore(st)
	eng := arm.NewEngine(reg, armStore)

	putWidget := func(rg, name string) arm.ID {
		path := "/subscriptions/sub1/resourceGroups/" + rg + "/providers/Microsoft.Test/widgets/" + name
		req, ok, err := eng.Resolve(http.MethodPut, path, nil)
		if !ok || err != nil {
			t.Fatalf("Resolve(%s): ok=%v err=%v", path, ok, err)
		}
		req.Body = map[string]any{}
		if status, _, err := eng.Do(context.Background(), req); err != nil || status != http.StatusCreated {
			t.Fatalf("Do(%s): status=%d err=%v", path, status, err)
		}
		return req.ID
	}
	inRG1 := putWidget("rg1", "w1")
	inRG10 := putWidget("rg10", "w2")

	var sawRG1Deleted bool
	cleaner := func(sub, rg string) {
		_, ok, err := armStore.Get(inRG1)
		if err != nil {
			t.Errorf("Get in cleaner: %v", err)
		}
		sawRG1Deleted = !ok
	}

	r := chi.NewRouter()
	NewHandler(st, eng, cleaner).Register(r)
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPut, "/subscriptions/sub1/resourcegroups/rg1", strings.NewReader(`{"location":"eastus"}`)))

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/subscriptions/sub1/resourcegroups/rg1", nil))
	if w.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", w.Code, w.Body.String())
	}
	if !sawRG1Deleted {
		t.Error("expected engine DeleteScope to run before legacy cleaners")
	}
	if _, ok, err := armStore.Get(inRG10); err != nil || !ok {
		t.Errorf("rg10 engine resource should survive rg1 delete: ok=%v err=%v", ok, err)
	}
}

func TestDeleteMissingGroupSkipsCleaners(t *testing.T) {
	called := false
	r := chi.NewRouter()
	NewHandler(store.New(), nil, func(sub, rg string) { called = true }).Register(r)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/subscriptions/sub1/resourcegroups/nope", nil))

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
	if called {
		t.Error("cleaners must not run when the resource group does not exist")
	}
}

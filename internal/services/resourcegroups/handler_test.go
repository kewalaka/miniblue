package resourcegroups

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/moabukar/miniblue/internal/store"
)

func TestDeleteRunsCleanersInOrder(t *testing.T) {
	var calls []string
	record := func(name string) Cleaner {
		return func(sub, rg string) { calls = append(calls, name+":"+sub+":"+rg) }
	}
	r := chi.NewRouter()
	NewHandler(store.New(), record("a"), record("b")).Register(r)

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

func TestDeleteMissingGroupSkipsCleaners(t *testing.T) {
	called := false
	r := chi.NewRouter()
	NewHandler(store.New(), func(sub, rg string) { called = true }).Register(r)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/subscriptions/sub1/resourcegroups/nope", nil))

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
	if called {
		t.Error("cleaners must not run when the resource group does not exist")
	}
}

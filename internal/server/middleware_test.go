package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

const skipPollingHeader = "X-Go-Azure-Sdk-Skip-Polling-Delay"

func TestAzureHeaders_SkipPollingDelay(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusCreated, http.StatusNotFound} {
		h := AzureHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(status)
		}))
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodPut, "/subscriptions/s/resourceGroups/rg", nil))

		if got := w.Header().Get(skipPollingHeader); got != "true" {
			t.Errorf("status %d: %s = %q, want true", status, skipPollingHeader, got)
		}
	}
}

func TestHandler_SendsSkipPollingDelay(t *testing.T) {
	w := httptest.NewRecorder()
	New().Handler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/health", nil))

	if got := w.Header().Get(skipPollingHeader); got != "true" {
		t.Fatalf("%s = %q, want true", skipPollingHeader, got)
	}
}

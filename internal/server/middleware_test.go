package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/moabukar/miniblue/internal/arm"
	"github.com/moabukar/miniblue/internal/token"
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

func TestExtractPrincipal(t *testing.T) {
	def := arm.Principal{
		ObjectID: token.DefaultClientOID,
		TenantID: token.DefaultTenantID,
		AppID:    token.DefaultClientID,
	}

	cases := []struct {
		name   string
		header string
		want   arm.Principal
	}{
		{
			name: "missing header uses default",
			want: def,
		},
		{
			name:   "malformed header uses default",
			header: "Bearer not-a-jwt",
			want:   def,
		},
		{
			name: "oid tid appid extracted",
			header: "Bearer " + token.Issue(token.Claims{
				OID:   "11111111-1111-1111-1111-111111111111",
				Sub:   "22222222-2222-2222-2222-222222222222",
				TID:   "33333333-3333-3333-3333-333333333333",
				AppID: "my-app",
			}),
			want: arm.Principal{
				ObjectID: "11111111-1111-1111-1111-111111111111",
				TenantID: "33333333-3333-3333-3333-333333333333",
				AppID:    "my-app",
			},
		},
		{
			name: "falls back to sub when oid empty",
			header: "Bearer " + token.Issue(token.Claims{
				Sub:   "22222222-2222-2222-2222-222222222222",
				AppID: "mi-client",
			}),
			want: arm.Principal{
				ObjectID: "22222222-2222-2222-2222-222222222222",
				TenantID: token.DefaultTenantID,
				AppID:    "mi-client",
			},
		},
		{
			name:   "empty oid and sub uses default",
			header: "Bearer " + token.Issue(token.Claims{TID: "33333333-3333-3333-3333-333333333333"}),
			want:   def,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got arm.Principal
			h := ExtractPrincipal(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				got = arm.PrincipalFrom(r.Context())
				w.WriteHeader(http.StatusOK)
			}))
			req := httptest.NewRequest(http.MethodGet, "/health", nil)
			if tc.header != "" {
				req.Header.Set("Authorization", tc.header)
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", w.Code)
			}
			if got != tc.want {
				t.Fatalf("principal = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestServerWiresExtractPrincipal(t *testing.T) {
	s := New()
	var got arm.Principal
	s.router.Get("/_test/principal", func(w http.ResponseWriter, r *http.Request) {
		got = arm.PrincipalFrom(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	tok := token.Issue(token.Claims{
		OID:   "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa",
		TID:   "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb",
		AppID: "ci-runner",
	})
	req := httptest.NewRequest(http.MethodGet, "/_test/principal", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	wantP := arm.Principal{
		ObjectID: "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa",
		TenantID: "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb",
		AppID:    "ci-runner",
	}
	if got != wantP {
		t.Fatalf("principal = %+v, want %+v", got, wantP)
	}
}

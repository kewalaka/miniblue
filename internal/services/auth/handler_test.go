package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/moabukar/miniblue/internal/store"
	"github.com/moabukar/miniblue/internal/token"
)

func TestTokenPerClientOID(t *testing.T) {
	r := chi.NewRouter()
	NewHandler(store.New()).Register(r)

	request := func(tenant, clientID string) token.Claims {
		t.Helper()
		form := url.Values{}
		if clientID != "" {
			form.Set("client_id", clientID)
		}
		req := httptest.NewRequest(http.MethodPost, "/"+tenant+"/oauth2/v2.0/token", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("POST token: want 200, got %d: %s", w.Code, w.Body.String())
		}
		var body map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		raw, _ := body["access_token"].(string)
		claims, ok := token.Parse(raw)
		if !ok {
			t.Fatalf("token.Parse(%q) failed", raw)
		}
		return claims
	}

	// Default client (omitted or "miniblue") keeps ...0002.
	omitted := request(token.DefaultTenantID, "")
	if omitted.OID != token.DefaultClientOID || omitted.Sub != token.DefaultClientOID {
		t.Fatalf("omitted client_id: got oid=%q sub=%q, want %q", omitted.OID, omitted.Sub, token.DefaultClientOID)
	}
	if omitted.AppID != token.DefaultClientID {
		t.Fatalf("omitted client_id: got appid=%q, want %q", omitted.AppID, token.DefaultClientID)
	}

	explicitDefault := request(token.DefaultTenantID, token.DefaultClientID)
	if explicitDefault.OID != token.DefaultClientOID {
		t.Fatalf("miniblue client_id: got oid=%q, want %q", explicitDefault.OID, token.DefaultClientOID)
	}

	// Non-default clients get distinct, stable OIDs.
	alpha1 := request(token.DefaultTenantID, "alpha-app")
	alpha2 := request(token.DefaultTenantID, "alpha-app")
	beta := request(token.DefaultTenantID, "beta-app")

	if alpha1.OID != alpha2.OID {
		t.Fatalf("alpha-app oid not stable: %q vs %q", alpha1.OID, alpha2.OID)
	}
	if alpha1.OID == token.DefaultClientOID || alpha1.OID == beta.OID {
		t.Fatalf("expected distinct non-default OIDs: alpha=%q beta=%q", alpha1.OID, beta.OID)
	}
	if alpha1.Sub != alpha1.OID || alpha1.AppID != "alpha-app" || alpha1.TID != token.DefaultTenantID {
		t.Fatalf("unexpected alpha claims: %+v", alpha1)
	}
}

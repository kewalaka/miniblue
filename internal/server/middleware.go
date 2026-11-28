package server

import (
	"net/http"

	"github.com/google/uuid"
	"github.com/moabukar/miniblue/internal/arm"
	"github.com/moabukar/miniblue/internal/token"
)

// CORS adds permissive CORS headers for local development.
func CORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS, HEAD")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, x-ms-version, x-ms-date, x-ms-client-request-id")
		w.Header().Set("Access-Control-Max-Age", "86400")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// AzureHeaders adds standard Azure response headers to every request.
// CORS preflight (OPTIONS) is answered earlier in the chain and is intentionally
// exempt: the skip-polling header only matters to SDK pollers, never to browsers.
func AzureHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := uuid.New().String()
		correlationID := uuid.New().String()

		w.Header().Set("x-ms-version", "2023-11-03")
		w.Header().Set("x-ms-request-id", requestID)
		w.Header().Set("x-ms-correlation-request-id", correlationID)
		w.Header().Set("x-ms-routing-request-id", "LOCALAZURE:"+requestID)
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		// go-azure-sdk skips its ~10s provisioningState poll delay on this header (ADR-0006).
		w.Header().Set("X-Go-Azure-Sdk-Skip-Polling-Delay", "true")

		next.ServeHTTP(w, r)
	})
}

// APIVersionCheck stores the api-version if provided but does not reject requests without it.
// Real Azure requires api-version on ARM calls but miniblue is lenient for local development.
func APIVersionCheck(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if apiVersion := r.URL.Query().Get("api-version"); apiVersion != "" {
			w.Header().Set("x-ms-api-version", apiVersion)
		}
		next.ServeHTTP(w, r)
	})
}

// ExtractPrincipal reads an unverified bearer token from the Authorization
// header and attaches an arm.Principal to the request context. Missing or
// malformed tokens resolve to the default principal; it never fails a request.
func ExtractPrincipal(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := principalFromHeader(r.Header.Get("Authorization"))
		next.ServeHTTP(w, r.WithContext(arm.WithPrincipal(r.Context(), p)))
	})
}

func principalFromHeader(header string) arm.Principal {
	claims, ok := token.Parse(header)
	if !ok {
		return defaultPrincipal()
	}
	oid := claims.OID
	if oid == "" {
		oid = claims.Sub
	}
	if oid == "" {
		return defaultPrincipal()
	}
	tid := claims.TID
	if tid == "" {
		tid = token.DefaultTenantID
	}
	return arm.Principal{
		ObjectID: oid,
		TenantID: tid,
		AppID:    claims.AppID,
	}
}

func defaultPrincipal() arm.Principal {
	return arm.Principal{
		ObjectID: token.DefaultClientOID,
		TenantID: token.DefaultTenantID,
		AppID:    token.DefaultClientID,
	}
}

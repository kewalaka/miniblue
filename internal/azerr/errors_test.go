package azerr

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestError_Error(t *testing.T) {
	e := New(http.StatusBadRequest, "InvalidParameter", "bad %s %d", "value", 3)
	if got, want := e.Error(), "InvalidParameter: bad value 3"; got != want {
		t.Fatalf("Error() = %q, want %q", got, want)
	}
	if e.Status != http.StatusBadRequest {
		t.Fatalf("Status = %d", e.Status)
	}
}

func TestWrite_TargetAndDetails(t *testing.T) {
	err := &Error{
		Status:  http.StatusBadRequest,
		Code:    "InvalidParameter",
		Message: "outer",
		Target:  "properties.sku",
		Details: []Error{{Code: "InvalidSku", Message: "inner", Target: "sku.name"}},
	}
	rec := httptest.NewRecorder()
	Write(rec, err)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json; charset=utf-8" {
		t.Fatalf("Content-Type = %q", ct)
	}
	want := `{"error":{"code":"InvalidParameter","message":"outer","target":"properties.sku",` +
		`"details":[{"code":"InvalidSku","message":"inner","target":"sku.name"}]}}`
	if got := strings.TrimSpace(rec.Body.String()); got != want {
		t.Fatalf("body = %s\nwant   %s", got, want)
	}
}

func TestWrite_WrappedError(t *testing.T) {
	wrapped := fmt.Errorf("hook failed: %w", New(http.StatusConflict, "Conflict", "exists"))
	rec := httptest.NewRecorder()
	Write(rec, wrapped)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"code":"Conflict"`) {
		t.Fatalf("body = %s", rec.Body.String())
	}
}

func TestWrite_NonAzerrBecomes500(t *testing.T) {
	for name, err := range map[string]error{
		"plain": errors.New("disk on fire"),
		"nil":   nil,
	} {
		t.Run(name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			Write(rec, err)

			if rec.Code != http.StatusInternalServerError {
				t.Fatalf("status = %d", rec.Code)
			}
			body := rec.Body.String()
			if !strings.Contains(body, `"code":"InternalServerError"`) {
				t.Fatalf("body = %s", body)
			}
			if strings.Contains(body, "disk on fire") {
				t.Fatalf("internal error text leaked: %s", body)
			}
		})
	}
}

func TestWrite_ZeroStatusBecomes500(t *testing.T) {
	rec := httptest.NewRecorder()
	Write(rec, &Error{Code: "X", Message: "y"})
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d", rec.Code)
	}
}

// The legacy helpers must keep their exact wire shape: code and message only.
func TestHelpers_UnchangedResponses(t *testing.T) {
	tests := []struct {
		name   string
		call   func(http.ResponseWriter)
		status int
		body   string
	}{
		{"WriteError", func(w http.ResponseWriter) { WriteError(w, 418, "Teapot", "short and stout") },
			418, `{"error":{"code":"Teapot","message":"short and stout"}}`},
		{"NotFound", func(w http.ResponseWriter) { NotFound(w, "vaults", "kv1") },
			404, `{"error":{"code":"ResourceNotFound","message":"The Resource 'vaults/kv1' under resource group could not be found."}}`},
		{"BadRequest", func(w http.ResponseWriter) { BadRequest(w, "nope") },
			400, `{"error":{"code":"InvalidRequestContent","message":"nope"}}`},
		{"Conflict", func(w http.ResponseWriter) { Conflict(w, "vaults", "kv1") },
			409, `{"error":{"code":"Conflict","message":"The resource 'vaults/kv1' already exists."}}`},
		{"Gone", func(w http.ResponseWriter) { Gone(w, "vaults", "kv1") },
			410, `{"error":{"code":"ResourceGone","message":"The resource 'vaults/kv1' has been deleted."}}`},
		{"MethodNotAllowed", func(w http.ResponseWriter) { MethodNotAllowed(w, "TRACE") },
			405, `{"error":{"code":"MethodNotAllowed","message":"The request method 'TRACE' is not allowed."}}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			tt.call(rec)
			if rec.Code != tt.status {
				t.Fatalf("status = %d, want %d", rec.Code, tt.status)
			}
			if got := rec.Body.String(); got != tt.body+"\n" {
				t.Fatalf("body = %q, want %q", got, tt.body+"\n")
			}
		})
	}
}

package servicebus

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/moabukar/miniblue/internal/store"
)

// A persistent store carries a meta:schema stamp, which sorts before the sb:
// keys. Receive must not return it in place of a message.
func TestReceiveMessageIgnoresSchemaStamp(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	fb, err := store.NewFileBackend(path)
	if err != nil {
		t.Fatalf("open state file: %v", err)
	}
	st := store.NewWithBackend(fb)
	if err := store.CheckSchema(st, path); err != nil {
		t.Fatal(err)
	}
	r := chi.NewRouter()
	NewHandler(st).Register(r)

	do := func(method, url, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, url, strings.NewReader(body))
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec
	}

	base := "/servicebus/ns/queues/q"
	if rec := do(http.MethodPut, base, ""); rec.Code != http.StatusCreated {
		t.Fatalf("create queue: %d", rec.Code)
	}
	if rec := do(http.MethodPost, base+"/messages", `{"body":"hello"}`); rec.Code != http.StatusCreated {
		t.Fatalf("send: %d", rec.Code)
	}

	rec := do(http.MethodGet, base+"/messages/head", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("receive: %d, body %q", rec.Code, rec.Body.String())
	}
	var msg Message
	if err := json.Unmarshal(rec.Body.Bytes(), &msg); err != nil || msg.Body != "hello" {
		t.Errorf("receive returned %q (err %v), want the sent message", rec.Body.String(), err)
	}
}

package blob

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/moabukar/miniblue/internal/store"
	"github.com/moabukar/miniblue/internal/store/storetest"
)

func do(s *store.Store, method, path string, hdr map[string]string) *httptest.ResponseRecorder {
	r := chi.NewRouter()
	NewHandler(s).Register(r)
	req := httptest.NewRequest(method, path, strings.NewReader("hello"))
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// After a restart the store returns decoded maps, not Container/Blob structs.
// Blob content is not asserted: it is not persisted yet (Blob.Content is
// tagged json:"-").
func TestHandlersAfterRestart(t *testing.T) {
	s, restart := storetest.NewFile(t)
	const (
		account   = "/blob/acct"
		container = account + "/c1"
		blobPath  = container + "/dir/b1.txt"
	)
	if w := do(s, http.MethodPut, container, nil); w.Code != http.StatusCreated {
		t.Fatalf("create container: %d %s", w.Code, w.Body)
	}
	w := do(s, http.MethodPut, blobPath, map[string]string{"x-ms-blob-content-type": "text/plain", "x-ms-meta-k": "v1"})
	if w.Code != http.StatusCreated {
		t.Fatalf("upload: %d %s", w.Code, w.Body)
	}

	s = restart()

	steps := []struct {
		name     string
		method   string
		path     string
		hdr      map[string]string
		wantCode int
		wantBody string
		wantHdr  map[string]string
	}{
		{name: "list containers", method: http.MethodGet, path: account + "?comp=list", wantCode: 200, wantBody: "<Name>c1</Name>"},
		{name: "list blobs", method: http.MethodGet, path: container, wantCode: 200, wantBody: "dir/b1.txt"},
		{name: "head", method: http.MethodHead, path: blobPath, wantCode: 200,
			wantHdr: map[string]string{"Content-Type": "text/plain", "x-ms-meta-k": "v1"}},
		{name: "get", method: http.MethodGet, path: blobPath, wantCode: 200,
			wantHdr: map[string]string{"Content-Type": "text/plain", "x-ms-meta-k": "v1"}},
		{name: "set metadata", method: http.MethodPut, path: blobPath + "?comp=metadata",
			hdr: map[string]string{"x-ms-meta-k": "v2"}, wantCode: 200},
		{name: "head after metadata", method: http.MethodHead, path: blobPath, wantCode: 200,
			wantHdr: map[string]string{"x-ms-meta-k": "v2"}},
		{name: "set properties", method: http.MethodPut, path: blobPath + "?comp=properties",
			hdr: map[string]string{"x-ms-blob-content-type": "application/json"}, wantCode: 200},
		{name: "head after properties", method: http.MethodHead, path: blobPath, wantCode: 200,
			wantHdr: map[string]string{"Content-Type": "application/json", "x-ms-meta-k": "v2"}},
		{name: "copy", method: http.MethodPut, path: container + "/copy.txt",
			hdr: map[string]string{"x-ms-copy-source": "http://localhost/blob/acct/c1/dir/b1.txt"}, wantCode: 202,
			wantHdr: map[string]string{"x-ms-copy-status": "success"}},
		{name: "head copy", method: http.MethodHead, path: container + "/copy.txt", wantCode: 200,
			wantHdr: map[string]string{"Content-Type": "application/json", "x-ms-meta-k": "v2"}},
	}
	for _, st := range steps {
		t.Run(st.name, func(t *testing.T) {
			w := do(s, st.method, st.path, st.hdr)
			if w.Code != st.wantCode {
				t.Fatalf("status %d, want %d: %s", w.Code, st.wantCode, w.Body)
			}
			if !strings.Contains(w.Body.String(), st.wantBody) {
				t.Errorf("body %q missing %q", w.Body, st.wantBody)
			}
			for k, v := range st.wantHdr {
				if got := w.Header().Get(k); got != v {
					t.Errorf("header %s = %q, want %q", k, got, v)
				}
			}
		})
	}
}

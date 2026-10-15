package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

// ---------- trie unit tests ----------

func newTrieFromPatterns(patterns ...string) *routeTrie {
	t := newRouteTrie()
	for _, p := range patterns {
		t.insert(p)
	}
	return t
}

func TestTrie_NormalizeMixedCase(t *testing.T) {
	trie := newTrieFromPatterns(
		"/subscriptions/{sub}/resourceGroups/{rg}/providers/Microsoft.Network/virtualNetworks/{n}",
	)
	r := trie.normalize("/subscriptions/abc/RESOURCEGROUPS/MyRG/providers/microsoft.network/VIRTUALNETWORKS/vnet1")
	if !r.matched {
		t.Fatal("expected match")
	}
	want := "/subscriptions/abc/resourceGroups/MyRG/providers/Microsoft.Network/virtualNetworks/vnet1"
	if r.rewrittenPath != want {
		t.Fatalf("rewritten=%q want %q", r.rewrittenPath, want)
	}
	// MyRG and vnet1 are param values: they must not appear as case pairs.
	for _, p := range r.pairs {
		if p[0] == "MyRG" || p[0] == "vnet1" {
			t.Fatalf("value segment recorded as literal pair: %v", p)
		}
	}
	// Expect canonical->client pairs for the three literals.
	expectPair(t, r.pairs, "resourceGroups", "RESOURCEGROUPS")
	expectPair(t, r.pairs, "Microsoft.Network", "microsoft.network")
	expectPair(t, r.pairs, "virtualNetworks", "VIRTUALNETWORKS")
}

func TestTrie_PicksCaseThatRoutes(t *testing.T) {
	// Two routes share the same literal position with different cases.
	// Each has a different sub-tree. The trie must pick the casing that
	// leads to a complete match for the requested path.
	trie := newTrieFromPatterns(
		"/subscriptions/{sub}/resourcegroups/{rg}",
		"/subscriptions/{sub}/resourceGroups/{rg}/providers/Microsoft.Network/virtualNetworks/{n}",
	)

	// Bare resource-group request must rewrite to the lowercase route.
	r := trie.normalize("/subscriptions/abc/RESOURCEGROUPS/MyRG")
	if !r.matched {
		t.Fatal("expected match for bare RG")
	}
	if r.rewrittenPath != "/subscriptions/abc/resourcegroups/MyRG" {
		t.Fatalf("rewritten=%q", r.rewrittenPath)
	}

	// Provider request must rewrite to the camelCase route.
	r = trie.normalize("/subscriptions/abc/resourcegroups/MyRG/providers/microsoft.network/virtualnetworks/v1")
	if !r.matched {
		t.Fatal("expected match for vnet route")
	}
	want := "/subscriptions/abc/resourceGroups/MyRG/providers/Microsoft.Network/virtualNetworks/v1"
	if r.rewrittenPath != want {
		t.Fatalf("rewritten=%q want %q", r.rewrittenPath, want)
	}
}

func TestTrie_UnmatchedPathReturnsOriginal(t *testing.T) {
	trie := newTrieFromPatterns("/subscriptions/{sub}/resourcegroups/{rg}")
	r := trie.normalize("/no/such/route")
	if r.matched {
		t.Fatal("should not match")
	}
	if r.rewrittenPath != "/no/such/route" {
		t.Fatalf("expected unchanged path on miss, got %q", r.rewrittenPath)
	}
}

func TestTrie_Wildcard(t *testing.T) {
	trie := newTrieFromPatterns("/subscriptions/{sub}/operationresults/*")
	r := trie.normalize("/subscriptions/x/OPERATIONRESULTS/some/long/tail/here")
	if !r.matched {
		t.Fatal("expected wildcard match")
	}
	want := "/subscriptions/x/operationresults/some/long/tail/here"
	if r.rewrittenPath != want {
		t.Fatalf("rewritten=%q want %q", r.rewrittenPath, want)
	}
}

func TestTrie_ParamValuePreserved(t *testing.T) {
	trie := newTrieFromPatterns("/subscriptions/{sub}/resourcegroups/{rg}")
	r := trie.normalize("/subscriptions/My-Sub-ID/resourcegroups/My-RG-Name")
	if !r.matched {
		t.Fatal("expected match")
	}
	if r.rewrittenPath != "/subscriptions/My-Sub-ID/resourcegroups/My-RG-Name" {
		t.Fatalf("value segments must be preserved: %q", r.rewrittenPath)
	}
}

func expectPair(t *testing.T, pairs [][2]string, canonical, original string) {
	t.Helper()
	for _, p := range pairs {
		if p[0] == canonical && p[1] == original {
			return
		}
	}
	t.Fatalf("missing pair (%q,%q) in %v", canonical, original, pairs)
}

// ---------- ARM path mirroring unit tests ----------

func TestReplaceSegment(t *testing.T) {
	cases := []struct {
		in, old, new, want string
	}{
		{"/a/resourceGroups/x", "resourceGroups", "RESOURCEGROUPS", "/a/RESOURCEGROUPS/x"},
		{"/a/resourceGroups?api-version=1", "resourceGroups", "RESOURCEGROUPS", "/a/RESOURCEGROUPS?api-version=1"},
		{"/a/resourceGroups#frag", "resourceGroups", "RESOURCEGROUPS", "/a/RESOURCEGROUPS#frag"},
		{"/a/resourceGroups", "resourceGroups", "RESOURCEGROUPS", "/a/RESOURCEGROUPS"},
		// Must NOT match when it is only part of a longer segment.
		{"/a/xresourceGroups/y", "resourceGroups", "RESOURCEGROUPS", "/a/xresourceGroups/y"},
		{"/a/resourceGroupsy/y", "resourceGroups", "RESOURCEGROUPS", "/a/resourceGroupsy/y"},
		{"/a/resourceGroups.v2/y", "resourceGroups", "RESOURCEGROUPS", "/a/resourceGroups.v2/y"},
		// Must NOT match when it is not a whole segment.
		{"/a/b-resourceGroups/y", "resourceGroups", "RESOURCEGROUPS", "/a/b-resourceGroups/y"},
		// Dotted RP namespace as a single segment.
		{"/providers/Microsoft.Network/", "Microsoft.Network", "microsoft.network", "/providers/microsoft.network/"},
		{"/providers/Microsoft.Network/", "Microsoft", "MICROSOFT", "/providers/Microsoft.Network/"},
	}
	for _, c := range cases {
		if got := replaceSegment(c.in, c.old, c.new); got != c.want {
			t.Errorf("replaceSegment(%q,%q,%q)=%q want %q", c.in, c.old, c.new, got, c.want)
		}
	}
}

func TestMirrorJSON(t *testing.T) {
	pairs := [][2]string{{"resourceGroups", "RESOURCEGROUPS"}}
	cases := []struct {
		name, in, want string
	}{
		{"user data is untouched", `{"tags":{"resourceGroups":"resourceGroups"},"n":"a/resourceGroups/b"}`,
			`{"tags":{"resourceGroups":"resourceGroups"},"n":"a/resourceGroups/b"}`},
		{"values and keys", ` { "/s/resourceGroups/k" : "/s/resourceGroups/v" } `,
			` { "/s/RESOURCEGROUPS/k" : "/s/RESOURCEGROUPS/v" } `},
		{"absolute URL", `"https://h/s/resourceGroups/x?a=1"`, `"https://h/s/RESOURCEGROUPS/x?a=1"`},
		{"escaped quote does not desync the scanner", `{"a":"say \"hi\" /resourceGroups","id":"/s/resourceGroups/x"}`,
			`{"a":"say \"hi\" /resourceGroups","id":"/s/RESOURCEGROUPS/x"}`},
		{"escapes are preserved", `"https://h/s/resourceGroups/x?a=1\u0026b=2"`, `"https://h/s/RESOURCEGROUPS/x?a=1\u0026b=2"`},
		{"escaped slashes", `"\/s\/resourceGroups\/x"`, `"/s/RESOURCEGROUPS/x"`},
		{"unterminated trailing literal", `{"id":"/s/resourceGroups/x","b":"/s/resourceGroups/y`,
			`{"id":"/s/RESOURCEGROUPS/x","b":"/s/resourceGroups/y`},
		{"not JSON", "one /resourceGroups/x\n", "one /resourceGroups/x\n"},
	}
	for _, c := range cases {
		if got := string(mirrorJSON([]byte(c.in), pairs)); got != c.want {
			t.Errorf("%s: mirrorJSON(%s)=%s want %s", c.name, c.in, got, c.want)
		}
	}
}

// ---------- end-to-end with caseMirrorWriter ----------

func TestMiddleware_BodyMirrorsClientCase(t *testing.T) {
	// Build a small chi router so the trie has registered routes.
	r := chi.NewRouter()
	r.Put("/subscriptions/{subscriptionId}/resourceGroups/{rg}", func(w http.ResponseWriter, r *http.Request) {
		// Handler always emits canonical case in the id.
		w.Header().Set("Location", "/subscriptions/abc/resourceGroups/x/operationresults/123")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"/subscriptions/abc/resourceGroups/x"}`))
	})
	trie := BuildRouteTrie(r)
	h := CaseInsensitiveARM(trie)(r)

	req := httptest.NewRequest(http.MethodPut,
		"/subscriptions/abc/RESOURCEGROUPS/x?api-version=2023-07-01", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status=%d", rec.Code)
	}
	wantBody := `{"id":"/subscriptions/abc/RESOURCEGROUPS/x"}`
	if rec.Body.String() != wantBody {
		t.Fatalf("body=%q want %q", rec.Body.String(), wantBody)
	}
	wantLoc := "/subscriptions/abc/RESOURCEGROUPS/x/operationresults/123"
	if rec.Header().Get("Location") != wantLoc {
		t.Fatalf("Location=%q want %q", rec.Header().Get("Location"), wantLoc)
	}
}

// Mirroring only rewrites ARM paths: user data that merely equals an ARM
// literal (tag keys/values, settings) must round-trip untouched, while IDs,
// ARM-ID map keys and the async headers still echo the client's casing.
func TestMiddleware_MirrorsOnlyARMPaths(t *testing.T) {
	r := chi.NewRouter()
	r.Get("/subscriptions/{sub}/resourceGroups/{rg}", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Location", "https://localhost:4566/subscriptions/abc/resourceGroups/x/operationresults/1?api-version=1")
		w.Header().Set("Azure-AsyncOperation", "/subscriptions/abc/resourceGroups/x/operationresults/1")
		_, _ = w.Write([]byte(`{"id":"/subscriptions/abc/resourceGroups/x",` +
			`"name":"resourceGroups",` +
			`"tags":{"resourceGroups":"resourceGroups","providers":"Microsoft.Network"},` +
			`"properties":{"settings":{"path":"a/resourceGroups/b","note":"resourceGroups"},` +
			`"vnet":{"id":"/subscriptions/abc/resourceGroups/x/providers/Microsoft.Network/virtualNetworks/v"}},` +
			`"identity":{"userAssignedIdentities":{"/subscriptions/abc/resourceGroups/x/providers/Microsoft.ManagedIdentity/userAssignedIdentities/i":{}}},` +
			`"nextLink":"https://localhost:4566/subscriptions/abc/resourceGroups/x/vms?a=1\u0026b=2"}`))
	})
	h := CaseInsensitiveARM(BuildRouteTrie(r))(r)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/subscriptions/abc/RESOURCEGROUPS/x", nil))

	want := `{"id":"/subscriptions/abc/RESOURCEGROUPS/x",` +
		`"name":"resourceGroups",` +
		`"tags":{"resourceGroups":"resourceGroups","providers":"Microsoft.Network"},` +
		`"properties":{"settings":{"path":"a/resourceGroups/b","note":"resourceGroups"},` +
		`"vnet":{"id":"/subscriptions/abc/RESOURCEGROUPS/x/providers/Microsoft.Network/virtualNetworks/v"}},` +
		`"identity":{"userAssignedIdentities":{"/subscriptions/abc/RESOURCEGROUPS/x/providers/Microsoft.ManagedIdentity/userAssignedIdentities/i":{}}},` +
		`"nextLink":"https://localhost:4566/subscriptions/abc/RESOURCEGROUPS/x/vms?a=1\u0026b=2"}`
	if got := rec.Body.String(); got != want {
		t.Fatalf("body=%s\nwant=%s", got, want)
	}
	if got, want := rec.Header().Get("Location"), "https://localhost:4566/subscriptions/abc/RESOURCEGROUPS/x/operationresults/1?api-version=1"; got != want {
		t.Fatalf("Location=%q want %q", got, want)
	}
	if got, want := rec.Header().Get("Azure-AsyncOperation"), "/subscriptions/abc/RESOURCEGROUPS/x/operationresults/1"; got != want {
		t.Fatalf("Azure-AsyncOperation=%q want %q", got, want)
	}
}

func TestMiddleware_NoRewriteWhenAlreadyCanonical(t *testing.T) {
	r := chi.NewRouter()
	r.Get("/subscriptions/{sub}/resourceGroups/{rg}", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"id":"/subscriptions/abc/resourceGroups/x"}`))
	})
	h := CaseInsensitiveARM(BuildRouteTrie(r))(r)

	req := httptest.NewRequest(http.MethodGet, "/subscriptions/abc/resourceGroups/x", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d", rec.Code)
	}
	if rec.Body.String() != `{"id":"/subscriptions/abc/resourceGroups/x"}` {
		t.Fatalf("body=%q", rec.Body.String())
	}
}

func TestMiddleware_DoesNotNormalizeBlobDataPlanePath(t *testing.T) {
	r := chi.NewRouter()
	r.Get("/blob/{accountName}", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	h := CaseInsensitiveARM(BuildRouteTrie(r))(r)

	req := httptest.NewRequest(http.MethodGet, "/Blob/acct?comp=properties&restype=service", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("Blob data-plane path was normalized; status=%d", rec.Code)
	}
}

func TestMiddleware_PanicAllowsOuterRecover(t *testing.T) {
	// If the handler panics before writing, the wrapper must not flush a
	// premature WriteHeader so an outer recover middleware can write its own.
	router := chi.NewRouter()
	router.Get("/subscriptions/{sub}/resourceGroups/{rg}", func(w http.ResponseWriter, r *http.Request) {
		panic("boom")
	})
	trie := BuildRouteTrie(router)
	inner := CaseInsensitiveARM(trie)(router)

	recovered := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rv := recover(); rv != nil {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte(`{"error":"recovered"}`))
			}
		}()
		inner.ServeHTTP(w, r)
	})

	req := httptest.NewRequest(http.MethodGet,
		"/subscriptions/abc/RESOURCEGROUPS/x", nil)
	rec := httptest.NewRecorder()
	recovered.ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 from recover, got %d", rec.Code)
	}
	if rec.Body.String() != `{"error":"recovered"}` {
		t.Fatalf("body=%q", rec.Body.String())
	}
}

// Streaming handlers (e.g. vm `logs?follow=true`) require http.Flusher and
// must see their chunks reach the client before the handler returns. Two
// registered casings of the same literal force the response wrapper on even
// when the client sends the canonical form.
func TestMiddleware_StreamingFlushNotBuffered(t *testing.T) {
	r := chi.NewRouter()
	rec := httptest.NewRecorder()
	r.Get("/subscriptions/{sub}/resourcegroups/{rg}/logs", func(w http.ResponseWriter, _ *http.Request) {
		f, ok := w.(http.Flusher)
		if !ok {
			t.Error("wrapped writer must implement http.Flusher")
			return
		}
		_, _ = w.Write([]byte(`{"id":"/resourcegroups/x"}` + "\n"))
		f.Flush()
		if rec.Body.Len() == 0 {
			t.Error("first chunk was not streamed before handler returned")
		}
		_, _ = w.Write([]byte(`{"id":"/resourcegroups/y"}` + "\n"))
		f.Flush()
	})
	r.Get("/subscriptions/{sub}/resourceGroups/{rg}/other", func(http.ResponseWriter, *http.Request) {})
	h := CaseInsensitiveARM(BuildRouteTrie(r))(r)

	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/subscriptions/s/resourceGroups/x/logs", nil))

	if rec.Code != http.StatusOK || !rec.Flushed {
		t.Fatalf("status=%d flushed=%v", rec.Code, rec.Flushed)
	}
	want := `{"id":"/resourceGroups/x"}` + "\n" + `{"id":"/resourceGroups/y"}` + "\n"
	if rec.Body.String() != want {
		t.Fatalf("body=%q want %q", rec.Body.String(), want)
	}
}

// Bodies declared as something other than JSON (container logs, blobs) are
// opaque data and must not be rewritten.
func TestMiddleware_NonJSONBodyUntouched(t *testing.T) {
	r := chi.NewRouter()
	r.Get("/subscriptions/{sub}/resourceGroups/{rg}", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte(`"/subscriptions/abc/resourceGroups/x"`))
	})
	h := CaseInsensitiveARM(BuildRouteTrie(r))(r)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/subscriptions/abc/RESOURCEGROUPS/x", nil))
	if got, want := rec.Body.String(), `"/subscriptions/abc/resourceGroups/x"`; got != want {
		t.Fatalf("body=%q want %q", got, want)
	}
}

func TestRouter_MethodNotAllowedStays405(t *testing.T) {
	r := chi.NewRouter()
	r.Get("/subscriptions/{sub}/resourceGroups/{rg}", func(http.ResponseWriter, *http.Request) {})
	r.NotFound(armNotFound)
	r.MethodNotAllowed(armMethodNotAllowed)
	h := CaseInsensitiveARM(BuildRouteTrie(r))(r)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/subscriptions/s/RESOURCEGROUPS/x", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status=%d want 405", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"MethodNotAllowed"`) {
		t.Fatalf("body=%q", rec.Body.String())
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/subscriptions/s/nope/x", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status=%d want 404", rec.Code)
	}
}

package server

import (
	"net/http"
	"net/http/httptest"
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

// ---------- boundaryReplace unit tests ----------

func TestBoundaryReplace(t *testing.T) {
	cases := []struct {
		in, old, new, want string
	}{
		{`{"id":"/a/resourceGroups/x"}`, "resourceGroups", "RESOURCEGROUPS",
			`{"id":"/a/RESOURCEGROUPS/x"}`},
		{"/a/resourceGroups?api-version=1", "resourceGroups", "RESOURCEGROUPS",
			"/a/RESOURCEGROUPS?api-version=1"},
		// Must NOT match inside an identifier (no boundary).
		{"xresourceGroupsy", "resourceGroups", "RESOURCEGROUPS", "xresourceGroupsy"},
		// Dotted RP namespace as a single token.
		{"/providers/Microsoft.Network/", "Microsoft.Network", "microsoft.network",
			"/providers/microsoft.network/"},
		// `Microsoft` alone shouldn't match inside `Microsoft.Network`.
		{"/providers/Microsoft.Network/", "Microsoft", "MICROSOFT",
			"/providers/Microsoft.Network/"},
	}
	for _, c := range cases {
		got := boundaryReplace(c.in, c.old, c.new)
		if got != c.want {
			t.Errorf("boundaryReplace(%q,%q,%q)=%q want %q", c.in, c.old, c.new, got, c.want)
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

// Package server: ARM URL case-insensitive routing & response casing mirror.
//
// Background: the Azure Resource Manager spec mandates URLs be case-insensitive
// (https://github.com/Azure/azure-resource-manager-rpc/blob/master/v1.0/common-api-details.md).
// chi (and most Go routers) match path segments case-sensitively. The
// hashicorp/azurerm v4 provider hard-codes canonical camelCase segments
// (`resourceGroups`, `providers/Microsoft.Network/virtualNetworks`, ...), so a
// strictly case-sensitive router rejects those requests with 404.
//
// Solution: a fully generic, two-part middleware:
//
//   - REQUEST side: a trie built once at startup from chi's registered route
//     patterns learns the canonical case for every literal segment at every
//     URL position. The middleware walks the request path against the trie
//     and rewrites each literal segment to the canonical case. Value/name
//     segments (matched by chi parameters or wildcards) are preserved.
//
//   - RESPONSE side: the writer is wrapped to buffer the body and intercept
//     the `Location` and `Azure-AsyncOperation` headers. Whenever the
//     normalizer rewrote a literal segment, the wrapper substitutes the
//     canonical literal back to the case the client originally sent — at
//     boundary-safe positions only — so JSON `id` fields, nested IDs, and
//     async-op URLs echo back exactly the casing the client used.
//
// This is registered as router-level middleware, so it applies globally to
// every endpoint without any per-handler changes.

package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
)

// ----- trie -----

// routeTrie indexes the literal segments of all registered routes by their
// position so an incoming request can be normalized to one of the registered
// casings. The same logical literal can be registered under more than one
// case (e.g. some handlers use `resourcegroups`, others use `resourceGroups`),
// so each node keeps every distinct case alongside its sub-tree, and matching
// picks whichever variant leads to a full route that chi will accept.
type routeTrie struct {
	// literals holds every registered case for this position. Multiple
	// entries may share the same lowercased key but differ in canonical case.
	literals []*literalEntry
	// param is followed when no literal matches (chi {param}).
	param *routeTrie
	// wildcard is true when chi's `*` rest-matcher was registered here. A
	// wildcard node accepts any remaining path tail and is itself terminal.
	wildcard bool
	// terminal indicates a route ends at this node.
	terminal bool
}

type literalEntry struct {
	canonical string // original-case literal as registered with chi
	child     *routeTrie
}

func newRouteTrie() *routeTrie {
	return &routeTrie{}
}

// insert adds a chi pattern (e.g. "/subscriptions/{subscriptionId}/resourceGroups/{name}")
// into the trie.
func (t *routeTrie) insert(pattern string) {
	parts := splitPath(pattern)
	node := t
	for _, p := range parts {
		switch {
		case p == "*":
			node.wildcard = true
			return
		case isParamSegment(p):
			if node.param == nil {
				node.param = newRouteTrie()
			}
			node = node.param
		default:
			node = node.addLiteral(p)
		}
	}
	node.terminal = true
}

// addLiteral inserts a literal child preserving its canonical case. If a
// literal with this exact case is already present the existing child is
// reused so subtrees from multiple route registrations are merged.
func (t *routeTrie) addLiteral(canonical string) *routeTrie {
	for _, lit := range t.literals {
		if lit.canonical == canonical {
			return lit.child
		}
	}
	child := newRouteTrie()
	t.literals = append(t.literals, &literalEntry{canonical: canonical, child: child})
	return child
}

// isParamSegment reports whether a chi pattern segment is a parameter
// (matches any value rather than a specific string).
func isParamSegment(seg string) bool {
	// chi params look like {name} or {name:regex}.
	return len(seg) >= 2 && seg[0] == '{' && seg[len(seg)-1] == '}'
}

// splitPath splits a URL path on "/" and drops the empty leading element, so
// "/a/b/c" -> ["a","b","c"]. An empty path or "/" yields nil.
func splitPath(p string) []string {
	if p == "" || p == "/" {
		return nil
	}
	if p[0] == '/' {
		p = p[1:]
	}
	if p == "" {
		return nil
	}
	parts := strings.Split(p, "/")
	// Trim trailing empty caused by trailing slash.
	if len(parts) > 0 && parts[len(parts)-1] == "" {
		parts = parts[:len(parts)-1]
	}
	return parts
}

// ----- normalization -----

// normalizationResult holds the rewritten request path plus the per-segment
// (canonical, original) pairs needed to mirror the client's casing back into
// the response. Only literals where canonical != original are recorded.
type normalizationResult struct {
	rewrittenPath string
	pairs         [][2]string
	matched       bool
}

// normalize walks the request path against the trie. For each segment it
// prefers a literal child whose lowercased value equals the segment's,
// choosing the case for which the rest of the path can also be routed.
// Value segments (chi params/wildcards) are kept as the client sent them.
//
// If no full match exists (the route isn't registered) the original path is
// returned unchanged so chi can reply with the standard 404.
func (t *routeTrie) normalize(path string) normalizationResult {
	parts := splitPath(path)
	if len(parts) == 0 {
		return normalizationResult{rewrittenPath: path, matched: t.terminal}
	}

	out := make([]string, 0, len(parts))
	pairs := make([][2]string, 0)
	if !t.match(parts, &out, &pairs) {
		return normalizationResult{rewrittenPath: path, matched: false}
	}

	rewritten := "/" + strings.Join(out, "/")
	if strings.HasSuffix(path, "/") && !strings.HasSuffix(rewritten, "/") {
		rewritten += "/"
	}
	return normalizationResult{rewrittenPath: rewritten, pairs: pairs, matched: true}
}

// match recursively walks the trie. On success it appends rewritten segments
// to *out and recorded (canonical, original) pairs to *pairs.
//
// We try literal children first (longest-match-by-specificity in chi's own
// routing model), then fall through to a param child, then to a wildcard.
// On wildcard match, all remaining segments are emitted unchanged and the
// recursion terminates successfully.
//
// For each literal position we additionally emit a (canonical, client) pair
// for every alternate registered casing at that position. This way, if a
// handler hardcodes a casing that differs from what the client sent (a
// common situation given that route registrations across the codebase mix
// `resourcegroups` and `resourceGroups`), the response wrapper will still
// rewrite it back to the client's casing.
func (t *routeTrie) match(segs []string, out *[]string, pairs *[][2]string) bool {
	if t.wildcard {
		// Wildcard consumes the rest of the path verbatim.
		*out = append(*out, segs...)
		return true
	}
	if len(segs) == 0 {
		return t.terminal
	}
	seg := segs[0]

	// Try every registered literal case.
	for _, lit := range t.literals {
		if !strings.EqualFold(lit.canonical, seg) {
			continue
		}
		// Take a snapshot so we can roll back on failure.
		outLen, pairLen := len(*out), len(*pairs)
		*out = append(*out, lit.canonical)
		// Record every alternate registered casing at this position. This
		// guards against handlers that hardcode a particular casing in
		// response bodies regardless of what the client sent.
		for _, alt := range t.literals {
			if !strings.EqualFold(alt.canonical, seg) {
				continue
			}
			if alt.canonical != seg {
				*pairs = append(*pairs, [2]string{alt.canonical, seg})
			}
		}
		if lit.child.match(segs[1:], out, pairs) {
			return true
		}
		*out = (*out)[:outLen]
		*pairs = (*pairs)[:pairLen]
	}

	// Param child: keep the segment as-is.
	if t.param != nil {
		outLen, pairLen := len(*out), len(*pairs)
		*out = append(*out, seg)
		if t.param.match(segs[1:], out, pairs) {
			return true
		}
		*out = (*out)[:outLen]
		*pairs = (*pairs)[:pairLen]
	}

	return false
}

// ----- middleware -----

// CaseInsensitiveARM returns middleware that normalizes the incoming request
// path to one of the canonical casings learned from chi's registered routes,
// then wraps the response so any canonical literals in the body / Location
// header are rewritten back to the case the client originally used.
//
// Resource name segments (anything that lands on a chi parameter or wildcard)
// are passed through untouched, so case-sensitive storage keys still work.
func CaseInsensitiveARM(trie *routeTrie) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if isBlobDataPlanePath(r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}

			res := trie.normalize(r.URL.Path)

			if res.matched && res.rewrittenPath != r.URL.Path {
				// Mutate URL.Path so chi sees one of the registered casings.
				// We only touch Path; chi uses Path (not RawPath) for matching.
				r.URL.Path = res.rewrittenPath
			}

			if len(res.pairs) > 0 {
				// Wrap the writer to mirror the client's casing back into the
				// response body and Location-style headers.
				cw := &caseMirrorWriter{
					ResponseWriter: w,
					pairs:          res.pairs,
					buf:            &bytes.Buffer{},
				}
				defer cw.flush()
				w = cw
			}

			next.ServeHTTP(w, r)
		})
	}
}

func isBlobDataPlanePath(path string) bool {
	parts := splitPath(path)
	return len(parts) > 0 && strings.EqualFold(parts[0], "blob")
}

// ----- response writer wrapper -----

// caseMirrorWriter buffers the response so we can rewrite literal segments
// back to the client's original casing on flush. Headers are rewritten in
// place via WriteHeader.
type caseMirrorWriter struct {
	http.ResponseWriter
	pairs       [][2]string
	buf         *bytes.Buffer
	status      int
	wroteHeader bool
	flushed     bool
}

func (w *caseMirrorWriter) WriteHeader(status int) {
	if w.wroteHeader {
		return
	}
	w.wroteHeader = true
	w.status = status
	// Mirror Location-style headers immediately so the client sees the
	// expected case even if the body is empty.
	for _, h := range []string{"Location", "Azure-AsyncOperation"} {
		if v := w.ResponseWriter.Header().Get(h); v != "" {
			w.ResponseWriter.Header().Set(h, mirrorString(v, w.pairs))
		}
	}
	// Defer the actual WriteHeader until flush so we can fix Content-Length
	// after rewriting the body (the rewritten body may differ in length when
	// canonical and client casings have different lengths, e.g. "Microsoft"
	// vs "MICROSOFT" — same length here but in general we cannot assume).
}

func (w *caseMirrorWriter) Write(p []byte) (int, error) {
	if !w.wroteHeader {
		// Mirror the implicit 200 path through WriteHeader for header rewriting.
		w.WriteHeader(http.StatusOK)
	}
	return w.buf.Write(p)
}

// flush writes the buffered, case-mirrored body to the underlying writer.
// Safe to call multiple times. If the wrapped handler never wrote anything
// (e.g. it panicked before producing a response), this is a no-op so that
// outer middleware like safeRecover can still emit its own 500 response.
func (w *caseMirrorWriter) flush() {
	if w.flushed {
		return
	}
	w.flushed = true

	if !w.wroteHeader {
		// Handler produced nothing; leave the underlying writer untouched.
		return
	}

	body := w.buf.Bytes()
	if len(body) > 0 {
		body = mirrorBytes(body, w.pairs)
		// Drop any pre-existing Content-Length: handlers like json.Encoder
		// don't usually set one, but if anything does, our rewrite may
		// invalidate it.
		w.ResponseWriter.Header().Del("Content-Length")
	}
	w.ResponseWriter.WriteHeader(w.status)
	if len(body) > 0 {
		_, _ = w.ResponseWriter.Write(body)
	}
}

// Header forwards to the underlying writer so handlers can set headers as usual.
func (w *caseMirrorWriter) Header() http.Header {
	return w.ResponseWriter.Header()
}

// ----- string rewriting -----

// mirrorString applies all (canonical, original) substitutions to s at safe
// boundary positions. A boundary is any non-letter/-digit character or the
// end of the string. This avoids matching canonical literals that happen to
// appear inside larger identifiers (e.g. a tag value).
func mirrorString(s string, pairs [][2]string) string {
	for _, p := range pairs {
		canon, orig := p[0], p[1]
		if canon == orig || canon == "" {
			continue
		}
		s = boundaryReplace(s, canon, orig)
	}
	return s
}

func mirrorBytes(b []byte, pairs [][2]string) []byte {
	// Operate on a string to keep replacement logic in one place; payloads
	// are typically small JSON bodies so the allocation cost is acceptable.
	return []byte(mirrorString(string(b), pairs))
}

// boundaryReplace replaces every occurrence of `old` in s with `new` provided
// the match is bordered on the left and right by something that cannot extend
// an ARM URL segment — i.e. not an ASCII letter, digit, '-', '_' or '.'.
//
// "." is treated as part of an identifier so that `Microsoft.Network` matches
// when prefixed by `/` and followed by `/` (without the dot rule, the `Microsoft`
// portion of `Microsoft.Network` would match independently). The full
// dotted RP namespace is registered as a single segment by chi, so it always
// arrives here as a single token surrounded by `/`.
func boundaryReplace(s, old, new string) string {
	if old == "" {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	i := 0
	for i < len(s) {
		j := strings.Index(s[i:], old)
		if j < 0 {
			b.WriteString(s[i:])
			break
		}
		start := i + j
		end := start + len(old)
		// Boundary checks.
		leftOK := start == 0 || !isIdentRune(s[start-1])
		rightOK := end == len(s) || !isIdentRune(s[end])
		b.WriteString(s[i:start])
		if leftOK && rightOK {
			b.WriteString(new)
		} else {
			b.WriteString(old)
		}
		i = end
	}
	return b.String()
}

func isIdentRune(c byte) bool {
	switch {
	case c >= 'a' && c <= 'z':
		return true
	case c >= 'A' && c <= 'Z':
		return true
	case c >= '0' && c <= '9':
		return true
	case c == '-' || c == '_' || c == '.':
		return true
	}
	return false
}

// ----- trie construction from chi -----

// BuildRouteTrie walks the chi router and returns a trie carrying every
// registered casing of every literal segment in every route pattern. It is
// safe to call after all routes have been registered.
func BuildRouteTrie(r chi.Router) *routeTrie {
	t := newRouteTrie()
	_ = chi.Walk(r, func(method, route string, handler http.Handler, middlewares ...func(http.Handler) http.Handler) error {
		t.insert(route)
		return nil
	})
	return t
}

// ----- ARM JSON 404 -----

// armNotFound returns the standard ARM error envelope for unmatched routes.
// The `azurerm` Terraform provider relies on this shape to surface a typed
// error rather than the raw "404 page not found" string.
func armNotFound(w http.ResponseWriter, r *http.Request) {
	parts := splitPath(r.URL.Path)
	segment := ""
	rp := ""
	for i, p := range parts {
		if strings.EqualFold(p, "providers") && i+1 < len(parts) {
			rp = parts[i+1]
			if i+2 < len(parts) {
				segment = parts[i+2]
			}
			break
		}
	}
	if segment == "" && len(parts) > 0 {
		segment = parts[len(parts)-1]
	}
	apiVersion := r.URL.Query().Get("api-version")
	msg := fmt.Sprintf(
		"The resource type '%s' could not be found in the namespace '%s' for api version '%s'.",
		segment, rp, apiVersion,
	)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusNotFound)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]any{
			"code":    "InvalidResourceType",
			"message": msg,
		},
	})
}

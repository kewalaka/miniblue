package server

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/moabukar/miniblue/internal/arm"
)

// checkEngineOverlap walks every chi route at boot and panics if any route
// overlaps a resource type already registered on the ARM engine.
func checkEngineOverlap(r chi.Router, reg *arm.Registry) {
	seen := make(map[string]struct{})
	_ = chi.Walk(r, func(_, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if _, dup := seen[route]; dup {
			return nil
		}
		seen[route] = struct{}{}

		id, err := arm.ParseID(sampleRoutePath(route))
		if err != nil {
			return nil
		}
		if t, ok := reg.Lookup(id); ok {
			panic(fmt.Sprintf("server: chi route %q overlaps registered ARM type %q", route, t.Name))
		}
		return nil
	})
}

// sampleRoutePath replaces chi path parameters with sample values and drops
// trailing wildcards so arm.ParseID can classify the route pattern.
func sampleRoutePath(route string) string {
	parts := splitPath(route)
	if len(parts) == 0 {
		return "/"
	}
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p == "*" {
			break
		}
		if isParamSegment(p) {
			out = append(out, "sample")
			continue
		}
		out = append(out, p)
	}
	if len(out) == 0 {
		return "/"
	}
	return "/" + strings.Join(out, "/")
}

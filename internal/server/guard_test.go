package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/moabukar/miniblue/internal/arm"
)

func TestCheckEngineOverlap_PanicsOnOverlap(t *testing.T) {
	reg := arm.NewRegistry()
	reg.Register(arm.Type{
		Name:        "Microsoft.Authorization/policyDefinitions",
		APIVersions: []string{"2023-04-01"},
		Scopes:      arm.ScopeSubscription,
	})

	cases := []struct {
		name  string
		route string
	}{
		{
			name:  "instance route",
			route: "/subscriptions/{subscriptionId}/providers/Microsoft.Authorization/policyDefinitions/{policyDefinitionName}",
		},
		{
			name:  "collection route",
			route: "/subscriptions/{subscriptionId}/providers/Microsoft.Authorization/policyDefinitions",
		},
		{
			name:  "mixed case route",
			route: "/subscriptions/{subscriptionId}/providers/microsoft.authorization/policydefinitions/{name}",
		},
		{
			name:  "different scope",
			route: "/subscriptions/{subscriptionId}/resourceGroups/{rg}/providers/Microsoft.Authorization/policyDefinitions/{name}",
		},
		{
			name:  "wildcard under registered type",
			route: "/subscriptions/{subscriptionId}/providers/Microsoft.Authorization/policyDefinitions/*",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := chi.NewRouter()
			r.Get(tc.route, func(http.ResponseWriter, *http.Request) {})

			defer func() {
				rvr := recover()
				if rvr == nil {
					t.Fatalf("expected panic for overlapping route %q", tc.route)
				}
				msg, _ := rvr.(string)
				if !strings.Contains(msg, "Microsoft.Authorization/policyDefinitions") {
					t.Fatalf("panic %q does not mention overlapping type", msg)
				}
			}()
			checkEngineOverlap(r, reg)
		})
	}
}

func TestCheckEngineOverlap_AllowsNonOverlappingRoutes(t *testing.T) {
	reg := arm.NewRegistry()
	reg.Register(arm.Type{
		Name:        "Microsoft.Authorization/policyDefinitions",
		APIVersions: []string{"2023-04-01"},
		Scopes:      arm.ScopeSubscription,
	})

	r := chi.NewRouter()
	r.Get("/health", func(http.ResponseWriter, *http.Request) {})
	r.Get("/blob/{account}/{container}/*", func(http.ResponseWriter, *http.Request) {})
	r.Get("/subscriptions/{sub}/providers/Microsoft.Authorization/roleDefinitions/{id}", func(http.ResponseWriter, *http.Request) {})
	r.Get("/subscriptions/{sub}/providers/Microsoft.KeyVault/checkNameAvailability", func(http.ResponseWriter, *http.Request) {})
	r.Get("/subscriptions/{sub}/resourceGroups/{rg}/providers/{rp}/{type}/{name}/providers/Microsoft.Authorization/*", func(http.ResponseWriter, *http.Request) {})

	checkEngineOverlap(r, reg)
}

func TestIsLegacyARMRoute(t *testing.T) {
	cases := []struct {
		route string
		want  bool
	}{
		{"/subscriptions/{sub}/resourceGroups/{rg}/providers/Microsoft.Network/virtualNetworks/{name}", true},
		{"/subscriptions/{sub}/providers/Microsoft.Network/virtualNetworks", true},
		{"/subscriptions/{sub}/resourcegroups/{rg}/", true},
		{"/providers/Microsoft.Management/managementGroups/{id}", true},
		// Exempt provider-level operations.
		{"/subscriptions/{sub}/providers/Microsoft.Compute/locations/{location}/vmSizes", false},
		{"/subscriptions/{sub}/providers/Microsoft.KeyVault/locations/{location}/deletedVaults/{vaultName}/purge", false},
		{"/subscriptions/{sub}/providers/Microsoft.KeyVault/checkNameAvailability", false},
		{"/subscriptions/{sub}/providers/Microsoft.Web/checknameavailability", false},
		{"/providers/Microsoft.Network/operations", false},
		// Non-ARM routes.
		{"/health", false},
		{"/blob/{account}/{container}/*", false},
		{"/kv/*", false},
		{"/{tenantId}/oauth2/token", false},
	}
	for _, tc := range cases {
		if got := isLegacyARMRoute(tc.route); got != tc.want {
			t.Errorf("isLegacyARMRoute(%q) = %v, want %v", tc.route, got, tc.want)
		}
	}
}

func TestDiffLegacyARMRoutes(t *testing.T) {
	frozen := []string{"/subscriptions/{sub}/a", "/subscriptions/{sub}/b"}
	current := []string{"/subscriptions/{sub}/b", "/subscriptions/{sub}/c"}

	unexpected, stale := diffLegacyARMRoutes(current, frozen)
	if !slices.Equal(unexpected, []string{"/subscriptions/{sub}/c"}) {
		t.Fatalf("unexpected = %v, want [/subscriptions/{sub}/c]", unexpected)
	}
	if !slices.Equal(stale, []string{"/subscriptions/{sub}/a"}) {
		t.Fatalf("stale = %v, want [/subscriptions/{sub}/a]", stale)
	}
}

func TestLegacyARMRouteFreeze(t *testing.T) {
	t.Setenv("SERVICES", "")
	s := New()

	current := collectLegacyARMRoutes(s.router)
	frozen := readLegacyARMRoutesFile(t, filepath.Join("testdata", "legacy_arm_routes.txt"))

	if !sort.StringsAreSorted(frozen) {
		t.Errorf("testdata/legacy_arm_routes.txt must be sorted")
	}

	unexpected, stale := diffLegacyARMRoutes(current, frozen)
	for _, route := range unexpected {
		t.Errorf("chi ARM route %q is not in testdata/legacy_arm_routes.txt; register an arm.Type instead", route)
	}
	for _, route := range stale {
		t.Logf("legacy route %q is no longer routed; delete it from testdata/legacy_arm_routes.txt", route)
	}
}

func collectLegacyARMRoutes(r chi.Router) []string {
	seen := make(map[string]struct{})
	var routes []string
	_ = chi.Walk(r, func(_, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if _, dup := seen[route]; dup {
			return nil
		}
		seen[route] = struct{}{}
		if isLegacyARMRoute(route) {
			routes = append(routes, route)
		}
		return nil
	})
	sort.Strings(routes)
	return routes
}

func isLegacyARMRoute(route string) bool {
	parts := splitPath(route)
	if len(parts) == 0 {
		return false
	}
	if !strings.EqualFold(parts[0], "subscriptions") && !strings.EqualFold(parts[0], "providers") {
		return false
	}
	return !isExemptProviderOperation(parts)
}

func isExemptProviderOperation(parts []string) bool {
	for i := len(parts) - 3; i >= 0; i-- {
		if strings.EqualFold(parts[i], "providers") {
			op := parts[i+2]
			return strings.EqualFold(op, "locations") ||
				strings.EqualFold(op, "operations") ||
				strings.EqualFold(op, "checkNameAvailability")
		}
	}
	return false
}

func readLegacyARMRoutesFile(t *testing.T, path string) []string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var routes []string
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		routes = append(routes, line)
	}
	return routes
}

func diffLegacyARMRoutes(current, frozen []string) (unexpected, stale []string) {
	frozenSet := make(map[string]struct{}, len(frozen))
	for _, r := range frozen {
		frozenSet[r] = struct{}{}
	}
	currentSet := make(map[string]struct{}, len(current))
	for _, r := range current {
		currentSet[r] = struct{}{}
		if _, ok := frozenSet[r]; !ok {
			unexpected = append(unexpected, r)
		}
	}
	for _, r := range frozen {
		if _, ok := currentSet[r]; !ok {
			stale = append(stale, r)
		}
	}
	return unexpected, stale
}

func TestServerScopeExistsAndRGDeleteCascade(t *testing.T) {
	s := New()
	s.armTypes.Register(arm.Type{
		Name:        "Microsoft.Test/widgets",
		APIVersions: []string{"2024-01-01"},
		Scopes:      arm.ScopeResourceGroup,
	})
	h := s.Handler()
	do := func(method, path, body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(method, path, strings.NewReader(body)))
		return w
	}

	const (
		rgPath     = "/subscriptions/sub1/resourcegroups/MyRG?api-version=2023-07-01"
		widgetPath = "/subscriptions/sub1/resourceGroups/myrg/providers/Microsoft.Test/widgets/w1?api-version=2024-01-01"
		subList    = "/subscriptions/sub1/providers/Microsoft.Test/widgets?api-version=2024-01-01"
	)

	// PUT to a missing RG returns 404 ResourceGroupNotFound.
	w := do(http.MethodPut, widgetPath, `{}`)
	if w.Code != http.StatusNotFound {
		t.Fatalf("PUT missing RG: status = %d, want 404 (%s)", w.Code, w.Body.String())
	}
	var errResp struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &errResp); err != nil || errResp.Error.Code != "ResourceGroupNotFound" {
		t.Fatalf("PUT missing RG error = %s, want ResourceGroupNotFound", w.Body.String())
	}

	// Creating the RG enables the RG-scoped PUT (case-insensitively).
	if w := do(http.MethodPut, rgPath, `{"location":"eastus"}`); w.Code != http.StatusCreated {
		t.Fatalf("PUT RG: status = %d (%s)", w.Code, w.Body.String())
	}
	if w := do(http.MethodPut, widgetPath, `{}`); w.Code != http.StatusCreated {
		t.Fatalf("PUT widget: status = %d (%s)", w.Code, w.Body.String())
	}

	// Subscription-wide list finds the RG-scoped widget.
	w = do(http.MethodGet, subList, "")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"w1"`) {
		t.Fatalf("sub-wide list: status = %d body = %s", w.Code, w.Body.String())
	}

	// Deleting the RG cascades to engine resources via DeleteScope.
	if w := do(http.MethodDelete, rgPath, ""); w.Code != http.StatusAccepted {
		t.Fatalf("DELETE RG: status = %d (%s)", w.Code, w.Body.String())
	}
	if w := do(http.MethodGet, widgetPath, ""); w.Code != http.StatusNotFound {
		t.Fatalf("GET widget after RG delete: status = %d, want 404", w.Code)
	}
}


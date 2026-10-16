package arm

import (
	"fmt"
	"strings"
)

// Type declares a resource type served by the engine.
type Type struct {
	// Name is "Namespace/type" for a top-level type; for a child in Children
	// it is just the child segment (e.g. "subnets").
	Name string
	// APIVersions lists the accepted api-version query values. A request
	// without api-version is accepted, like the rest of miniblue.
	APIVersions []string
	// Scopes is the bitmask of scopes the type may be created in. Requests at
	// other scopes are not claimed and fall through to the next handler.
	Scopes ScopeKind
	// Children are nested types (e.g. subnets under virtualNetworks). They
	// inherit APIVersions and Scopes when those are empty.
	Children []Type
	// PatchTags enables PATCH of the tags property.
	PatchTags bool
	Hooks     Hooks
}

// Hooks are optional per-type behaviour. The engine is pure passthrough
// without them: the body is stored as sent plus id, name and type. Numbers in
// the body are json.Number, not float64, so large integers are not altered.
type Hooks struct {
	// Validate rejects a PUT body; the error text becomes a 400 response.
	Validate func(id ID, body map[string]interface{}) error
	// Defaults fills server-side defaults into a PUT body, after Validate.
	// Types whose clients drift on missing defaults (e.g. azurerm) use it.
	Defaults func(id ID, body map[string]interface{})
}

// Registry holds the registered types, keyed by lowercased full type name.
type Registry struct {
	types map[string]*Type
}

// NewRegistry returns an empty Registry.
func NewRegistry() *Registry { return &Registry{types: map[string]*Type{}} }

// Register adds t and its children. It panics on a malformed or duplicate
// registration, which is a programming error caught at startup.
func (r *Registry) Register(t Type) {
	if strings.Count(t.Name, "/") != 1 {
		panic(fmt.Sprintf("arm: type %q must be Namespace/type", t.Name))
	}
	r.add(t)
}

func (r *Registry) add(t Type) {
	if len(t.APIVersions) == 0 || t.Scopes == 0 {
		panic(fmt.Sprintf("arm: type %q needs APIVersions and Scopes", t.Name))
	}
	key := strings.ToLower(t.Name)
	if _, dup := r.types[key]; dup {
		panic(fmt.Sprintf("arm: type %q registered twice", t.Name))
	}
	r.types[key] = &t
	for _, c := range t.Children {
		c.Name = t.Name + "/" + c.Name
		if len(c.APIVersions) == 0 {
			c.APIVersions = t.APIVersions
		}
		if c.Scopes == 0 {
			c.Scopes = t.Scopes
		}
		r.add(c)
	}
}

// lookup finds the type for id; the returned Type carries the full type name.
func (r *Registry) lookup(id ID) (*Type, bool) {
	t, ok := r.types[strings.ToLower(id.TypeName())]
	return t, ok
}

func (t *Type) supports(apiVersion string) bool {
	for _, v := range t.APIVersions {
		if strings.EqualFold(v, apiVersion) {
			return true
		}
	}
	return false
}

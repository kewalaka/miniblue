// Package arm is a generic Azure Resource Manager resource engine: a
// case-insensitive resource ID parser, a canonical-ID store and a registry
// driven CRUD handler. Resource types are registered declaratively; see
// website/docs/architecture/arm-engine.md.
package arm

import (
	"errors"
	"fmt"
	"strings"
)

// ErrInvalidID is returned for paths that are not a (supported) ARM resource ID.
var ErrInvalidID = errors.New("invalid ARM resource ID")

// ScopeKind identifies where a resource lives. Registered types use it as a
// bitmask of allowed scopes.
type ScopeKind uint8

const (
	ScopeTenant ScopeKind = 1 << iota
	ScopeManagementGroup
	ScopeSubscription
	ScopeResourceGroup
	// ScopeResource is an extension resource: its scope is another resource.
	ScopeResource
)

// Has reports whether k includes every bit of o.
func (k ScopeKind) Has(o ScopeKind) bool { return k&o == o }

const (
	nsResources          = "Microsoft.Resources"
	nsManagement         = "Microsoft.Management"
	typeSubscriptions    = "subscriptions"
	typeResourceGroups   = "resourceGroups"
	typeManagementGroups = "managementGroups"
)

// ID is a parsed ARM resource ID. Names keep the casing they were parsed with;
// compare IDs with Key. The zero value is the tenant root ("/").
//
// Subscriptions and resource groups are modelled as ordinary resources of
// Microsoft.Resources so that every scope is just another ID. A nil Scope
// means tenant scope. Collection paths (".../policyDefinitions") parse to an ID
// with one more Type than Names.
type ID struct {
	Scope     *ID
	Namespace string
	Types     []string
	Names     []string
}

// ParseID parses an ARM path (no query string), case-insensitively. Paths that
// are not resource IDs return ErrInvalidID.
func ParseID(path string) (ID, error) {
	trimmed := strings.Trim(path, "/")
	if trimmed == "" {
		return ID{}, nil
	}
	segs := strings.Split(trimmed, "/")
	for _, s := range segs {
		if s == "" {
			return ID{}, fmt.Errorf("%w: empty segment in %q", ErrInvalidID, path)
		}
	}
	return parseSegments(segs)
}

func parseSegments(segs []string) (ID, error) {
	p := lastProviders(segs)
	if p < 0 {
		return parseBuiltin(segs)
	}
	var scope *ID
	if p > 0 {
		s, err := parseSegments(segs[:p])
		if err != nil {
			return ID{}, err
		}
		scope = &s
	}
	ns, rest := segs[p+1], segs[p+2:]
	if len(rest) == 0 {
		return ID{}, fmt.Errorf("%w: no resource type after provider %q", ErrInvalidID, ns)
	}
	id := ID{Scope: scope, Namespace: ns}
	for i, s := range rest {
		if i%2 == 0 {
			id.Types = append(id.Types, s)
		} else {
			id.Names = append(id.Names, s)
		}
	}
	if strings.EqualFold(ns, nsManagement) && strings.EqualFold(id.Types[0], typeManagementGroups) {
		id.Namespace, id.Types[0] = nsManagement, typeManagementGroups
	}
	return id, nil
}

// lastProviders returns the index of the last "providers" delimiter that is
// followed by a namespace, or -1. Resource names always sit at odd positions
// (the built-in prefixes and every "providers/{ns}/{type}/{name}..." group
// start on an even one), so only even positions can be a delimiter and a
// resource named "providers" is not mistaken for one.
func lastProviders(segs []string) int {
	for i := len(segs) - 2; i >= 0; i-- {
		if i%2 == 0 && strings.EqualFold(segs[i], "providers") {
			return i
		}
	}
	return -1
}

// parseBuiltin handles the IDs ARM writes without a "providers" segment:
// subscriptions[/{s}[/resourceGroups[/{rg}]]].
func parseBuiltin(segs []string) (ID, error) {
	bad := fmt.Errorf("%w: unsupported path %q", ErrInvalidID, strings.Join(segs, "/"))
	if !strings.EqualFold(segs[0], typeSubscriptions) || len(segs) > 4 {
		return ID{}, bad
	}
	sub := ID{Namespace: nsResources, Types: []string{typeSubscriptions}}
	if len(segs) == 1 {
		return sub, nil
	}
	sub.Names = []string{segs[1]}
	if len(segs) == 2 {
		return sub, nil
	}
	if !strings.EqualFold(segs[2], typeResourceGroups) {
		return ID{}, bad
	}
	rg := ID{Scope: &sub, Namespace: nsResources, Types: []string{typeResourceGroups}}
	if len(segs) == 4 {
		rg.Names = []string{segs[3]}
	}
	return rg, nil
}

// IsTenant reports whether id is the tenant root.
func (id ID) IsTenant() bool { return id.Namespace == "" }

// IsCollection reports whether id addresses a collection (list) rather than a resource.
func (id ID) IsCollection() bool { return !id.IsTenant() && len(id.Names) < len(id.Types) }

// Name is the final resource name, or "" for a collection or the tenant.
func (id ID) Name() string {
	if id.IsTenant() || id.IsCollection() {
		return ""
	}
	return id.Names[len(id.Names)-1]
}

// TypeName is the full resource type, e.g. "Microsoft.Network/virtualNetworks/subnets".
func (id ID) TypeName() string {
	if id.IsTenant() {
		return ""
	}
	return id.Namespace + "/" + strings.Join(id.Types, "/")
}

// WithTypeName returns id with the namespace and type segments replaced by the
// given type name of the same shape; used to apply a registered type's casing.
func (id ID) WithTypeName(typeName string) ID {
	parts := strings.Split(typeName, "/")
	if id.IsTenant() || len(parts) != len(id.Types)+1 {
		return id
	}
	id.Namespace, id.Types = parts[0], parts[1:]
	return id
}

// Parent is the resource that directly contains id: the parent resource for a
// child type, otherwise the scope.
func (id ID) Parent() ID {
	if n := len(id.Types); n > 1 {
		return ID{Scope: id.Scope, Namespace: id.Namespace, Types: id.Types[:n-1], Names: id.Names[:n-1]}
	}
	if id.Scope == nil {
		return ID{}
	}
	return *id.Scope
}

// ScopeKind classifies the scope id lives in.
func (id ID) ScopeKind() ScopeKind {
	s := id.Scope
	switch {
	case s == nil:
		return ScopeTenant
	case s.is(nsResources, typeSubscriptions):
		return ScopeSubscription
	case s.is(nsResources, typeResourceGroups):
		return ScopeResourceGroup
	case s.is(nsManagement, typeManagementGroups):
		return ScopeManagementGroup
	default:
		return ScopeResource
	}
}

func (id ID) is(ns, typ string) bool {
	return strings.EqualFold(id.Namespace, ns) && len(id.Types) == 1 && strings.EqualFold(id.Types[0], typ)
}

func (id ID) isBuiltin() bool {
	return id.Namespace == nsResources && len(id.Types) == 1 &&
		(id.Types[0] == typeSubscriptions || id.Types[0] == typeResourceGroups)
}

// String renders the ID with the casing it holds.
func (id ID) String() string {
	if id.IsTenant() {
		return "/"
	}
	var b strings.Builder
	if id.Scope != nil {
		b.WriteString(id.Scope.String())
	}
	if id.isBuiltin() {
		b.WriteString("/" + id.Types[0])
		if len(id.Names) > 0 {
			b.WriteString("/" + id.Names[0])
		}
		return b.String()
	}
	b.WriteString("/providers/" + id.Namespace)
	for i, t := range id.Types {
		b.WriteString("/" + t)
		if i < len(id.Names) {
			b.WriteString("/" + id.Names[i])
		}
	}
	return b.String()
}

// Key is the case-insensitive identity of an ID.
func (id ID) Key() string { return strings.ToLower(id.String()) }

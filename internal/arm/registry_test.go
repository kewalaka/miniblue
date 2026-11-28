package arm

import (
	"reflect"
	"testing"
)

func TestRegistryTypes(t *testing.T) {
	reg := NewRegistry()
	if got := reg.Types(); len(got) != 0 {
		t.Fatalf("empty registry Types() = %v, want empty", got)
	}

	reg.Register(Type{
		Name:        "Microsoft.Zeta/widgets",
		APIVersions: []string{"2024-01-01"},
		Scopes:      ScopeResourceGroup,
		Children: []Type{
			{Name: "parts", Children: []Type{{Name: "bolts"}}},
			{Name: "alphas"},
		},
	})
	reg.Register(Type{
		Name:        "Microsoft.Alpha/things",
		APIVersions: []string{"2024-01-01"},
		Scopes:      ScopeSubscription,
	})

	types := reg.Types()
	got := make([]string, len(types))
	for i, typ := range types {
		got[i] = typ.Name
	}
	want := []string{
		"Microsoft.Alpha/things",
		"Microsoft.Zeta/widgets",
		"Microsoft.Zeta/widgets/alphas",
		"Microsoft.Zeta/widgets/parts",
		"Microsoft.Zeta/widgets/parts/bolts",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Types() = %v, want %v", got, want)
	}
}

package arm

import (
	"errors"
	"testing"
)

func TestParseID(t *testing.T) {
	const sub = "/subscriptions/A1B2"
	tests := []struct {
		name       string
		path       string
		str        string // String() with the casing it was parsed with
		typeName   string
		resName    string
		collection bool
		scope      ScopeKind
		parent     string
	}{
		{
			name: "tenant root", path: "/", str: "/",
		},
		{
			name: "subscription", path: "/SUBSCRIPTIONS/A1B2", str: sub,
			typeName: "Microsoft.Resources/subscriptions", resName: "A1B2", scope: ScopeTenant, parent: "/",
		},
		{
			name: "subscriptions collection", path: "/subscriptions", str: "/subscriptions",
			typeName: "Microsoft.Resources/subscriptions", collection: true, scope: ScopeTenant, parent: "/",
		},
		{
			name: "resource group", path: "/subscriptions/A1B2/RESOURCEGROUPS/Rg1", str: sub + "/resourceGroups/Rg1",
			typeName: "Microsoft.Resources/resourceGroups", resName: "Rg1", scope: ScopeSubscription, parent: sub,
		},
		{
			name: "resource groups collection", path: "/subscriptions/A1B2/resourcegroups", str: sub + "/resourceGroups",
			typeName: "Microsoft.Resources/resourceGroups", collection: true, scope: ScopeSubscription, parent: sub,
		},
		{
			name: "management group", path: "/PROVIDERS/microsoft.management/MANAGEMENTGROUPS/Mg1",
			str:      "/providers/Microsoft.Management/managementGroups/Mg1",
			typeName: "Microsoft.Management/managementGroups", resName: "Mg1", scope: ScopeTenant, parent: "/",
		},
		{
			name: "tenant scope resource", path: "/providers/Microsoft.Authorization/policyDefinitions/Builtin",
			str:      "/providers/Microsoft.Authorization/policyDefinitions/Builtin",
			typeName: "Microsoft.Authorization/policyDefinitions", resName: "Builtin", scope: ScopeTenant, parent: "/",
		},
		{
			name:     "management group scope resource",
			path:     "/providers/Microsoft.Management/managementGroups/Mg1/providers/Microsoft.Authorization/policyDefinitions/Pd",
			str:      "/providers/Microsoft.Management/managementGroups/Mg1/providers/Microsoft.Authorization/policyDefinitions/Pd",
			typeName: "Microsoft.Authorization/policyDefinitions", resName: "Pd", scope: ScopeManagementGroup,
			parent: "/providers/Microsoft.Management/managementGroups/Mg1",
		},
		{
			name: "subscription scope resource", path: "/subscriptions/A1B2/providers/Microsoft.Authorization/policyDefinitions/Pd",
			str:      sub + "/providers/Microsoft.Authorization/policyDefinitions/Pd",
			typeName: "Microsoft.Authorization/policyDefinitions", resName: "Pd", scope: ScopeSubscription, parent: sub,
		},
		{
			name: "subscription scope collection", path: "/subscriptions/A1B2/providers/Microsoft.Authorization/policyDefinitions",
			str:      sub + "/providers/Microsoft.Authorization/policyDefinitions",
			typeName: "Microsoft.Authorization/policyDefinitions", collection: true, scope: ScopeSubscription, parent: sub,
		},
		{
			name:     "resource group scope resource",
			path:     "/subscriptions/A1B2/resourceGroups/Rg1/providers/Microsoft.Network/virtualNetworks/Vnet",
			str:      sub + "/resourceGroups/Rg1/providers/Microsoft.Network/virtualNetworks/Vnet",
			typeName: "Microsoft.Network/virtualNetworks", resName: "Vnet", scope: ScopeResourceGroup,
			parent: sub + "/resourceGroups/Rg1",
		},
		{
			name:     "nested child",
			path:     "/subscriptions/A1B2/resourceGroups/Rg1/providers/Microsoft.Network/virtualNetworks/Vnet/subnets/Sn",
			str:      sub + "/resourceGroups/Rg1/providers/Microsoft.Network/virtualNetworks/Vnet/subnets/Sn",
			typeName: "Microsoft.Network/virtualNetworks/subnets", resName: "Sn", scope: ScopeResourceGroup,
			parent: sub + "/resourceGroups/Rg1/providers/Microsoft.Network/virtualNetworks/Vnet",
		},
		{
			name:     "nested child collection",
			path:     "/subscriptions/A1B2/resourceGroups/Rg1/providers/Microsoft.Network/virtualNetworks/Vnet/subnets",
			str:      sub + "/resourceGroups/Rg1/providers/Microsoft.Network/virtualNetworks/Vnet/subnets",
			typeName: "Microsoft.Network/virtualNetworks/subnets", collection: true, scope: ScopeResourceGroup,
			parent: sub + "/resourceGroups/Rg1/providers/Microsoft.Network/virtualNetworks/Vnet",
		},
		{
			name:     "extension resource on a resource",
			path:     "/subscriptions/A1B2/resourceGroups/Rg1/providers/Microsoft.Compute/virtualMachines/Vm/providers/Microsoft.Authorization/roleAssignments/Ra",
			str:      sub + "/resourceGroups/Rg1/providers/Microsoft.Compute/virtualMachines/Vm/providers/Microsoft.Authorization/roleAssignments/Ra",
			typeName: "Microsoft.Authorization/roleAssignments", resName: "Ra", scope: ScopeResource,
			parent: sub + "/resourceGroups/Rg1/providers/Microsoft.Compute/virtualMachines/Vm",
		},
		{
			name:     "extension resource on a nested child",
			path:     "/subscriptions/A1B2/resourceGroups/Rg1/providers/Microsoft.Network/virtualNetworks/Vnet/subnets/Sn/providers/Microsoft.Authorization/locks/L",
			str:      sub + "/resourceGroups/Rg1/providers/Microsoft.Network/virtualNetworks/Vnet/subnets/Sn/providers/Microsoft.Authorization/locks/L",
			typeName: "Microsoft.Authorization/locks", resName: "L", scope: ScopeResource,
			parent: sub + "/resourceGroups/Rg1/providers/Microsoft.Network/virtualNetworks/Vnet/subnets/Sn",
		},
		{
			name:     "extension resource on an extension resource",
			path:     "/providers/Microsoft.Management/managementGroups/Mg1/providers/Microsoft.Authorization/roleAssignments/Ra/providers/Microsoft.Authorization/locks/L",
			str:      "/providers/Microsoft.Management/managementGroups/Mg1/providers/Microsoft.Authorization/roleAssignments/Ra/providers/Microsoft.Authorization/locks/L",
			typeName: "Microsoft.Authorization/locks", resName: "L", scope: ScopeResource,
			parent: "/providers/Microsoft.Management/managementGroups/Mg1/providers/Microsoft.Authorization/roleAssignments/Ra",
		},
		{
			name:     "child of a parent named providers",
			path:     "/subscriptions/A1B2/providers/Microsoft.Test/widgets/providers/parts/P",
			str:      sub + "/providers/Microsoft.Test/widgets/providers/parts/P",
			typeName: "Microsoft.Test/widgets/parts", resName: "P", scope: ScopeSubscription,
			parent: sub + "/providers/Microsoft.Test/widgets/providers",
		},
		{
			name:     "extension resource on a resource named providers",
			path:     "/subscriptions/A1B2/providers/Microsoft.Test/widgets/providers/providers/Microsoft.Authorization/locks/L",
			str:      sub + "/providers/Microsoft.Test/widgets/providers/providers/Microsoft.Authorization/locks/L",
			typeName: "Microsoft.Authorization/locks", resName: "L", scope: ScopeResource,
			parent: sub + "/providers/Microsoft.Test/widgets/providers",
		},
		{
			name:     "resource group named providers",
			path:     "/subscriptions/A1B2/resourceGroups/providers/providers/Microsoft.Network/virtualNetworks/V",
			str:      sub + "/resourceGroups/providers/providers/Microsoft.Network/virtualNetworks/V",
			typeName: "Microsoft.Network/virtualNetworks", resName: "V", scope: ScopeResourceGroup,
			parent: sub + "/resourceGroups/providers",
		},
		{
			name:     "trailing slash and mangled keywords",
			path:     "/Subscriptions/A1B2/PROVIDERS/microsoft.authorization/POLICYDEFINITIONS/Pd/",
			str:      sub + "/providers/microsoft.authorization/POLICYDEFINITIONS/Pd",
			typeName: "microsoft.authorization/POLICYDEFINITIONS", resName: "Pd", scope: ScopeSubscription, parent: sub,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			id, err := ParseID(tc.path)
			if err != nil {
				t.Fatal(err)
			}
			if got := id.String(); got != tc.str {
				t.Errorf("String() = %q, want %q", got, tc.str)
			}
			if tc.str == "/" {
				if !id.IsTenant() {
					t.Error("want tenant")
				}
				return
			}
			if got := id.TypeName(); got != tc.typeName {
				t.Errorf("TypeName() = %q, want %q", got, tc.typeName)
			}
			if got := id.Name(); got != tc.resName {
				t.Errorf("Name() = %q, want %q", got, tc.resName)
			}
			if got := id.IsCollection(); got != tc.collection {
				t.Errorf("IsCollection() = %v, want %v", got, tc.collection)
			}
			if got := id.ScopeKind(); got != tc.scope {
				t.Errorf("ScopeKind() = %v, want %v", got, tc.scope)
			}
			if got := id.Parent().String(); got != tc.parent {
				t.Errorf("Parent() = %q, want %q", got, tc.parent)
			}
		})
	}
}

func TestParseIDKeyIsCaseInsensitive(t *testing.T) {
	a, err := ParseID("/subscriptions/abc/providers/Microsoft.Authorization/policyDefinitions/Pd")
	if err != nil {
		t.Fatal(err)
	}
	b, err := ParseID("/SUBSCRIPTIONS/ABC/PROVIDERS/MICROSOFT.AUTHORIZATION/POLICYDEFINITIONS/PD")
	if err != nil {
		t.Fatal(err)
	}
	if a.Key() != b.Key() {
		t.Errorf("keys differ: %q vs %q", a.Key(), b.Key())
	}
	if a.String() == b.String() {
		t.Error("String() should keep the parsed casing")
	}
}

func TestParseIDInvalid(t *testing.T) {
	for _, path := range []string{
		"/subscriptions/a//resourceGroups/rg",
		"/subscriptions/a/providers",
		"/subscriptions/a/providers/Microsoft.Authorization",
		"/subscriptions/a/resourceGroups/rg/deployments/d",
		"/subscriptions/a/locations",
		"/health",
		"/subscriptions/a/foo/providers/Microsoft.Authorization/policyDefinitions/pd", // "providers" in a name position
		"/subscriptions/a/resourceGroups/rg/providers",
	} {
		if id, err := ParseID(path); err == nil {
			t.Errorf("ParseID(%q) = %v, want error", path, id)
		} else if !errors.Is(err, ErrInvalidID) {
			t.Errorf("ParseID(%q) error = %v, want ErrInvalidID", path, err)
		}
	}
}

func TestIDWithTypeName(t *testing.T) {
	id, err := ParseID("/subscriptions/a/providers/MICROSOFT.AUTHORIZATION/POLICYDEFINITIONS/Pd")
	if err != nil {
		t.Fatal(err)
	}
	got := id.WithTypeName("Microsoft.Authorization/policyDefinitions").String()
	if want := "/subscriptions/a/providers/Microsoft.Authorization/policyDefinitions/Pd"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if got := id.WithTypeName("Microsoft.Authorization/a/b").String(); got != id.String() {
		t.Errorf("mismatched shape must be a no-op, got %q", got)
	}
}

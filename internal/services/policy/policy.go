// Package policy registers the Azure Policy resource types served by the
// generic ARM engine (internal/arm).
package policy

import "github.com/moabukar/miniblue/internal/arm"

// Register adds the policy types to reg. Policy definitions are pure
// passthrough: no hooks, the engine stores the body as sent.
func Register(reg *arm.Registry) {
	reg.Register(arm.Type{
		Name:        "Microsoft.Authorization/policyDefinitions",
		APIVersions: []string{"2023-04-01"},
		Scopes:      arm.ScopeSubscription,
	})
}

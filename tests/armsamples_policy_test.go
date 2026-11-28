package tests

import "github.com/moabukar/miniblue/internal/arm/armtest"

var policySamples = []armtest.Sample{
	{
		Type: "Microsoft.Authorization/policyDefinitions",
		Body: map[string]any{
			"properties": map[string]any{
				"policyType":  "Custom",
				"mode":        "All",
				"displayName": "Deny non-EU locations",
				"description": "Removed on update to verify PUT replace semantics",
				"policyRule": map[string]any{
					"if":   map[string]any{"not": map[string]any{"field": "location", "in": []any{"westeurope"}}},
					"then": map[string]any{"effect": "deny"},
				},
			},
		},
		Update: map[string]any{
			"properties": map[string]any{
				"policyType":  "Custom",
				"mode":        "Indexed",
				"displayName": "Audit non-EU locations",
				"policyRule": map[string]any{
					"if":   map[string]any{"not": map[string]any{"field": "location", "in": []any{"westeurope"}}},
					"then": map[string]any{"effect": "audit"},
				},
			},
		},
	},
}

package arm

import (
	"errors"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/moabukar/miniblue/internal/azerr"
	"github.com/moabukar/miniblue/internal/token"
)

func TestCheckName(t *testing.T) {
	re := regexp.MustCompile(`^[a-z][a-z0-9-]*[a-z0-9]$`)

	for _, tc := range []struct {
		name    string
		input   string
		re      *regexp.Regexp
		min     int
		max     int
		wantErr bool
	}{
		{"valid", "my-service-1", re, 3, 20, false},
		{"exact min", "abc", re, 3, 20, false},
		{"exact max", "a1234", re, 3, 5, false},
		{"too short", "ab", re, 3, 20, true},
		{"too long", "a12345", re, 3, 5, true},
		{"pattern mismatch", "Bad_Name", re, 3, 20, true},
		{"nil regex length ok", "Any_Thing", nil, 1, 20, false},
		{"nil regex too short", "", nil, 1, 20, true},
		{"no length bounds regex ok", "a1", re, 0, 0, false},
		{"no length bounds regex fail", "A1", re, 0, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := CheckName(tc.input, tc.re, tc.min, tc.max)
			if !tc.wantErr {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			var ae *azerr.Error
			if !errors.As(err, &ae) {
				t.Fatalf("error = %T (%v), want *azerr.Error", err, err)
			}
			if ae.Status != http.StatusBadRequest || ae.Code != "InvalidResourceName" {
				t.Fatalf("error = %d %s, want 400 InvalidResourceName", ae.Status, ae.Code)
			}
		})
	}
}

func TestDefaultIdentityNoOp(t *testing.T) {
	doc := map[string]any{"properties": map[string]any{}}
	DefaultIdentity(doc, nil, nil)
	if _, ok := doc["identity"]; ok {
		t.Fatalf("should not add identity block: %v", doc)
	}

	noneDoc := map[string]any{"identity": map[string]any{"type": "None"}}
	cur := map[string]any{
		"identity": map[string]any{
			"type":        "SystemAssigned",
			"principalId": "11111111-1111-1111-1111-111111111111",
			"tenantId":    token.DefaultTenantID,
		},
	}
	DefaultIdentity(noneDoc, cur, nil)
	idObj := noneDoc["identity"].(map[string]any)
	if _, has := idObj["principalId"]; has {
		t.Fatalf("type=None should not populate principalId: %v", idObj)
	}
	if _, has := idObj["tenantId"]; has {
		t.Fatalf("type=None should not populate tenantId: %v", idObj)
	}
}

func TestDefaultIdentitySystemAssigned(t *testing.T) {
	doc := map[string]any{"identity": map[string]any{"type": "SystemAssigned"}}
	DefaultIdentity(doc, nil, nil)

	idObj := doc["identity"].(map[string]any)
	pid, _ := idObj["principalId"].(string)
	tid, _ := idObj["tenantId"].(string)
	if _, err := uuid.Parse(pid); err != nil {
		t.Fatalf("principalId %q is not a valid UUID: %v", pid, err)
	}
	if tid != token.DefaultTenantID {
		t.Fatalf("tenantId = %q, want %q", tid, token.DefaultTenantID)
	}

	// Re-running with Current preserves principalId and tenantId.
	doc2 := map[string]any{"identity": map[string]any{"type": "SystemAssigned"}}
	DefaultIdentity(doc2, doc, nil)
	idObj2 := doc2["identity"].(map[string]any)
	if idObj2["principalId"] != pid || idObj2["tenantId"] != tid {
		t.Fatalf("update changed SystemAssigned ids: got %v, want %s/%s", idObj2, pid, tid)
	}

	// If Current did not have SystemAssigned, a fresh UUID is generated.
	curUser := map[string]any{"identity": map[string]any{"type": "UserAssigned"}}
	doc3 := map[string]any{"identity": map[string]any{"type": "SystemAssigned"}}
	DefaultIdentity(doc3, curUser, nil)
	pid3, _ := doc3["identity"].(map[string]any)["principalId"].(string)
	if _, err := uuid.Parse(pid3); err != nil || pid3 == pid {
		t.Fatalf("expected new UUID, got %q", pid3)
	}
}

func TestDefaultIdentityUserAssigned(t *testing.T) {
	const (
		knownKey   = "/subscriptions/s1/resourceGroups/rg1/providers/Microsoft.ManagedIdentity/userAssignedIdentities/known"
		missingKey = "/subscriptions/s1/resourceGroups/rg1/providers/Microsoft.ManagedIdentity/userAssignedIdentities/missing"
	)
	lookup := func(id string) (string, string, bool) {
		if strings.EqualFold(id, knownKey) {
			return "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb", true
		}
		return "", "", false
	}

	doc := map[string]any{
		"identity": map[string]any{
			"type": "UserAssigned",
			"userAssignedIdentities": map[string]any{
				knownKey:   map[string]any{},
				missingKey: map[string]any{},
			},
		},
	}
	DefaultIdentity(doc, nil, lookup)

	uai := doc["identity"].(map[string]any)["userAssignedIdentities"].(map[string]any)
	known := uai[knownKey].(map[string]any)
	if known["principalId"] != "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa" ||
		known["clientId"] != "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb" {
		t.Fatalf("known UAMI = %v", known)
	}
	missing := uai[missingKey].(map[string]any)
	missPID, _ := missing["principalId"].(string)
	missCID, _ := missing["clientId"].(string)
	if _, err := uuid.Parse(missPID); err != nil {
		t.Fatalf("missing UAMI principalId %q invalid UUID", missPID)
	}
	if _, err := uuid.Parse(missCID); err != nil {
		t.Fatalf("missing UAMI clientId %q invalid UUID", missCID)
	}

	// Second PUT with nil lookup preserves existing IDs from Current (even with different key casing).
	doc2 := map[string]any{
		"identity": map[string]any{
			"type": "UserAssigned",
			"userAssignedIdentities": map[string]any{
				strings.ToUpper(missingKey): map[string]any{},
			},
		},
	}
	DefaultIdentity(doc2, doc, nil)
	uai2 := doc2["identity"].(map[string]any)["userAssignedIdentities"].(map[string]any)
	preserved := uai2[strings.ToUpper(missingKey)].(map[string]any)
	if preserved["principalId"] != missPID || preserved["clientId"] != missCID {
		t.Fatalf("missing UAMI not preserved from Current: got %v, want %s/%s", preserved, missPID, missCID)
	}
}

func TestDefaultIdentitySystemAndUserAssigned(t *testing.T) {
	const key = "/subscriptions/s1/resourceGroups/rg1/providers/Microsoft.ManagedIdentity/userAssignedIdentities/id1"
	doc := map[string]any{
		"identity": map[string]any{
			"type": "SystemAssigned, UserAssigned",
			"userAssignedIdentities": map[string]any{
				key: map[string]any{},
			},
		},
	}
	DefaultIdentity(doc, nil, func(string) (string, string, bool) {
		return "11111111-1111-1111-1111-111111111111", "22222222-2222-2222-2222-222222222222", true
	})

	idObj := doc["identity"].(map[string]any)
	if _, err := uuid.Parse(idObj["principalId"].(string)); err != nil {
		t.Fatalf("system principalId invalid: %v", idObj["principalId"])
	}
	if idObj["tenantId"] != token.DefaultTenantID {
		t.Fatalf("tenantId = %v, want %s", idObj["tenantId"], token.DefaultTenantID)
	}
	entry := idObj["userAssignedIdentities"].(map[string]any)[key].(map[string]any)
	if entry["principalId"] != "11111111-1111-1111-1111-111111111111" ||
		entry["clientId"] != "22222222-2222-2222-2222-222222222222" {
		t.Fatalf("userAssignedIdentities entry = %v", entry)
	}
}

package arm

import (
	"net/http"
	"regexp"
	"strings"

	"github.com/google/uuid"
	"github.com/moabukar/miniblue/internal/azerr"
	"github.com/moabukar/miniblue/internal/token"
)

// IdentityLookup returns a user-assigned managed identity's principalId and
// clientId for the given resource ID. A nil lookup is allowed.
type IdentityLookup func(id string) (principalID, clientID string, ok bool)

// CheckName validates a resource name against an optional length range (min/max
// when > 0) and an optional regular expression, returning 400 InvalidResourceName
// on failure.
func CheckName(name string, re *regexp.Regexp, min, max int) error {
	if min > 0 && max > 0 && (len(name) < min || len(name) > max) {
		return azerr.New(http.StatusBadRequest, "InvalidResourceName",
			"The resource name '%s' must be between %d and %d characters.", name, min, max)
	}
	if min > 0 && len(name) < min {
		return azerr.New(http.StatusBadRequest, "InvalidResourceName",
			"The resource name '%s' must be at least %d characters.", name, min)
	}
	if max > 0 && len(name) > max {
		return azerr.New(http.StatusBadRequest, "InvalidResourceName",
			"The resource name '%s' must be at most %d characters.", name, max)
	}
	if re != nil && !re.MatchString(name) {
		return azerr.New(http.StatusBadRequest, "InvalidResourceName",
			"The resource name '%s' is invalid.", name)
	}
	return nil
}

// DefaultIdentity populates server-owned identity fields on doc in place,
// preserving existing IDs from cur when present.
//
// It is a no-op when doc["identity"] is absent or its type is "None", and never
// adds an identity block the client did not send.
func DefaultIdentity(doc, cur map[string]any, uami IdentityLookup) {
	idObj, ok := doc["identity"].(map[string]any)
	if !ok || idObj == nil {
		return
	}
	idType, _ := idObj["type"].(string)
	if idType == "" || strings.EqualFold(strings.TrimSpace(idType), "None") {
		return
	}

	var curID map[string]any
	if cur != nil {
		curID, _ = cur["identity"].(map[string]any)
	}

	if hasIdentityType(idType, "SystemAssigned") {
		var principalID, tenantID string
		if curID != nil {
			if curType, _ := curID["type"].(string); hasIdentityType(curType, "SystemAssigned") {
				principalID, _ = curID["principalId"].(string)
				tenantID, _ = curID["tenantId"].(string)
			}
		}
		if principalID == "" {
			principalID = uuid.NewString()
		}
		if tenantID == "" {
			tenantID = token.DefaultTenantID
		}
		idObj["principalId"] = principalID
		idObj["tenantId"] = tenantID
	}

	if hasIdentityType(idType, "UserAssigned") {
		uai, _ := idObj["userAssignedIdentities"].(map[string]any)
		if uai == nil {
			return
		}
		var curUAI map[string]any
		if curID != nil {
			curUAI, _ = curID["userAssignedIdentities"].(map[string]any)
		}
		for key := range uai {
			var principalID, clientID string
			if uami != nil {
				if pid, cid, found := uami(key); found {
					principalID, clientID = pid, cid
				}
			}
			if principalID == "" || clientID == "" {
				curEntry := lookupMapCI(curUAI, key)
				if principalID == "" {
					principalID, _ = curEntry["principalId"].(string)
				}
				if clientID == "" {
					clientID, _ = curEntry["clientId"].(string)
				}
			}
			// (verify) whether real Azure rejects a missing UAMI rather than echoing generated IDs
			if principalID == "" {
				principalID = uuid.NewString()
			}
			if clientID == "" {
				clientID = uuid.NewString()
			}
			uai[key] = map[string]any{
				"principalId": principalID,
				"clientId":    clientID,
			}
		}
	}
}

func hasIdentityType(idType, want string) bool {
	for _, part := range strings.Split(idType, ",") {
		if strings.EqualFold(strings.TrimSpace(part), want) {
			return true
		}
	}
	return false
}

func lookupMapCI(m map[string]any, key string) map[string]any {
	if m == nil {
		return nil
	}
	if v, ok := m[key]; ok {
		entry, _ := v.(map[string]any)
		return entry
	}
	for k, v := range m {
		if strings.EqualFold(k, key) {
			entry, _ := v.(map[string]any)
			return entry
		}
	}
	return nil
}

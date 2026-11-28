package token

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"strings"

	"github.com/google/uuid"
)

const (
	// DefaultTenantID is the emulator's default tenant UUID.
	DefaultTenantID = "00000000-0000-0000-0000-000000000001"
	// DefaultClientID is used when an OAuth2 token request omits client_id.
	DefaultClientID = "miniblue"
	// DefaultClientOID is the stable object ID kept for the default client.
	DefaultClientOID = "00000000-0000-0000-0000-000000000002"
)

var (
	defaultTenantUUID = uuid.MustParse(DefaultTenantID)
	encodedHeader     = base64.RawURLEncoding.EncodeToString([]byte(`{"typ":"JWT","alg":"none"}`))
)

// Claims holds the JWT payload fields emitted and parsed by miniblue.
type Claims struct {
	OID        string `json:"oid,omitempty"`
	Sub        string `json:"sub,omitempty"`
	TID        string `json:"tid,omitempty"`
	AppID      string `json:"appid,omitempty"`
	Aud        string `json:"aud,omitempty"`
	Iss        string `json:"iss,omitempty"`
	IAT        int64  `json:"iat,omitempty"`
	NBF        int64  `json:"nbf,omitempty"`
	Exp        int64  `json:"exp,omitempty"`
	XmsMirid   string `json:"xms_mirid,omitempty"`
	MiniblueVM string `json:"miniblue_vm,omitempty"`
}

// ClientOID returns a stable object ID for a client in a tenant.
// The default client ("miniblue" or empty) keeps DefaultClientOID so existing
// users see no change; every other client gets a deterministic UUIDv5.
func ClientOID(tenantID, clientID string) string {
	if clientID == "" || clientID == DefaultClientID {
		return DefaultClientOID
	}
	ns, err := uuid.Parse(tenantID)
	if err != nil {
		ns = defaultTenantUUID
	}
	return uuid.NewSHA1(ns, []byte(clientID)).String()
}

// Issue encodes c as an unsigned (alg: none) JWT-shaped token.
func Issue(c Claims) string {
	payload, _ := json.Marshal(c)
	return encodedHeader + "." + base64.RawURLEncoding.EncodeToString(payload) + ".mock-signature"
}

// Parse extracts Claims from a raw JWT or a "Bearer <jwt>" header value
// without verifying signatures.
func Parse(bearer string) (Claims, bool) {
	raw := strings.TrimSpace(bearer)
	if len(raw) >= 7 && strings.EqualFold(raw[:7], "Bearer ") {
		raw = strings.TrimSpace(raw[7:])
	}
	if raw == "" {
		return Claims{}, false
	}
	parts := strings.Split(raw, ".")
	if len(parts) != 3 || parts[1] == "" {
		return Claims{}, false
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		payload, err = base64.URLEncoding.DecodeString(parts[1])
		if err != nil {
			return Claims{}, false
		}
	}
	if !bytes.HasPrefix(bytes.TrimSpace(payload), []byte("{")) {
		return Claims{}, false
	}
	var c Claims
	if err := json.Unmarshal(payload, &c); err != nil {
		return Claims{}, false
	}
	return c, true
}

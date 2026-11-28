package token

import (
	"encoding/base64"
	"testing"

	"github.com/google/uuid"
)

func TestIssueAndParseRoundTrip(t *testing.T) {
	want := Claims{
		OID:        "11111111-1111-1111-1111-111111111111",
		Sub:        "11111111-1111-1111-1111-111111111111",
		TID:        DefaultTenantID,
		AppID:      "reader-app",
		Aud:        "https://management.azure.com/",
		Iss:        "https://sts.windows.net/" + DefaultTenantID + "/",
		IAT:        1700000000,
		NBF:        1700000000,
		Exp:        1700086400,
		XmsMirid:   "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.ManagedIdentity/userAssignedIdentities/id1",
		MiniblueVM: "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Compute/virtualMachines/vm1",
	}

	raw := Issue(want)
	for _, input := range []string{raw, "Bearer " + raw, "bearer " + raw, "  Bearer   " + raw + "  "} {
		got, ok := Parse(input)
		if !ok {
			t.Fatalf("Parse(%q) returned ok=false", input)
		}
		if got != want {
			t.Fatalf("Parse(%q) = %+v, want %+v", input, got, want)
		}
	}
}

func TestParseRejectsInvalidTokens(t *testing.T) {
	nullPayload := base64.RawURLEncoding.EncodeToString([]byte("null"))
	arrPayload := base64.RawURLEncoding.EncodeToString([]byte("[]"))
	badJSON := base64.RawURLEncoding.EncodeToString([]byte("{"))

	cases := []string{
		"",
		"   ",
		"Bearer ",
		"Basic dXNlcjpwYXNz",
		"only-one-part",
		"two.parts",
		"a.b.c.d",
		"hdr..sig",
		"hdr.!!!not-base64!!!.sig",
		"hdr." + nullPayload + ".sig",
		"hdr." + arrPayload + ".sig",
		"hdr." + badJSON + ".sig",
	}
	for _, in := range cases {
		if c, ok := Parse(in); ok {
			t.Fatalf("Parse(%q) = (%+v, true), want false", in, c)
		}
	}
}

func TestParseAcceptsPaddedBase64AndExtraClaims(t *testing.T) {
	payload := base64.URLEncoding.EncodeToString([]byte(`{"oid":"abc","extra":"ignored"}`))
	got, ok := Parse("Bearer hdr." + payload + ".sig")
	if !ok {
		t.Fatal("expected padded base64 payload to parse")
	}
	if got.OID != "abc" {
		t.Fatalf("OID = %q, want abc", got.OID)
	}
}

func TestClientOID(t *testing.T) {
	if got := ClientOID(DefaultTenantID, ""); got != DefaultClientOID {
		t.Fatalf("empty client_id: got %q, want %q", got, DefaultClientOID)
	}
	if got := ClientOID(DefaultTenantID, DefaultClientID); got != DefaultClientOID {
		t.Fatalf("default client_id: got %q, want %q", got, DefaultClientOID)
	}
	if got := ClientOID("common", DefaultClientID); got != DefaultClientOID {
		t.Fatalf("common tenant + default client_id: got %q, want %q", got, DefaultClientOID)
	}

	a1 := ClientOID(DefaultTenantID, "client-a")
	a2 := ClientOID(DefaultTenantID, "client-a")
	b := ClientOID(DefaultTenantID, "client-b")
	if a1 != a2 {
		t.Fatalf("ClientOID not deterministic: %q vs %q", a1, a2)
	}
	if a1 == DefaultClientOID || a1 == b {
		t.Fatalf("expected distinct non-default OIDs, got a=%q b=%q", a1, b)
	}
	if _, err := uuid.Parse(a1); err != nil {
		t.Fatalf("expected valid UUID, got %q: %v", a1, err)
	}

	// Different tenant UUIDs produce different OIDs for the same client_id.
	otherTenant := "11111111-1111-1111-1111-111111111111"
	if got := ClientOID(otherTenant, "client-a"); got == a1 {
		t.Fatalf("expected different tenant to yield different OID, both were %q", got)
	}

	// Non-UUID tenant (e.g. "common") falls back to DefaultTenantID namespace.
	if got := ClientOID("common", "client-a"); got != a1 {
		t.Fatalf("non-UUID tenant: got %q, want %q", got, a1)
	}
}

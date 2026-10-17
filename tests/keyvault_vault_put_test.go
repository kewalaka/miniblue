package tests

import "testing"

func TestKeyVaultPutValidatesNameAndResourceGroup(t *testing.T) {
	ts := setupServer()
	defer ts.Close()

	const sub = "00000000-0000-0000-0000-000000000000"
	vaultURL := func(rg, name string) string {
		return ts.URL + "/subscriptions/" + sub + "/resourceGroups/" + rg + "/providers/Microsoft.KeyVault/vaults/" + name + "?api-version=2023-07-01"
	}
	body := `{"location":"eastus","properties":{"tenantId":"00000000-0000-0000-0000-000000000000","sku":{"family":"A","name":"standard"}}}`

	resp := doRequest(t, "PUT", vaultURL("missing-rg", "kv-ok"), body)
	defer resp.Body.Close()
	expectStatus(t, resp, 404)
	if code := decodeError(t, resp).Error.Code; code != "ResourceGroupNotFound" {
		t.Fatalf("expected ResourceGroupNotFound, got %q", code)
	}

	createRG(t, ts, sub, "rg1")

	for _, name := range []string{"ab", "1vault", "vault-", "va--ult", "this-vault-name-is-too-long-for-azure", "bad_name"} {
		resp := doRequest(t, "PUT", vaultURL("rg1", name), body)
		expectStatus(t, resp, 400)
		if code := decodeError(t, resp).Error.Code; code != "VaultNameNotValid" {
			t.Fatalf("%q: expected VaultNameNotValid, got %q", name, code)
		}
		resp.Body.Close()
	}

	resp = doRequest(t, "PUT", vaultURL("rg1", "a-b1"), body)
	defer resp.Body.Close()
	expectStatus(t, resp, 201)
}

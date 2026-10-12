package main

import (
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/moabukar/miniblue/internal/server"
)

func setupMiniblue() *httptest.Server {
	srv := server.New()
	return httptest.NewServer(srv.Handler())
}

func runAzlocal(ts *httptest.Server, args ...string) (string, string, int) {
	cwd, _ := os.Getwd()
	binPath := cwd + "/../../bin/azlocal"
	cmd := exec.Command(binPath, args...)
	cmd.Env = append(os.Environ(), "LOCAL_AZURE_ENDPOINT="+ts.URL)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return string(output), err.Error(), -1
	}
	return string(output), "", 0
}

func TestStorageAccountCreate(t *testing.T) {
	ts := setupMiniblue()
	defer ts.Close()

	output, _, _ := runAzlocal(ts, "storage", "account", "create",
		"--resource-group", "myRG",
		"--name", "testacct")

	if !strings.Contains(output, "name") || !strings.Contains(output, "testacct") {
		t.Fatalf("expected account name in output, got: %s", output)
	}
}

func TestStorageAccountCreateWithFlags(t *testing.T) {
	ts := setupMiniblue()
	defer ts.Close()

	output, _, _ := runAzlocal(ts, "storage", "account", "create",
		"--resource-group", "myRG",
		"--name", "testacct2",
		"--location", "westus2",
		"--sku", "Premium_LRS")

	if !strings.Contains(output, "name") {
		t.Fatalf("expected account response, got: %s", output)
	}
}

func TestStorageAccountList(t *testing.T) {
	ts := setupMiniblue()
	defer ts.Close()

	runAzlocal(ts, "storage", "account", "create", "--resource-group", "myRG", "--name", "acct1")
	runAzlocal(ts, "storage", "account", "create", "--resource-group", "myRG", "--name", "acct2")

	output, _, _ := runAzlocal(ts, "storage", "account", "list", "--resource-group", "myRG")

	if !strings.Contains(output, "acct1") || !strings.Contains(output, "acct2") {
		t.Fatalf("expected both accounts in list, got: %s", output)
	}
}

func TestStorageAccountShow(t *testing.T) {
	ts := setupMiniblue()
	defer ts.Close()

	runAzlocal(ts, "storage", "account", "create", "--resource-group", "myRG", "--name", "showacct")

	output, _, _ := runAzlocal(ts, "storage", "account", "show",
		"--resource-group", "myRG",
		"--name", "showacct")

	if !strings.Contains(output, "showacct") || !strings.Contains(output, "Microsoft.Storage/storageAccounts") {
		t.Fatalf("expected account details in output, got: %s", output)
	}
}

func TestStorageAccountShowNotFound(t *testing.T) {
	ts := setupMiniblue()
	defer ts.Close()

	output, _, _ := runAzlocal(ts, "storage", "account", "show",
		"--resource-group", "myRG",
		"--name", "nonexistent")

	if !strings.Contains(output, "404") && !strings.Contains(output, "NotFound") {
		t.Fatalf("expected not found error in output, got: %s", output)
	}
}

func TestStorageAccountListKeys(t *testing.T) {
	ts := setupMiniblue()
	defer ts.Close()

	runAzlocal(ts, "storage", "account", "create", "--resource-group", "myRG", "--name", "keyacct")

	output, _, _ := runAzlocal(ts, "storage", "account", "list-keys",
		"--resource-group", "myRG",
		"--name", "keyacct")

	if !strings.Contains(output, "key1") || !strings.Contains(output, "key2") {
		t.Fatalf("expected keys in output, got: %s", output)
	}
}

func TestStorageAccountDelete(t *testing.T) {
	ts := setupMiniblue()
	defer ts.Close()

	runAzlocal(ts, "storage", "account", "create", "--resource-group", "myRG", "--name", "deleteacct")

	output, _, _ := runAzlocal(ts, "storage", "account", "delete",
		"--resource-group", "myRG",
		"--name", "deleteacct")

	if !strings.Contains(strings.ToLower(output), "deleted") {
		t.Fatalf("expected delete confirmation, got: %s", output)
	}

	showOutput, _, _ := runAzlocal(ts, "storage", "account", "show",
		"--resource-group", "myRG",
		"--name", "deleteacct")

	if !strings.Contains(showOutput, "404") && !strings.Contains(showOutput, "NotFound") {
		t.Fatalf("expected not found error after deletion, got: %s", showOutput)
	}
}

func TestStorageAccountMissingResourceGroup(t *testing.T) {
	ts := setupMiniblue()
	defer ts.Close()

	_, _, code := runAzlocal(ts, "storage", "account", "create",
		"--name", "testacct")

	if code == 0 {
		t.Fatal("expected error for missing --resource-group")
	}
}

func TestVMLifecycleCLI(t *testing.T) {
	t.Setenv("MINIBLUE_DISABLE_DOCKER", "1")
	ts := setupMiniblue()
	defer ts.Close()

	runAzlocal(ts, "group", "create", "--name", "vmRG", "--location", "eastus")

	output, _, _ := runAzlocal(ts, "vm", "create",
		"--resource-group", "vmRG",
		"--name", "webvm",
		"--image", "ubuntu:24.04")
	if !strings.Contains(output, "webvm") || !strings.Contains(output, "running") {
		t.Fatalf("expected created VM in output, got: %s", output)
	}

	output, _, _ = runAzlocal(ts, "vm", "list", "--resource-group", "vmRG")
	if !strings.Contains(output, "webvm") {
		t.Fatalf("expected VM in list, got: %s", output)
	}

	output, _, _ = runAzlocal(ts, "vm", "show", "--resource-group", "vmRG", "--name", "webvm")
	if !strings.Contains(output, "Microsoft.Compute/virtualMachines") {
		t.Fatalf("expected VM type in show, got: %s", output)
	}

	output, _, _ = runAzlocal(ts, "vm", "delete", "--resource-group", "vmRG", "--name", "webvm")
	if !strings.Contains(strings.ToLower(output), "deleted") {
		t.Fatalf("expected delete confirmation, got: %s", output)
	}
}

func TestIdentityCLI(t *testing.T) {
	t.Setenv("MINIBLUE_DISABLE_DOCKER", "1")
	ts := setupMiniblue()
	defer ts.Close()

	runAzlocal(ts, "group", "create", "--name", "idRG", "--location", "eastus")

	output, _, _ := runAzlocal(ts, "identity", "create", "--resource-group", "idRG", "--name", "app-id")
	if !strings.Contains(output, "principalId") || !strings.Contains(output, "clientId") {
		t.Fatalf("expected identity properties in output, got: %s", output)
	}

	output, _, _ = runAzlocal(ts, "vm", "create",
		"--resource-group", "idRG",
		"--name", "idvm",
		"--identity", "app-id")
	if !strings.Contains(output, "UserAssigned") || !strings.Contains(output, "app-id") {
		t.Fatalf("expected identity assignment in VM output, got: %s", output)
	}

	output, _, _ = runAzlocal(ts, "identity", "list", "--resource-group", "idRG")
	if !strings.Contains(output, "app-id") {
		t.Fatalf("expected identity in list, got: %s", output)
	}
}

func TestProviderShowIncludesStorageAccountPreviewAPIVersion(t *testing.T) {
	ts := setupMiniblue()
	defer ts.Close()

	output, _, code := runAzlocal(ts, "provider", "show", "--namespace", "Microsoft.Storage")
	if code != 0 {
		t.Fatalf("provider show failed: %s", output)
	}
	if !strings.Contains(output, "storageAccounts") || !strings.Contains(output, "2022-05-01-preview") {
		t.Fatalf("expected storage account preview api-version in output, got: %s", output)
	}
}

func TestAppConfigStoreCommands(t *testing.T) {
	ts := setupMiniblue()
	defer ts.Close()

	output, _, _ := runAzlocal(ts, "appconfig", "store", "create",
		"--resource-group", "myRG",
		"--name", "appcfg",
		"--sku", "free")
	if !strings.Contains(output, "appcfg") || !strings.Contains(output, "Microsoft.AppConfiguration/configurationStores") {
		t.Fatalf("expected app configuration store in output, got: %s", output)
	}

	listOutput, _, _ := runAzlocal(ts, "appconfig", "store", "list", "--resource-group", "myRG")
	if !strings.Contains(listOutput, "appcfg") {
		t.Fatalf("expected store in list output, got: %s", listOutput)
	}

	keysOutput, _, _ := runAzlocal(ts, "appconfig", "store", "list-keys",
		"--resource-group", "myRG",
		"--name", "appcfg")
	if !strings.Contains(keysOutput, "Primary") || !strings.Contains(keysOutput, "miniblue-primary-key") {
		t.Fatalf("expected app configuration keys in output, got: %s", keysOutput)
	}

	deleteOutput, _, _ := runAzlocal(ts, "appconfig", "store", "delete",
		"--resource-group", "myRG",
		"--name", "appcfg")
	if !strings.Contains(strings.ToLower(deleteOutput), "deleted") {
		t.Fatalf("expected delete confirmation, got: %s", deleteOutput)
	}
}

func TestStorageAccountManagementPolicyCommands(t *testing.T) {
	ts := setupMiniblue()
	defer ts.Close()

	runAzlocal(ts, "storage", "account", "create", "--resource-group", "myRG", "--name", "policyacct")

	policyFile := filepath.Join(t.TempDir(), "policy.json")
	policyJSON := `{"properties":{"policy":{"enabled":true,"rules":[{"name":"delete-old-blobs","enabled":true,"type":"Lifecycle","definition":{"actions":{"baseBlob":{"delete":{"daysAfterModificationGreaterThan":7}}},"filters":{"blobTypes":["blockBlob"]}}}]}}}`
	if err := os.WriteFile(policyFile, []byte(policyJSON), 0600); err != nil {
		t.Fatal(err)
	}

	createOut, _, code := runAzlocal(ts, "storage", "account", "management-policy", "create",
		"--resource-group", "myRG",
		"--account-name", "policyacct",
		"--policy", "@"+policyFile)
	if code != 0 {
		t.Fatalf("management-policy create failed: %s", createOut)
	}
	if !strings.Contains(createOut, "delete-old-blobs") || !strings.Contains(createOut, "managementPolicies") {
		t.Fatalf("expected policy response, got: %s", createOut)
	}

	showOut, _, code := runAzlocal(ts, "storage", "account", "management-policy", "show",
		"--resource-group", "myRG",
		"--account-name", "policyacct")
	if code != 0 {
		t.Fatalf("management-policy show failed: %s", showOut)
	}
	if !strings.Contains(showOut, "delete-old-blobs") || !strings.Contains(showOut, "managementPolicies") {
		t.Fatalf("expected policy response, got: %s", showOut)
	}

	deleteOut, _, code := runAzlocal(ts, "storage", "account", "management-policy", "delete",
		"--resource-group", "myRG",
		"--account-name", "policyacct")
	if code != 0 {
		t.Fatalf("management-policy delete failed: %s", deleteOut)
	}
	if !strings.Contains(strings.ToLower(deleteOut), "deleted") {
		t.Fatalf("expected delete confirmation, got: %s", deleteOut)
	}
}

func TestKeyVaultSecretCommands(t *testing.T) {
	ts := setupMiniblue()
	defer ts.Close()

	setOut, _, code := runAzlocal(ts, "keyvault", "secret", "set",
		"--vault", "myvault",
		"--name", "db-pass",
		"--value", "supersecret")
	if code != 0 {
		t.Fatalf("keyvault secret set failed: %s", setOut)
	}
	if !strings.Contains(setOut, "supersecret") || !strings.Contains(setOut, "https://myvault.vault.azure.net/secrets/db-pass/") {
		t.Fatalf("expected secret value and versioned id in set output, got: %s", setOut)
	}

	showOut, _, code := runAzlocal(ts, "keyvault", "secret", "show",
		"--vault", "myvault",
		"--name", "db-pass")
	if code != 0 {
		t.Fatalf("keyvault secret show failed: %s", showOut)
	}
	if !strings.Contains(showOut, "supersecret") {
		t.Fatalf("expected secret value in show output, got: %s", showOut)
	}

	listOut, _, code := runAzlocal(ts, "keyvault", "secret", "list",
		"--vault", "myvault")
	if code != 0 {
		t.Fatalf("keyvault secret list failed: %s", listOut)
	}
	if !strings.Contains(listOut, "db-pass") || strings.Contains(listOut, "supersecret") {
		t.Fatalf("expected redacted secret metadata in list output, got: %s", listOut)
	}

	deleteOut, _, code := runAzlocal(ts, "keyvault", "secret", "delete",
		"--vault", "myvault",
		"--name", "db-pass")
	if code != 0 {
		t.Fatalf("keyvault secret delete failed: %s", deleteOut)
	}
	if !strings.Contains(strings.ToLower(deleteOut), "deleted") {
		t.Fatalf("expected delete confirmation, got: %s", deleteOut)
	}
}

func TestIdentityCreateShowUpdateListDelete(t *testing.T) {
	ts := setupMiniblue()
	defer ts.Close()

	runAzlocal(ts, "group", "create", "--name", "myRG", "--location", "eastus")
	createOut, _, code := runAzlocal(ts, "identity", "create",
		"--resource-group", "myRG",
		"--name", "id1",
		"--location", "westus2",
		"--tags", "env=test")
	if code != 0 {
		t.Fatalf("identity create failed: %s", createOut)
	}
	if !strings.Contains(createOut, "\"name\": \"id1\"") ||
		!strings.Contains(createOut, "Microsoft.ManagedIdentity/userAssignedIdentities") ||
		!strings.Contains(createOut, "principalId") ||
		!strings.Contains(createOut, "clientId") ||
		!strings.Contains(createOut, "tenantId") {
		t.Fatalf("expected identity details in create output, got: %s", createOut)
	}

	showOut, _, _ := runAzlocal(ts, "identity", "show",
		"--resource-group", "myRG",
		"--name", "id1")
	if !strings.Contains(showOut, "\"name\": \"id1\"") {
		t.Fatalf("expected identity name in show output, got: %s", showOut)
	}

	updateOut, _, code := runAzlocal(ts, "identity", "update",
		"--resource-group", "myRG",
		"--name", "id1",
		"--tags", "env=patched")
	if code != 0 {
		t.Fatalf("identity update failed: %s", updateOut)
	}
	if !strings.Contains(updateOut, "\"env\": \"patched\"") {
		t.Fatalf("expected patched tag, got: %s", updateOut)
	}

	listOut, _, _ := runAzlocal(ts, "identity", "list", "--resource-group", "myRG")
	if !strings.Contains(listOut, "\"name\": \"id1\"") {
		t.Fatalf("expected identity in list output, got: %s", listOut)
	}

	deleteOut, _, _ := runAzlocal(ts, "identity", "delete",
		"--resource-group", "myRG",
		"--name", "id1")
	if !strings.Contains(strings.ToLower(deleteOut), "deleted") {
		t.Fatalf("expected delete confirmation, got: %s", deleteOut)
	}
}

func TestRoleDefinitionListAndShow(t *testing.T) {
	ts := setupMiniblue()
	defer ts.Close()

	scope := "/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/webapp-lab-dev-rg/providers/Microsoft.DocumentDB/databaseAccounts/webapp-lab-dev-cosmos"
	listOut, _, code := runAzlocal(ts, "role", "definition", "list",
		"--scope", scope,
		"--name", "Cosmos DB Account Reader Role")
	if code != 0 {
		t.Fatalf("role definition list failed: %s", listOut)
	}
	if !strings.Contains(listOut, "Cosmos DB Account Reader Role") ||
		!strings.Contains(listOut, "fbdf93bf-df7d-467e-a4d2-9458aa1360c8") {
		t.Fatalf("expected filtered built-in role in list output, got: %s", listOut)
	}

	showOut, _, code := runAzlocal(ts, "role", "definition", "show",
		"--name", "Reader")
	if code != 0 {
		t.Fatalf("role definition show failed: %s", showOut)
	}
	if !strings.Contains(showOut, "\"roleName\": \"Reader\"") ||
		!strings.Contains(showOut, "Microsoft.Authorization/roleDefinitions") {
		t.Fatalf("expected Reader role definition in show output, got: %s", showOut)
	}
}

func TestRoleAssignmentCreateListDelete(t *testing.T) {
	ts := setupMiniblue()
	defer ts.Close()

	scope := "/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/myRG"
	name := "11111111-1111-1111-1111-111111111111"
	createOut, _, code := runAzlocal(ts, "role", "assignment", "create",
		"--scope", scope,
		"--name", name,
		"--assignee", "principal-1",
		"--role", "Reader")
	if code != 0 {
		t.Fatalf("role assignment create failed: %s", createOut)
	}
	if !strings.Contains(createOut, "\"principalId\": \"principal-1\"") ||
		!strings.Contains(createOut, "acdd72a7-3385-48ef-bd42-f606fba81ae7") ||
		!strings.Contains(createOut, scope) {
		t.Fatalf("expected role assignment details in create output, got: %s", createOut)
	}

	listOut, _, code := runAzlocal(ts, "role", "assignment", "list", "--scope", scope)
	if code != 0 {
		t.Fatalf("role assignment list failed: %s", listOut)
	}
	if !strings.Contains(listOut, name) || !strings.Contains(listOut, "principal-1") {
		t.Fatalf("expected role assignment in list output, got: %s", listOut)
	}

	deleteOut, _, code := runAzlocal(ts, "role", "assignment", "delete",
		"--scope", scope,
		"--name", name)
	if code != 0 {
		t.Fatalf("role assignment delete failed: %s", deleteOut)
	}
	if !strings.Contains(strings.ToLower(deleteOut), "deleted") {
		t.Fatalf("expected delete confirmation, got: %s", deleteOut)
	}
}

func TestRoleAssignmentKeyVaultScope(t *testing.T) {
	ts := setupMiniblue()
	defer ts.Close()

	scope := "/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/myRG/providers/Microsoft.KeyVault/vaults/probe-kv-001"
	name := "22222222-2222-2222-2222-222222222222"
	createOut, _, code := runAzlocal(ts, "role", "assignment", "create",
		"--scope", scope,
		"--name", name,
		"--assignee", "principal-1",
		"--role", "Key Vault Secrets User")
	if code != 0 {
		t.Fatalf("role assignment create failed: %s", createOut)
	}
	if !strings.Contains(createOut, "\"principalId\": \"principal-1\"") ||
		!strings.Contains(createOut, "4633458b-17de-408a-b874-0445c86b69e6") ||
		!strings.Contains(createOut, scope) {
		t.Fatalf("expected Key Vault role assignment details in create output, got: %s", createOut)
	}

	listOut, _, code := runAzlocal(ts, "role", "assignment", "list", "--scope", scope)
	if code != 0 {
		t.Fatalf("role assignment list failed: %s", listOut)
	}
	if !strings.Contains(listOut, name) || !strings.Contains(listOut, "principal-1") {
		t.Fatalf("expected Key Vault role assignment in list output, got: %s", listOut)
	}
}

func TestAzlocalExitsNonZeroForARMErrors(t *testing.T) {
	ts := setupMiniblue()
	defer ts.Close()

	output, _, code := runAzlocal(ts, "storage", "account", "show",
		"--resource-group", "myRG",
		"--name", "missingacct")
	if code == 0 {
		t.Fatalf("expected non-zero exit for ARM error, got output: %s", output)
	}
	if !strings.Contains(output, "NotFound") {
		t.Fatalf("expected ARM error response, got: %s", output)
	}
}

func TestCosmosDBTableCommands(t *testing.T) {
	ts := setupMiniblue()
	defer ts.Close()

	createOut, _, code := runAzlocal(ts, "cosmosdb", "table", "create",
		"--resource-group", "myRG",
		"--account", "acct1",
		"--name", "users",
		"--data", `{"properties":{"resource":{"id":"users"}}}`)
	if code != 0 {
		t.Fatalf("cosmosdb table create failed: %s", createOut)
	}
	if !strings.Contains(createOut, "\"name\": \"users\"") ||
		!strings.Contains(createOut, "Microsoft.DocumentDB/databaseAccounts/tables") {
		t.Fatalf("expected table details in create output, got: %s", createOut)
	}

	listOut, _, code := runAzlocal(ts, "cosmosdb", "table", "list",
		"--resource-group", "myRG",
		"--account", "acct1")
	if code != 0 {
		t.Fatalf("cosmosdb table list failed: %s", listOut)
	}
	if !strings.Contains(listOut, "\"name\": \"users\"") {
		t.Fatalf("expected table in list output, got: %s", listOut)
	}

	throughputOut, _, code := runAzlocal(ts, "cosmosdb", "table", "throughput", "update",
		"--resource-group", "myRG",
		"--account", "acct1",
		"--name", "users",
		"--data", `{"properties":{"resource":{"throughput":400}}}`)
	if code != 0 {
		t.Fatalf("cosmosdb table throughput update failed: %s", throughputOut)
	}
	if !strings.Contains(throughputOut, "\"name\": \"default\"") ||
		!strings.Contains(throughputOut, "\"throughput\": 400") {
		t.Fatalf("expected throughput details in update output, got: %s", throughputOut)
	}

	showThroughputOut, _, code := runAzlocal(ts, "cosmosdb", "table", "throughput", "show",
		"--resource-group", "myRG",
		"--account", "acct1",
		"--name", "users")
	if code != 0 {
		t.Fatalf("cosmosdb table throughput show failed: %s", showThroughputOut)
	}
	if !strings.Contains(showThroughputOut, "\"throughput\": 400") {
		t.Fatalf("expected throughput in show output, got: %s", showThroughputOut)
	}

	deleteOut, _, code := runAzlocal(ts, "cosmosdb", "table", "delete",
		"--resource-group", "myRG",
		"--account", "acct1",
		"--name", "users")
	if code != 0 {
		t.Fatalf("cosmosdb table delete failed: %s", deleteOut)
	}
	if !strings.Contains(strings.ToLower(deleteOut), "deleted") {
		t.Fatalf("expected delete confirmation, got: %s", deleteOut)
	}
}

// --- vm ---

func TestVMCreateShowListDelete(t *testing.T) {
	t.Setenv("MINIBLUE_DISABLE_DOCKER", "1")
	ts := setupMiniblue()
	defer ts.Close()

	runAzlocal(ts, "group", "create", "--name", "myRG", "--location", "eastus")

	createOut, _, code := runAzlocal(ts, "vm", "create",
		"--resource-group", "myRG",
		"--name", "vm1",
		"--location", "westus2",
		"--size", "Standard_B1s",
		"--image", "ubuntu:22.04",
		"--admin-username", "azureuser")
	if code != 0 {
		t.Fatalf("vm create failed: %s", createOut)
	}
	if !strings.Contains(createOut, "\"name\": \"vm1\"") ||
		!strings.Contains(createOut, "Microsoft.Compute/virtualMachines") ||
		!strings.Contains(createOut, "Standard_B1s") ||
		!strings.Contains(createOut, "azureuser") {
		t.Fatalf("expected VM details in create output, got: %s", createOut)
	}

	showOut, _, _ := runAzlocal(ts, "vm", "show",
		"--resource-group", "myRG",
		"--name", "vm1")
	if !strings.Contains(showOut, "\"name\": \"vm1\"") {
		t.Fatalf("expected VM name in show output, got: %s", showOut)
	}

	listOut, _, _ := runAzlocal(ts, "vm", "list", "--resource-group", "myRG")
	if !strings.Contains(listOut, "\"name\": \"vm1\"") {
		t.Fatalf("expected VM in list output, got: %s", listOut)
	}

	deleteOut, _, _ := runAzlocal(ts, "vm", "delete",
		"--resource-group", "myRG",
		"--name", "vm1")
	if !strings.Contains(strings.ToLower(deleteOut), "deleted") {
		t.Fatalf("expected delete confirmation, got: %s", deleteOut)
	}
}

func TestVMPowerActionsAndInstanceView(t *testing.T) {
	t.Setenv("MINIBLUE_DISABLE_DOCKER", "1")
	ts := setupMiniblue()
	defer ts.Close()

	runAzlocal(ts, "group", "create", "--name", "myRG", "--location", "eastus")
	runAzlocal(ts, "vm", "create", "--resource-group", "myRG", "--name", "vm1")

	stopOut, _, code := runAzlocal(ts, "vm", "stop", "--resource-group", "myRG", "--name", "vm1")
	if code != 0 {
		t.Fatalf("vm stop failed: %s", stopOut)
	}

	instanceViewOut, _, _ := runAzlocal(ts, "vm", "get-instance-view",
		"--resource-group", "myRG",
		"--name", "vm1")
	if !strings.Contains(instanceViewOut, "PowerState/stopped") {
		t.Fatalf("expected stopped power state, got: %s", instanceViewOut)
	}

	startOut, _, code := runAzlocal(ts, "vm", "start", "--resource-group", "myRG", "--name", "vm1")
	if code != 0 {
		t.Fatalf("vm start failed: %s", startOut)
	}
}

func TestVMExtensionLifecycle(t *testing.T) {
	t.Setenv("MINIBLUE_DISABLE_DOCKER", "1")
	ts := setupMiniblue()
	defer ts.Close()

	runAzlocal(ts, "group", "create", "--name", "myRG", "--location", "eastus")
	runAzlocal(ts, "vm", "create", "--resource-group", "myRG", "--name", "vm1")

	setOut, _, code := runAzlocal(ts, "vm", "extension", "set",
		"--resource-group", "myRG",
		"--vm-name", "vm1",
		"--name", "customScript",
		"--publisher", "Microsoft.Azure.Extensions",
		"--type", "CustomScript",
		"--settings", `{"commandToExecute":"echo ok"}`)
	if code != 0 {
		t.Fatalf("vm extension set failed: %s", setOut)
	}
	if !strings.Contains(setOut, "\"name\": \"customScript\"") {
		t.Fatalf("expected extension name in set output, got: %s", setOut)
	}

	listOut, _, _ := runAzlocal(ts, "vm", "extension", "list",
		"--resource-group", "myRG",
		"--vm-name", "vm1")
	if !strings.Contains(listOut, "customScript") {
		t.Fatalf("expected extension in list output, got: %s", listOut)
	}

	deleteOut, _, _ := runAzlocal(ts, "vm", "extension", "delete",
		"--resource-group", "myRG",
		"--vm-name", "vm1",
		"--name", "customScript")
	if !strings.Contains(strings.ToLower(deleteOut), "deleted") {
		t.Fatalf("expected extension delete confirmation, got: %s", deleteOut)
	}
}

// --- network vnet subnet ---

func TestNetworkVNetSubnetCreateAndShow(t *testing.T) {
	ts := setupMiniblue()
	defer ts.Close()

	runAzlocal(ts, "network", "vnet", "create",
		"--resource-group", "myRG", "--name", "vnet1",
		"--address-prefix", "10.0.0.0/16")

	createOut, _, code := runAzlocal(ts, "network", "vnet", "subnet", "create",
		"--resource-group", "myRG", "--vnet-name", "vnet1",
		"--name", "web", "--address-prefixes", "10.0.1.0/24")
	if code != 0 {
		t.Fatalf("subnet create failed: %s", createOut)
	}
	if !strings.Contains(createOut, "\"name\": \"web\"") || !strings.Contains(createOut, "10.0.1.0/24") {
		t.Fatalf("expected subnet name and prefix in create output, got: %s", createOut)
	}

	showOut, _, _ := runAzlocal(ts, "network", "vnet", "subnet", "show",
		"--resource-group", "myRG", "--vnet-name", "vnet1", "--name", "web")
	if !strings.Contains(showOut, "Microsoft.Network/virtualNetworks/subnets") {
		t.Fatalf("expected subnet type in show output, got: %s", showOut)
	}
}

func TestNetworkVNetSubnetList(t *testing.T) {
	ts := setupMiniblue()
	defer ts.Close()

	runAzlocal(ts, "network", "vnet", "create",
		"--resource-group", "myRG", "--name", "vnet1",
		"--address-prefix", "10.0.0.0/16")
	runAzlocal(ts, "network", "vnet", "subnet", "create",
		"--resource-group", "myRG", "--vnet-name", "vnet1",
		"--name", "web", "--address-prefixes", "10.0.1.0/24")
	runAzlocal(ts, "network", "vnet", "subnet", "create",
		"--resource-group", "myRG", "--vnet-name", "vnet1",
		"--name", "app", "--address-prefixes", "10.0.2.0/24")

	out, _, _ := runAzlocal(ts, "network", "vnet", "subnet", "list",
		"--resource-group", "myRG", "--vnet-name", "vnet1")
	if !strings.Contains(out, "web") || !strings.Contains(out, "app") {
		t.Fatalf("expected both subnets in list, got: %s", out)
	}
}

func TestNetworkVNetSubnetUpdate(t *testing.T) {
	ts := setupMiniblue()
	defer ts.Close()

	runAzlocal(ts, "network", "vnet", "create",
		"--resource-group", "myRG", "--name", "vnet1",
		"--address-prefix", "10.0.0.0/16")
	runAzlocal(ts, "network", "vnet", "subnet", "create",
		"--resource-group", "myRG", "--vnet-name", "vnet1",
		"--name", "web", "--address-prefixes", "10.0.1.0/24")

	out, _, code := runAzlocal(ts, "network", "vnet", "subnet", "update",
		"--resource-group", "myRG", "--vnet-name", "vnet1",
		"--name", "web", "--address-prefixes", "10.0.5.0/24")
	if code != 0 {
		t.Fatalf("subnet update failed: %s", out)
	}
	if !strings.Contains(out, "10.0.5.0/24") {
		t.Fatalf("expected updated prefix in output, got: %s", out)
	}
}

func TestNetworkVNetSubnetDelete(t *testing.T) {
	ts := setupMiniblue()
	defer ts.Close()

	runAzlocal(ts, "network", "vnet", "create",
		"--resource-group", "myRG", "--name", "vnet1",
		"--address-prefix", "10.0.0.0/16")
	runAzlocal(ts, "network", "vnet", "subnet", "create",
		"--resource-group", "myRG", "--vnet-name", "vnet1",
		"--name", "web", "--address-prefixes", "10.0.1.0/24")

	out, _, _ := runAzlocal(ts, "network", "vnet", "subnet", "delete",
		"--resource-group", "myRG", "--vnet-name", "vnet1", "--name", "web")
	if !strings.Contains(strings.ToLower(out), "deleted") {
		t.Fatalf("expected delete confirmation, got: %s", out)
	}

	showOut, _, _ := runAzlocal(ts, "network", "vnet", "subnet", "show",
		"--resource-group", "myRG", "--vnet-name", "vnet1", "--name", "web")
	if !strings.Contains(showOut, "404") && !strings.Contains(showOut, "NotFound") {
		t.Fatalf("expected not-found after delete, got: %s", showOut)
	}
}

func TestNetworkVNetSubnetUnknownAction(t *testing.T) {
	ts := setupMiniblue()
	defer ts.Close()

	out, _, code := runAzlocal(ts, "network", "vnet", "subnet", "frobnicate",
		"--resource-group", "myRG", "--vnet-name", "vnet1")
	if code == 0 {
		t.Fatalf("expected non-zero exit for unknown subnet action, got 0; output: %s", out)
	}
	if !strings.Contains(out, "Unknown subcommand") {
		t.Fatalf("expected 'Unknown subcommand' in output, got: %s", out)
	}
}

func TestNetworkVNetUnknownAction(t *testing.T) {
	ts := setupMiniblue()
	defer ts.Close()

	out, _, code := runAzlocal(ts, "network", "vnet", "frobnicate",
		"--resource-group", "myRG", "--name", "vnet1")
	if code == 0 {
		t.Fatalf("expected non-zero exit for unknown vnet action, got 0; output: %s", out)
	}
	if !strings.Contains(out, "Unknown subcommand") {
		t.Fatalf("expected 'Unknown subcommand' in output, got: %s", out)
	}
}

func TestNetworkUnknownResource(t *testing.T) {
	ts := setupMiniblue()
	defer ts.Close()

	out, _, code := runAzlocal(ts, "network", "frobnicate", "list")
	if code == 0 {
		t.Fatalf("expected non-zero exit for unknown network resource, got 0; output: %s", out)
	}
	if !strings.Contains(out, "Unknown subcommand") {
		t.Fatalf("expected 'Unknown subcommand' in output, got: %s", out)
	}
}

// --- network nsg ---

func TestNetworkNSGCreateShowListDelete(t *testing.T) {
	ts := setupMiniblue()
	defer ts.Close()

	createOut, _, code := runAzlocal(ts, "network", "nsg", "create",
		"--resource-group", "myRG", "--name", "mynsg", "--location", "eastus")
	if code != 0 {
		t.Fatalf("nsg create failed: %s", createOut)
	}
	if !strings.Contains(createOut, "\"name\": \"mynsg\"") ||
		!strings.Contains(createOut, "Microsoft.Network/networkSecurityGroups") {
		t.Fatalf("expected nsg name and type in create output, got: %s", createOut)
	}

	showOut, _, _ := runAzlocal(ts, "network", "nsg", "show",
		"--resource-group", "myRG", "--name", "mynsg")
	if !strings.Contains(showOut, "\"name\": \"mynsg\"") {
		t.Fatalf("expected nsg in show output, got: %s", showOut)
	}

	listOut, _, _ := runAzlocal(ts, "network", "nsg", "list",
		"--resource-group", "myRG")
	if !strings.Contains(listOut, "mynsg") {
		t.Fatalf("expected nsg in list output, got: %s", listOut)
	}

	delOut, _, _ := runAzlocal(ts, "network", "nsg", "delete",
		"--resource-group", "myRG", "--name", "mynsg")
	if !strings.Contains(strings.ToLower(delOut), "deleted") {
		t.Fatalf("expected delete confirmation, got: %s", delOut)
	}
}

func TestNetworkNSGRuleListAndShow(t *testing.T) {
	ts := setupMiniblue()
	defer ts.Close()

	runAzlocal(ts, "network", "nsg", "create",
		"--resource-group", "myRG", "--name", "mynsg")

	createOut, _, code := runAzlocal(ts, "network", "nsg", "rule", "create",
		"--resource-group", "myRG", "--nsg-name", "mynsg", "--name", "allowSSH")
	if code != 0 {
		t.Fatalf("nsg rule create failed: %s", createOut)
	}
	if !strings.Contains(createOut, "allowSSH") {
		t.Fatalf("expected rule name in create output, got: %s", createOut)
	}

	listOut, _, _ := runAzlocal(ts, "network", "nsg", "rule", "list",
		"--resource-group", "myRG", "--nsg-name", "mynsg")
	if !strings.Contains(listOut, "allowSSH") {
		t.Fatalf("expected rule in list output, got: %s", listOut)
	}
}

// --- network lb ---

func TestNetworkLBCreateShowListDelete(t *testing.T) {
	ts := setupMiniblue()
	defer ts.Close()

	createOut, _, code := runAzlocal(ts, "network", "lb", "create",
		"--resource-group", "myRG", "--name", "mylb",
		"--location", "eastus", "--sku", "Standard")
	if code != 0 {
		t.Fatalf("lb create failed: %s", createOut)
	}
	if !strings.Contains(createOut, "\"name\": \"mylb\"") ||
		!strings.Contains(createOut, "Microsoft.Network/loadBalancers") {
		t.Fatalf("expected lb name and type in create output, got: %s", createOut)
	}

	showOut, _, _ := runAzlocal(ts, "network", "lb", "show",
		"--resource-group", "myRG", "--name", "mylb")
	if !strings.Contains(showOut, "\"name\": \"mylb\"") {
		t.Fatalf("expected lb in show output, got: %s", showOut)
	}

	listOut, _, _ := runAzlocal(ts, "network", "lb", "list",
		"--resource-group", "myRG")
	if !strings.Contains(listOut, "mylb") {
		t.Fatalf("expected lb in list output, got: %s", listOut)
	}

	delOut, _, _ := runAzlocal(ts, "network", "lb", "delete",
		"--resource-group", "myRG", "--name", "mylb")
	if !strings.Contains(strings.ToLower(delOut), "deleted") {
		t.Fatalf("expected delete confirmation, got: %s", delOut)
	}
}

func TestNetworkLBRuleAndProbeList(t *testing.T) {
	ts := setupMiniblue()
	defer ts.Close()

	runAzlocal(ts, "network", "lb", "create",
		"--resource-group", "myRG", "--name", "mylb")

	ruleListOut, _, code := runAzlocal(ts, "network", "lb", "rule", "list",
		"--resource-group", "myRG", "--lb-name", "mylb")
	if code != 0 {
		t.Fatalf("lb rule list failed: %s", ruleListOut)
	}
	if !strings.Contains(ruleListOut, "value") {
		t.Fatalf("expected value array in rule list output, got: %s", ruleListOut)
	}

	probeListOut, _, code := runAzlocal(ts, "network", "lb", "probe", "list",
		"--resource-group", "myRG", "--lb-name", "mylb")
	if code != 0 {
		t.Fatalf("lb probe list failed: %s", probeListOut)
	}
	if !strings.Contains(probeListOut, "value") {
		t.Fatalf("expected value array in probe list output, got: %s", probeListOut)
	}
}

// --- cosmosdb account ---

func TestCosmosDBAccountCreateShowListDelete(t *testing.T) {
	ts := setupMiniblue()
	defer ts.Close()

	createOut, _, code := runAzlocal(ts, "cosmosdb", "create",
		"--resource-group", "myRG", "--name", "myaccount", "--location", "eastus")
	if code != 0 {
		t.Fatalf("cosmosdb create failed: %s", createOut)
	}
	if !strings.Contains(createOut, "\"name\": \"myaccount\"") ||
		!strings.Contains(createOut, "Microsoft.DocumentDB/databaseAccounts") {
		t.Fatalf("expected account name and type in create output, got: %s", createOut)
	}

	showOut, _, _ := runAzlocal(ts, "cosmosdb", "show",
		"--resource-group", "myRG", "--name", "myaccount")
	if !strings.Contains(showOut, "\"name\": \"myaccount\"") {
		t.Fatalf("expected account in show output, got: %s", showOut)
	}

	listOut, _, _ := runAzlocal(ts, "cosmosdb", "list",
		"--resource-group", "myRG")
	if !strings.Contains(listOut, "myaccount") {
		t.Fatalf("expected account in list output, got: %s", listOut)
	}

	delOut, _, _ := runAzlocal(ts, "cosmosdb", "delete",
		"--resource-group", "myRG", "--name", "myaccount")
	if !strings.Contains(strings.ToLower(delOut), "deleted") {
		t.Fatalf("expected delete confirmation, got: %s", delOut)
	}
}

func TestCosmosDBUnknownSubcommand(t *testing.T) {
	ts := setupMiniblue()
	defer ts.Close()

	out, _, code := runAzlocal(ts, "cosmosdb", "frobnicate")
	if code == 0 {
		t.Fatalf("expected non-zero exit for unknown cosmosdb subcommand, got 0; output: %s", out)
	}
	if !strings.Contains(out, "Unknown subcommand") {
		t.Fatalf("expected 'Unknown subcommand' in output, got: %s", out)
	}
}


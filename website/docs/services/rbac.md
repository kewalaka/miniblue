# Role-Based Access Control (RBAC)

miniblue emulates `Microsoft.Authorization` role assignment and role
definition endpoints. It ships a fixed catalog of common built-in roles and
accepts custom role definitions and assignments at any scope.

This is sufficient for Terraform's `azurerm_role_assignment` /
`azurerm_role_definition` resources and for the Azure CLI `az role` commands
to plan and apply against miniblue. **No access control is enforced** —
assignments are stored for round-trip fidelity only.

## ARM endpoints

The routes are registered at every common Azure scope: subscription, resource
group, and individual resources (one, two and three levels deep).

| Method | Path (relative to `{scope}`) | Description |
|--------|------------------------------|-------------|
| `PUT` | `{scope}/providers/Microsoft.Authorization/roleAssignments/{name}` | Create assignment |
| `GET` | `{scope}/providers/Microsoft.Authorization/roleAssignments/{name}` | Get assignment |
| `DELETE` | `{scope}/providers/Microsoft.Authorization/roleAssignments/{name}` | Delete assignment |
| `GET` | `{scope}/providers/Microsoft.Authorization/roleAssignments` | List assignments |
| `PUT` | `{scope}/providers/Microsoft.Authorization/roleDefinitions/{name}` | Create custom role |
| `GET` | `{scope}/providers/Microsoft.Authorization/roleDefinitions/{name}` | Get role definition |
| `GET` | `{scope}/providers/Microsoft.Authorization/roleDefinitions` | List role definitions (supports `$filter=roleName eq '...'`) |
| `DELETE` | `{scope}/providers/Microsoft.Authorization/roleDefinitions/{name}` | Delete custom role |

`{scope}` can be any of:

- `/subscriptions/{sub}`
- `/subscriptions/{sub}/resourceGroups/{rg}`
- `/subscriptions/{sub}/resourceGroups/{rg}/providers/{provider}/{type}/{name}` (and deeper)

## Built-in roles

The following built-in roles are always returned by `roleDefinitions` and can
be referenced by name from the azlocal CLI:

| Role | Built-in role ID |
|------|------------------|
| Reader | `acdd72a7-3385-48ef-bd42-f606fba81ae7` |
| Contributor | `b24988ac-6180-42a0-ab88-20f7382dd24c` |
| Owner | `8e3af657-a8ff-443c-a75c-2fe8c4bcb635` |
| Storage Blob Data Reader | `2a2b9908-6ea1-4ae2-8e65-a410df84e7d1` |
| Storage Blob Data Contributor | `ba92f5b4-2d11-453d-a403-e96b0029c9fe` |
| Key Vault Secrets User | `4633458b-17de-408a-b874-0445c86b69e6` |
| Key Vault Secrets Officer | `b86a8fe4-44ce-4948-aee5-eccb2c155cd7` |
| App Configuration Data Reader | `516239f1-63e1-4d78-a4de-a74fb236a071` |
| Cosmos DB Account Reader Role | `fbdf93bf-df7d-467e-a4d2-9458aa1360c8` |

## Create a role assignment

```bash
curl -X PUT \
  "http://localhost:4566/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/myRG/providers/Microsoft.Authorization/roleAssignments/$(uuidgen)?api-version=2022-04-01" \
  -H "Content-Type: application/json" \
  -d '{
    "properties": {
      "principalId":      "11111111-1111-1111-1111-111111111111",
      "principalType":    "ServicePrincipal",
      "roleDefinitionId": "/subscriptions/00000000-0000-0000-0000-000000000000/providers/Microsoft.Authorization/roleDefinitions/b24988ac-6180-42a0-ab88-20f7382dd24c"
    }
  }'
```

## Terraform

```hcl
resource "azurerm_role_assignment" "contributor" {
  scope                = azurerm_resource_group.example.id
  role_definition_name = "Contributor"
  principal_id         = azurerm_user_assigned_identity.example.principal_id
}
```

## azlocal

```bash
# Assignments
azlocal role assignment create --assignee 11111111-1111-1111-1111-111111111111 \
  --role Contributor --resource-group myRG
azlocal role assignment list   --resource-group myRG
azlocal role assignment delete --name <assignment-guid> --resource-group myRG

# Definitions
azlocal role definition list
azlocal role definition show --name Reader
azlocal role definition create --name MyCustomRole \
  --description "Read storage accounts" --scope /subscriptions/00000000-0000-0000-0000-000000000000
```

When `--name` is not supplied on `assignment create`, azlocal derives a
deterministic GUID from `{scope, principalId, roleDefinitionId}` so repeated
applies remain idempotent.

## Limitations

- No access control is enforced — every other miniblue service still serves
  every request without authorization checks.
- No deny assignments, no eligible/PIM role assignments, no scope inheritance
  evaluation.
- The built-in role catalog is intentionally small; custom roles created via
  PUT are stored verbatim.

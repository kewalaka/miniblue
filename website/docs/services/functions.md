# Azure Functions

miniblue emulates Azure Functions via ARM endpoints using the `Microsoft.Web/sites` handler (`internal/services/sites`, registered under `SERVICES=sites`), shared with App Service. Sites default to `kind: "functionapp"` when `kind` is omitted, and can be created, updated, listed and deleted along with deployment slots, site configuration and App Service plans, but no function execution is performed.

## API endpoints

| Method | Path | Description |
|--------|------|-------------|
| `PUT` | `/subscriptions/{sub}/resourceGroups/{rg}/providers/Microsoft.Web/sites/{name}` | Create or update function app / site |
| `GET` | `/subscriptions/{sub}/resourceGroups/{rg}/providers/Microsoft.Web/sites/{name}` | Get function app / site |
| `DELETE` | `/subscriptions/{sub}/resourceGroups/{rg}/providers/Microsoft.Web/sites/{name}` | Delete function app / site |
| `GET` | `/subscriptions/{sub}/resourceGroups/{rg}/providers/Microsoft.Web/sites` | List function apps / sites |
| `PUT` / `GET` / `DELETE` / `GET` | `/subscriptions/{sub}/resourceGroups/{rg}/providers/Microsoft.Web/sites/{name}/slots[/{slotName}]` | Manage deployment slots |
| `GET` / `PUT` / `POST` | `/subscriptions/{sub}/resourceGroups/{rg}/providers/Microsoft.Web/sites/{name}/config/...` | Manage site config (`web`, `appSettings`, `connectionstrings`, `authsettings`, `authsettingsV2`, `logs`, `metadata`, `backup`, `azurestorageaccounts`, `pushsettings`, `slotConfigNames`, `publishingcredentials/list`) |
| `GET` / `PUT` | `/subscriptions/{sub}/resourceGroups/{rg}/providers/Microsoft.Web/sites/{name}/basicPublishingCredentialsPolicies[/{ftp\|scm}]` | Manage publishing credential policies |
| `PUT` / `GET` / `DELETE` / `GET` | `/subscriptions/{sub}/resourceGroups/{rg}/providers/Microsoft.Web/serverFarms[/{name}]` | Manage App Service plans |
| `POST` | `/subscriptions/{sub}/providers/Microsoft.Web/checknameavailability` | Check site name availability |

## Limitations

- Stored control-plane state only: no function execution, triggers or runtime hosting
- No Kudu/SCM endpoint or slot swap

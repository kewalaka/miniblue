# App Configuration

miniblue emulates Azure App Configuration. Both ARM endpoints (for managing
configuration stores) and the key-value data plane are implemented.

## API endpoints

### ARM (control plane)

| Method | Path | Description |
|--------|------|-------------|
| `PUT` | `/subscriptions/{sub}/resourceGroups/{rg}/providers/Microsoft.AppConfiguration/configurationStores/{name}` | Create or update store |
| `GET` | `/subscriptions/{sub}/resourceGroups/{rg}/providers/Microsoft.AppConfiguration/configurationStores/{name}` | Get store |
| `DELETE` | `/subscriptions/{sub}/resourceGroups/{rg}/providers/Microsoft.AppConfiguration/configurationStores/{name}` | Delete store |
| `GET` | `/subscriptions/{sub}/resourceGroups/{rg}/providers/Microsoft.AppConfiguration/configurationStores` | List stores |

### Data plane (key-values)

| Method | Path | Description |
|--------|------|-------------|
| `PUT` | `/appconfig/{store}/kv/{key}` | Set key-value |
| `GET` | `/appconfig/{store}/kv/{key}` | Get key-value |
| `GET` | `/appconfig/{store}/kv` | List key-values |
| `DELETE` | `/appconfig/{store}/kv/{key}` | Delete key-value |

The data plane is **also** mounted at the root path (`/kv/...`) so that
requests issued against `https://{store}.azconfig.io/kv/{key}` resolve when
DNS or hosts-file entries point those names at the miniblue HTTPS listener.
The TLS certificate includes `*.azconfig.io` to cover this case.

## Limitations

- No labels or feature flags
- No configuration snapshots
- No event notifications

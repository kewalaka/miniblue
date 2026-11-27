# Table Storage

miniblue emulates Azure Table Storage via data plane endpoints. It supports creating tables and upserting, getting, querying and deleting entities with partition and row keys.

## API endpoints

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/table/{account}/Tables` | Create table |
| `POST` | `/table/{account}/{table}` | Upsert entity |
| `GET` | `/table/{account}/{table}(PartitionKey='{pk}',RowKey='{rk}')` | Get entity |
| `GET` | `/table/{account}/{table}()` | Query entities |
| `DELETE` | `/table/{account}/{table}(PartitionKey='{pk}',RowKey='{rk}')` | Delete entity |

## Limitations

- OData query filtering is not supported.
- Batch operations are not supported.
- Continuation tokens for large result sets are not supported.

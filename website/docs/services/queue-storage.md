# Queue Storage

miniblue emulates Azure Queue Storage via data plane endpoints. It supports creating, clearing and deleting queues, plus sending, receiving and peeking messages with dequeue count tracking.

## API endpoints

| Method | Path | Description |
|--------|------|-------------|
| `PUT` | `/queue/{account}/{queue}` | Create queue |
| `POST` | `/queue/{account}/{queue}/messages` | Send message |
| `GET` | `/queue/{account}/{queue}/messages` | Receive messages |
| `GET` | `/queue/{account}/{queue}/messages?peekonly=true` | Peek messages |
| `DELETE` | `/queue/{account}/{queue}/messages` | Clear messages |
| `DELETE` | `/queue/{account}/{queue}` | Delete queue |

## Limitations

- Visibility timeout on receive is not supported.
- Message TTL is not supported.
- Poison queues are not supported.

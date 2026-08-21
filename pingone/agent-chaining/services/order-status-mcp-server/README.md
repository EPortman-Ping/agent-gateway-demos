# Order Status MCP Server

A protected, deterministic MCP server exposing one read-only capability: `get_order_status`.

The Order Status Agent is the only agent intended to call this service. The server validates the final token audience, scope, actor, and user subject before returning mock order data.

## Configure

```bash
cp .env.sample .env
```

Production configuration uses a PingOne issuer, the `order-status-mcp-server` audience, and the `order:read` scope. Local mode uses claims-shaped `local-rfc8693.*` tokens only as a development stand-in.

## Run

```bash
make run
```

The MCP endpoint is:

```text
http://localhost:8082/mcp
```

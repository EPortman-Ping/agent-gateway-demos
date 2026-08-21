# Order Status MCP Server

A Go Streamable HTTP MCP server deployed to Cloud Run. It exposes one protected read-only capability: `get_order_status`.

## Configure and deploy

```bash
cp .env.sample .env
make build
make deploy
```

The resulting endpoint is:

```text
https://<cloud-run-service-url>/mcp
```

Copy that URL into `MCP_ORDER_STATUS_SERVER_URL` in the Order Status Agent configuration.

## Configuration

| Variable | Purpose |
|---|---|
| `GC_REGION` | Cloud Run region |
| `GC_CLOUD_RUN_SERVICE_NAME` | Cloud Run service name |
| `IDP_ISSUER` | PingOne issuer; JWKS is derived as `<issuer>/jwks` |
| `IDP_REQUIRED_AUDIENCE` | `order-status-mcp-server` |
| `IDP_REQUIRED_SCOPE` | `order:read` |

## Security

The service is deployed with `--allow-unauthenticated` so the Agent Gateway can reach Cloud Run. Authentication is enforced in the application: every request must contain a signed PingOne JWT with the configured issuer, audience, expiry, and scope. The server validates the token against PingOne JWKS before passing the request to the MCP handler.

The service has no local Uvicorn mode and does not accept development-only tokens. It is a Cloud Run deployment unit only.

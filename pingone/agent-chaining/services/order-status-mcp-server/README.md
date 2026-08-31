# Order Status MCP Server

An MCP server exposing a `get_order_status` tool. Deployed on Cloud Run and registered as an MCP Server in Agent Registry. This service holds the order data and is the final enforcement point for the whole agent chaining delegation model.

## Configure

### 1. Create the Order Status MCP Server Resource in PingOne

In PingOne, create a **Resource** named `AC Order Status MCP Server` with the `order:read` scope and `order-status-mcp-server` audience.
![Order Status MCP Server Resource Config]()

### 2. Make this resource prove who delegated to whom (`act` claim)

On the `AC Order Status MCP Server` resource, go to the **Attributes** tab and configure two attributes:

1. **`sub`** — click its gear icon to open Advanced Expressions and enter:
   ```text
   (#root.context.requestData.grantType == "client_credentials") ? "no-subject" : #root.context.requestData.subjectToken.sub
   ```
   The `subjectToken.sub` half carries the subject token's `sub` through to the exchanged token, so the customer's identity survives this hop. **The `client_credentials` branch is required, not optional**: the gateway extension's own actor-token fetch for the `order:read` scope is a plain `client_credentials` request against this same resource, and that grant type has no `subjectToken` — leaving this expression as `#root.context.requestData.subjectToken.sub` alone 400s that call with `sub is configured as required for the Access token but does not have a value`, which blocks the entire MCP hop.

2. **`act`** — click **Add**, name it `act`, open its Advanced Expressions, and enter:
   ```text
   (#root.context.requestData.grantType == "client_credentials")?"noActor":((#root.context.requestData.subjectToken.may_act.sub == #root.context.requestData.actorToken.client_id)?#root.context.requestData.subjectToken.may_act:null)
   ```
   Check **Required**. `client_credentials` requests (each service minting its own actor token) get `noActor`. Token-exchange requests get the subject token's `may_act` value only if it names the current actor — otherwise `null`, which fails the exchange closed instead of issuing an unproven delegation.

### 3. Configure environment values

```bash
cp .env.sample .env
```

| Variable | Value |
|---|---|
| `GC_REGION` | GCP region, e.g. `us-central1` |
| `GC_CLOUD_RUN_SERVICE_NAME` | Cloud Run service name, e.g. `ac-order-status-mcp-server` |
| `IDP_ISSUER` | PingOne issuer URL, e.g. `https://auth.pingone.com/<env-id>/as`. |
| `IDP_REQUIRED_AUDIENCE` | Expected `aud` claim, e.g. `order-status-mcp-server` |
| `IDP_REQUIRED_SCOPE` | Scope the inbound token must carry, e.g. `order:read` |

## Deploy

```bash
make deploy
```

`deploy` runs `setup`, then `push`, then `gcloud run deploy`.

## Register

Register the server in the Agent Registry (Agent Platform → Govern → Agent Registry → Add MCP Server):
- **Name:** `ac-order-status-mcp-server`
- **Description:** Order status MCP server for the Agent Chaining demo
- **Region:** Same as the Cloud Run deployment (e.g. `us-central1`)
- **MCP Server URL:** `<Cloud Run service URL>/mcp`
- **Tool specification JSON:** Paste the contents of `tool-spec.json`

![Order Status MCP Server GCP Config]()

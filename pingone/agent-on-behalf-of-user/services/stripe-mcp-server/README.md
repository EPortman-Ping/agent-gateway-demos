# Stripe MCP Server

An MCP server exposing Stripe tools. Deployed on Cloud Run, registered as an MCP Server in Agent Platform.

On every request it:
1. Validates `iss`, `aud`, `scopes`, and `signature` of the inbound token via PingOne JWKS
2. Extracts the `sub` from the verified token, obtains a management token via `client_credentials`, and looks up the user's email from the PingOne management API
3. Executes Stripe operations on behalf of that user

## Configure

### 1. Stripe Setup

In the **Stripe Dashboard**, create the products and customers the agent will operate on. Each customer must have a saved payment method attached to their account so that `create_stripe_payment_intent` can charge them without requiring card details at runtime. The customer email in stripe must match the user email in PingOne.

### 2. PingOne Worker Application

Create a **Worker** application in PingOne for the MCP server to authenticate as:
- **Name:** `aobou-stripe-mcp-server`
- **Grant type:** Client Credentials
- **Scopes:** `p1:read:user` (to look up users by sub via the management API)

### 3. Configure environment values

```bash
cp .env.sample .env
```

| Variable | Value |
|---|---|
| `GC_CLOUD_RUN_SERVICE_NAME` | Cloud Run service name, e.g. `aobou-stripe-mcp-server` |
| `GC_REGION` | GCP region, e.g. `us-central1` |
| `STRIPE_SECRET_KEY` | Stripe secret key (`sk_...`) |
| `IDP_ISSUER` | PingOne issuer URL, e.g. `https://auth.pingone.com/<env-id>/as`. JWKS URL, env ID, and API base are all derived from this. |
| `IDP_REQUIRED_SCOPE` | Space-separated list of scopes the token must contain, e.g. `stripe_mcp:invoke` |
| `IDP_REQUIRED_AUDIENCE` | Expected `aud` claim value, e.g. `stripe-mcp-server`. If empty, audience is not checked. |
| `STEP_UP_SCOPE` | Scope required for high-value payments, e.g. `stripe_mcp:high_value`. If empty, step-up is not enforced. |
| `CLIENT_ID` | Client ID of the PingOne worker app |
| `CLIENT_SECRET` | Client secret — stored in Secret Manager as `mgmt-client-secret` |

## Deploy

```bash
make deploy
```

`deploy` runs `push` (builds and pushes the Docker image), then `gcloud run deploy $(GC_CLOUD_RUN_SERVICE_NAME)` with `--allow-unauthenticated`. `STRIPE_SECRET_KEY` and `CLIENT_SECRET` are stored in Secret Manager and mounted via `--set-secrets` — they are never passed as plain environment variables.

After deploying, register the Cloud Run service URL as an **MCP Server** in **Agent Platform → Govern → Agent Registry**.

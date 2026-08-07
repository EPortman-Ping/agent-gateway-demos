# Agent Gateway Extension Service

An Envoy `ext_proc` gRPC handler that the Agent Gateway calls on every request on the governed path. Deployed on Cloud Run, registered as a Service Extension.

For requests bound to the Stripe MCP tool it:
1. Validates the incoming delegated token carries both `sub` (user) and `act.client_id` (agent) claims
2. Performs a second RFC 8693 exchange to produce a tool-audienced token
3. Requests the body, then on `tools/call` calls PingOne Authorize with compound attributes (user sub + agent client_id + tool name + amount)
4. If the payment amount exceeds `STEP_UP_THRESHOLD_CENTS`, returns a `401 step_up_required` response — the Agent Bridge handles the MFA challenge

## Configure

### 1. PingOne Authorize - Trust Framework

In **Authorization → Trust Framework**, define the request attributes that PingOne Authorize will use to make a decision.

| Attribute | Type | Resolver Parameter |
|---|---|---|
| `user_sub` | String | `user_sub` |
| `agent_client_id` | String | `agent_client_id` |
| `tool_name` | String | `tool_name` |
| `amount_cents` | Number | `amount_cents` |
| `request_hour` | Number | `request_hour` |

### 2. PingOne Authorize - Policies

In **Authorization → Policies**, create a Policy Set named `OBO Agent Gateway Policies` with combining algorithm **DenyOverrides** (`Unless one decision is deny, the decision will be permit`). Add these 3 child policies:

**Policy 1: Agent-to-Tool Access Control** — combining: DenyOverrides
- Rule `Permit OBO Agent`
- Rule `Deny All other agents`

**Policy 2: Payment Amount Limit** — combining: DenyOverrides
- Rule `Deny Amounts Above Daily Limit`
- Rule `Permit Normal Amount`

**Policy 3: Business Hours Only** — combining: DenyOverrides
- Rule `Deny Outside Business Hours`
- Rule `Permit During Business Hours`

Note: step-up (above threshold) is handled before Authorize is called — see `STEP_UP_THRESHOLD_CENTS`.

### 3. PingOne Token Exchange - OIDC Web App

Create an **OIDC Web App application** in PingOne:
- **Name:** OBO Agent Gateway Extension
- **Grant Types:** enable both **Client Credentials** and **Token Exchange**
- Assign it the `OBO Google Cloud Agent Gateway` resource so it may request the `stripe_mcp:invoke` scope

### 4. Configure environment values

```bash
cp .env.sample .env
```

| Variable | Value |
|---|---|
| `GC_REGION` | Deploy region, e.g. `us-central1` |
| `GC_CLOUD_RUN_SERVICE_NAME` | `obo-agent-gateway-extension-service` |
| `IDP_TOKEN_ENDPOINT` | `https://auth.pingone.<region>/<env-id>/as/token` |
| `IDP_CLIENT_ID` | Token-exchange app Client ID |
| `IDP_CLIENT_SECRET` | Token-exchange app Client Secret |
| `IDP_SCOPE` | `stripe_mcp:invoke` |
| `TOOL_URL` | The Stripe MCP tool's Cloud Run base URL |
| `STEP_UP_THRESHOLD_CENTS` | Payment threshold that triggers step-up MFA, in cents (default `100000` = $1000) |
| `AUTHZ_DECISION_ENDPOINT` | PingOne Authorize decision endpoint URL |
| `AUTHZ_CLIENT_ID` | Authorize worker app Client ID |
| `AUTHZ_CLIENT_SECRET` | Authorize worker app Client Secret |

## Deploy

```bash
make deploy
```

`deploy` runs `setup` (creates the service account, stores `IDP_CLIENT_SECRET` and `AUTHZ_CLIENT_SECRET` in Secret Manager and grants the SA `secretAccessor`), then `push` (builds and pushes the Docker image), then `gcloud run deploy` with `--allow-unauthenticated --ingress=all --port 50051 --use-http2`.

# Agent Gateway Extension Service

Go [Envoy ext_proc](https://www.envoyproxy.io/docs/envoy/latest/configuration/http/http_filters/ext_proc_filter) gRPC server. Intercepts every MCP request routed through the Agent Gateway, validates the inbound PingOne user token minted by the Gemini Enterprise auth manager, resolves the caller's email, asks PingOne Authorize for a PERMIT/DENY decision, and on PERMIT performs an RFC 8693 token exchange before forwarding the request to the HR MCP server.

## Configure

### 1. Create the Extension Service app in PingOne

In **Connections → Applications**, create an **OIDC Web App**:

| Field | Value |
|---|---|
| **Name** | `GE Agent Gateway Extension Service` |
| **Grant types** | Client Credentials, Token Exchange |

On the **Resources** tab, assign the `GE HR MCP Server` resource so the app may request the `hr_mcp:read` scope. Note the Client ID and Client Secret — these become `IDP_CLIENT_ID` and `IDP_CLIENT_SECRET`.

### 2. Create the Authorize Worker app in PingOne

In **Connections → Applications**, create a **Worker** application:

| Field | Value |
|---|---|
| **Name** | `GE PingOne Authorize Worker` |
| **Grant type** | Client Credentials |

On the **Roles** tab, grant **Environment Admin** and **Identity Data Read Only** scoped to this environment. These roles are required for PingOne Authorize decisions and for resolving user email from `sub` via the management API. Note the Client ID and Client Secret — these become `AUTHZ_CLIENT_ID` and `AUTHZ_CLIENT_SECRET`.

### 3. PingOne Authorize — Trust Framework

In **Authorization → Trust Framework**, define the request attributes PingOne Authorize will use to make decisions:

| Attribute | Type | Resolver Parameter |
|---|---|---|
| `user_sub` | String | `user_sub` |
| `tool_name` | String | `tool_name` |
| `request_hour` | Number | `request_hour` |

### 4. PingOne Authorize — Policies

In **Authorization → Policies**, create a Policy Set named `GE Agent Gateway Policies` with combining algorithm **DenyOverrides**. Add these policies:

**Policy 1: User Identity Check** — combining: Unless one decision is permit, the decision will be deny
- Rule `Valid PingOne User` — condition: `user_sub` resolves to a user in this environment

**Policy 2: HR Read Access** — combining: Unless one decision is permit, the decision will be deny
- Rule `Permit hr_team` — condition: `tool_name` is one of `list_employees`, `get_employee` AND `user_sub` is a member of the `hr_team` group

**Policy 3: HR Admin Access** — combining: Unless one decision is permit, the decision will be deny
- Rule `Permit hr_admin` — condition: `tool_name` equals `create_employee` AND `user_sub` is a member of the `hr_admin` group

**Policy 4: Business Hours** — combining: Unless one decision is deny, the decision will be permit
- Rule `Deny Outside Business Hours` — condition: `request_hour` is less than `8` OR greater than `18`

### 5. PingOne Authorize — Publish and note decision endpoint

Go to **Authorization → Version History** and publish the latest version.

Note the decision endpoint URL from **Authorization → Decision Endpoints** — this becomes `AUTHZ_DECISION_ENDPOINT`.

### 6. Configure the Gemini Enterprise Auth Manager app in PingOne

In **Connections → Applications**, create a **Single Page Application** (Authorization Code + PKCE):

| Field | Value |
|---|---|
| **Name** | `GE Auth Manager` |
| **Grant type** | Authorization Code (PKCE) |
| **Redirect URI** | Gemini Enterprise auth manager callback URL |

On the **Resources** tab, assign `openid`, `profile`, `email`, and the `hr_mcp:read` scope from the `GE HR MCP Server` resource. Note the Client ID — this is used when configuring the auth manager in the Gemini Enterprise app console.

### 7. Configure environment values

```bash
cp .env.sample .env
```

| Variable | Value |
|---|---|
| `GC_CLOUD_RUN_SERVICE_NAME` | Cloud Run service name, e.g. `ge-agent-gateway-extension-service` |
| `GC_REGION` | GCP region, e.g. `us-central1` |
| `IDP_TOKEN_ENDPOINT` | `https://auth.pingone.com/<env-id>/as/token` |
| `IDP_CLIENT_ID` | Extension Service app Client ID from step 1 |
| `IDP_CLIENT_SECRET` | Extension Service app Client Secret from step 1 |
| `IDP_SCOPE` | `hr_mcp:read` |
| `IDP_REQUIRED_AUDIENCE` | `hr-mcp-server` — must match the PingOne resource audience |
| `TOOL_URL` | Cloud Run URL of the HR MCP server |
| `AUTHZ_DECISION_ENDPOINT` | PingOne Authorize decision endpoint URL from step 5 |
| `AUTHZ_CLIENT_ID` | Authorize Worker app Client ID from step 2 |
| `AUTHZ_CLIENT_SECRET` | Authorize Worker app Client Secret from step 2 |

## Deploy

```bash
make deploy
```

`make deploy` runs in order:

1. Creates a dedicated Cloud Run service account if it doesn't exist.
2. Stores `IDP_CLIENT_SECRET` and `AUTHZ_CLIENT_SECRET` in Secret Manager and grants the service account access.
3. Builds the Docker image (`GOOS=linux GOARCH=amd64`) and pushes to Artifact Registry.
4. Deploys to Cloud Run with `--use-http2 --ingress=all --allow-unauthenticated --port=50051` and mounts secrets via `--set-secrets`.

Note the Cloud Run service hostname after deploy — you need it when attaching the extension service to the Agent Gateway.

## What it does per request

| Phase | Action |
|---|---|
| Request headers | Extracts `Authorization: Bearer <token>`. Validates token signature (JWKS), `iss`, `aud`, `scope`. Resolves user email from `sub` via PingOne management API. Injects `X-User-Email` header. Performs RFC 8693 exchange → tool-scoped token. |
| Request body (`tools/call` only) | Parses JSON-RPC body. Calls PingOne Authorize with `user_sub`, `tool_name`, `request_hour`. On DENY: returns HTTP 403. On PERMIT: forwards with exchanged token. |
| Request body (`initialize`, `tools/list`) | Echoes body chunks without calling Authorize. |
| Response | Echoes response body chunks (required by `CONTENT_AUTHZ` policy profile). |

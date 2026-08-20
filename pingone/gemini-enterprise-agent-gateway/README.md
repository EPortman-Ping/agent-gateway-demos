# Gemini Enterprise + PingOne HR Agent

A Gemini Enterprise agent manages a live employee directory backed by the PingOne Users API — without storing any API credentials in the agent or its configuration.

Gemini Enterprise's built-in auth manager performs a 3-legged OAuth flow against PingOne and attaches the resulting user token to each MCP tool call. The HR MCP server validates the token before executing any tool, ensuring only authenticated PingOne users can invoke HR operations.

## Architecture

```
Gemini Enterprise UI (user message)
  │
  │  GE auth manager: 3LO against PingOne → user_token
  │  user_token: sub=<user>  aud=hr-mcp-server  scope=hr_mcp:read
  │  Bearer token attached to tools/call requests
  ▼
HR MCP Server (Cloud Run)
  │  initialize / tools/list  → no auth (GE sends no token during discovery)
  │  tools/call               → JWT validation (iss, aud, scope, sig via JWKS)
  │  Calls PingOne Users API with its own client_credentials token
  ▼
PingOne Users API
```

**Planned Phase 2:** Add the GCP Agent Gateway between GE and the HR MCP server. The gateway will call a PingOne Authorize policy before each tool call and perform an RFC 8693 token exchange to produce a short-lived tool-scoped token. The extension service code is already in this repo (`services/agent-gateway-extension-service/`).

## Prerequisites

### Google Cloud
- Google Cloud project with billing enabled
- Gemini Enterprise provisioned (Gemini Enterprise license + GCP project linked)
- `gcloud` CLI authenticated: `gcloud auth login && gcloud config set project <project-id>`
- Docker installed (for building Cloud Run images)
- Container Registry enabled: `gcloud services enable containerregistry.googleapis.com`

### PingOne Identity Provider
You need a PingOne environment with the following configured. All of this lives in the PingOne admin console.

**1. Resource: `GE HR MCP Server`**
- Audience: `hr-mcp-server`
- Custom scope: `hr_mcp:read`

**2. Single Page App: `Gemini Enterprise Auth Manager`**
- Grant types: Authorization Code + PKCE
- Redirect URI: the Gemini Enterprise auth manager callback URL (provided by GE when you configure the connector)
- Scopes: `openid`, `profile`, `email`, `hr_mcp:read`, `offline_access`
- Note the **Client ID** — this becomes `GE_AUTH_MANAGER_CLIENT_ID` in the GE connector config

**3. OIDC Web App: `Extension Service`** *(Phase 2 only)*
- Grant types: Client Credentials + Token Exchange
- Assigned scope: `hr_mcp:read`
- Note the **Client ID** and **Client Secret** — becomes `IDP_CLIENT_ID` / `IDP_CLIENT_SECRET`

**4. Worker App: `HR Management Worker`**
- Grant type: Client Credentials
- Role: `Identity Data Admin` (required for `create_employee`)
- Note the **Client ID** and **Client Secret** — becomes `PINGONE_MGMT_CLIENT_ID` / `PINGONE_MGMT_CLIENT_SECRET`

## Deployment

### Step 1 — HR MCP Server

```bash
cd services/hr-mcp-server
cp .env.sample .env
```

Fill in `.env`:

```
GC_CLOUD_RUN_SERVICE_NAME=ge-hr-mcp-server
GC_REGION=us-central1
IDP_ISSUER=https://auth.pingone.com/<env-id>/as
IDP_REQUIRED_SCOPE=hr_mcp:read
IDP_REQUIRED_AUDIENCE=hr-mcp-server
PINGONE_MGMT_CLIENT_ID=<hr-management-worker-client-id>
PINGONE_MGMT_CLIENT_SECRET=<hr-management-worker-client-secret>
```

Deploy:

```bash
make deploy
```

This will:
1. Enable required GCP APIs
2. Create a service account `hr-mcp-svc`
3. Store `PINGONE_MGMT_CLIENT_SECRET` in Secret Manager
4. Build and push the Docker image to Container Registry
5. Deploy to Cloud Run

Note the Cloud Run URL printed at the end — you need it for the GE connector configuration.

### Step 2 — Gemini Enterprise App and Connector

**2a. Create a Gemini Enterprise App**

In the GCP console, go to Gemini Enterprise → Apps → Create App. Note the **App URL** — this is what users will access (it will contain a `?csesidx=...` parameter when launched from the GCP console).

**2b. Add the HR MCP connector**

In the GE app → Data Stores → Add Data Store → Custom MCP Server. Configure:

| Field | Value |
|---|---|
| MCP Server URL | `https://<cloud-run-url>` (no `/mcp` suffix) |
| Auth Type | OAuth 2.0 |
| Authorization URL | `https://auth.pingone.com/<env-id>/as/authorize` |
| Token URL | `https://auth.pingone.com/<env-id>/as/token` |
| Client ID | `<GE_AUTH_MANAGER_CLIENT_ID>` (the SPA app from PingOne) |
| Client Secret | the SPA app's secret |
| Scopes | `openid profile email hr_mcp:read offline_access` |
| Enable PKCE | checked |

Click **Verify Auth** to confirm the credentials work, then save.

GE will call `tools/list` on your MCP server and populate `dynamicTools`. Confirm the three tools appear (`list_employees`, `get_employee`, `create_employee`).

**2c. Set the system instruction**

In the GE app → Configurations → Assistant, add:

```
You are an assistant with access to custom tools via MCP connectors. You have one connector available: PingOne HR MCP Server, which has list_employees, get_employee, and create_employee tools. When users ask about employees, use these tools to fetch real data. Always invoke these tools for employee-related requests.
```

### Step 3 — Per-user OAuth identity linking (required before first use)

This step is required for every user before they can invoke MCP tools. GE's `THIRD_PARTY_FEDERATED` connector type requires a per-user OAuth identity link stored in GE's BAP layer. Without it, the model will say the tool is not available even though the connector is active.

**This must be done from the GCP console URL** (the one with `?csesidx=...`). The plain `vertexaisearch.cloud.google.com` URL does not have full enterprise session context and won't show MCP connectors in the Sources panel.

1. Open the GE app from the GCP console (Gemini Enterprise → Apps → Open)
2. In the chat input, click **Sources**
3. Find the HR MCP connector row — it shows an **Authorize** button
4. Click **Authorize** — a PingOne login popup appears
5. Complete the PingOne login
6. The row switches to show an **Unauthorize** button and an enabled toggle — done

The HR MCP tools are now in the model's function declarations for this user.

## Verify

Send a message in the GE chat:

> "List all employees in the directory."

Expected: the model calls `list_employees` and returns real PingOne user data.

> "Get details for alice@example.com."

Expected: the model calls `get_employee` with the email.

Watch Cloud Run logs to confirm:

```bash
gcloud logging read 'resource.type="cloud_run_revision" AND resource.labels.service_name="ge-hr-mcp-server"' \
  --project=<project-id> --freshness=5m --format="table(timestamp,httpRequest.status,textPayload)"
```

You should see `POST /mcp HTTP/2 200` entries, and `[HRSvc] tool=list_employees` log lines.

## Security model

The HR MCP server validates the PingOne JWT on every `tools/call` request:
- **Signature**: verified against PingOne's JWKS endpoint
- **Issuer**: must match `IDP_ISSUER`
- **Audience**: must include `hr-mcp-server`
- **Scope**: must include `hr_mcp:read`

`tools/list` and `initialize` requests are unauthenticated — GE does not send a token during tool discovery, only at call time.

The management API calls (to actually read/write PingOne users) use a separate `client_credentials` token from the `HR Management Worker` app. This token never leaves the HR MCP server process.

## Phase 2: Agent Gateway

The `services/agent-gateway-extension-service/` and `services/agent-gateway/` directories contain the code and configuration to add PingOne Authorize policy enforcement between GE and the HR MCP server. When deployed:

- Members of PingOne group `hr_team` can call `list_employees` and `get_employee`
- Only members of `hr_admin` can call `create_employee`
- An RFC 8693 token exchange mints a short-lived tool-scoped token so the HR MCP server receives a token audienced to itself rather than the user's raw GE token

See `services/agent-gateway-extension-service/README.md` for deployment instructions.

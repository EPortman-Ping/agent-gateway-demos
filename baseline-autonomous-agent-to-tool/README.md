# Customer Journey 1: Baseline Autonomous Agent to Tool

A complete Go + GCP deployment of the Agent Gateway's core security flow. A CRM agent detects low inventory and calls an external supply chain **MCP tool** — it never sees or stores a credential, the real GCP Agent Gateway handles everything.

The agent is an MCP client and the supply chain tool is an MCP server (Streamable HTTP transport) exposing a `restock` tool. The agent opens its MCP session *through* the gateway, so token injection happens transparently underneath the MCP protocol.

## Architecture

```
CRM Agent (Cloud Run) ── MCP client
  │
  │  MCP over Streamable HTTP, carrying a GCP identity token (OIDC, from metadata server)
  ▼
GCP Agent Gateway  ──ext_proc (gRPC)──►  Extension Service (Cloud Run)
                                               │
                                               │  RFC 8693 Token Exchange
                                               ▼
                                          PingOne (3P IdP)
  │
  │  MCP request enriched with Authorization: Bearer <scoped token>
  ▼
Supply Chain Tool (Cloud Run) ── MCP server, `restock` tool
```

**Deny path:** if the agent's service account is not in the policy store, the extension service returns a `ProcessingResponse_ImmediateResponse` and the Agent Gateway returns `403` — the supply chain tool is never reached.

## Prerequisites

- GCP project with billing enabled
- `gcloud` CLI authenticated (`gcloud auth login`)
- Terraform >= 1.5
- Docker (for building images)
- A PingOne environment (its authorization server provides the RFC 8693-compatible token endpoint)

## What you deploy

| Service | Where | What it does |
|---|---|---|
| `services/agent` | Cloud Run | CRM agent — MCP client, calls the `restock` tool through the gateway, never touches credentials |
| `services/agent-gateway-extension-service` | Cloud Run (gRPC) | ext_proc handler — validates identity, calls IdP for token exchange |
| `services/supply-chain-mcp-tool` | Cloud Run | MCP server (`restock` tool) — validates the Bearer token injected by the gateway |
| `infra/` | Terraform | **Only** the Agent Gateway + traffic extension |

Each service is fully self-contained: `make deploy` from its directory (or `make deploy-<svc>` from the root) creates that service's own service account, reads its `.env`, and runs `gcloud run deploy` — so everything a service needs lives right next to its code. There is no shared bootstrap step. Terraform is scoped to just the Agent Gateway and the traffic extension; everything else is gcloud.

**You do not deploy or implement the Agent Gateway** — it's a real GCP product configured via Terraform.

## Setup & deploy

Steps are ordered by real dependencies: the extension service must be deployed before the gateway can point at it, and the gateway URL feeds the agent. Run each service's `make deploy` from its own directory.

### 1. MCP Server

In this demo the MCP tool is a mock restock tool that the agent calls. It exposes a single `restock` tool over MCP (Streamable HTTP) that returns a hardcoded "accepted" order — the point isn't the business logic, it's that the tool independently validates the OAuth token the gateway injects (signature, issuer, audience, and the `supply-chain:restock` scope) before running, so it never trusts a caller the gateway hasn't authorized.

#### 1.1. Deploy the MCP Server as a Cloud Run service

Copy the env template and fill it in — `make deploy` requires `GC_CLOUD_RUN_SERVICE_NAME`, `GC_REGION`, `IDP_ISSUER`, and `IDP_AUDIENCE`, and errors out if any are missing.

```bash
cd services/supply-chain-mcp-tool
cp .env.sample .env          # then edit: set IDP_ISSUER and IDP_AUDIENCE (the token this tool validates)
make deploy
cd ../..
```

`make deploy` builds the tool's container image, pushes it to GCR, creates the tool's own service account, and deploys it to Cloud Run with the config from `.env`. The dedicated service account gives the tool its own least-privilege identity — so Cloud Run doesn't fall back to the over-privileged default compute account. What actually closes the tool off is `--no-allow-unauthenticated` plus the invoker binding granted to the agent later (step 4).

#### 1.2. Create the resource that represents the MCP Server in PingOne

In PingOne, go to **Applications → Resources** and create the `supply-chain-mcp-tool` resource with the `supply-chain:restock` scope. Its audience must match the `IDP_AUDIENCE` you set in 1.1.

#### 1.3. Register the MCP Server in the Agent Registry

In the Cloud console, go to **Agent Platform → Govern → Agent Registry** and click **Add MCP Server**:

- **Name** — `BAATT Supply Chain MCP Tool`
- **Description** — e.g. "Restock MCP Tool - Baseline Autonomous Agent to Tool"
- **Region** — same region you deployed to
- **Tool specification** — paste the contents of [`services/supply-chain-mcp-tool/tool-spec.json`](services/supply-chain-mcp-tool/tool-spec.json)

### 2. Extension Service (token exchange)

#### 2.1. Configure token exchange in PingOne

The extension service performs an RFC 8693 token exchange: it presents the verified agent identity as the `subject_token` and gets back an access token scoped to `supply-chain:restock` and audienced to the tool. This adapts PingOne's [token exchange guide](https://docs.pingidentity.com/pingone/use_cases/p1_oauth_2_token_exchange_delegation.html) — our case has **no on-behalf-of user** (the agent is the subject), so there's no `act`/`may_act` actor configuration.

1. **Confirm the tool resource exists** — the `supply-chain-mcp-tool` resource with the `supply-chain:restock` scope from step 1.2. Its **Audience** is what the exchanged token's `aud` will be, and must match `IDP_AUDIENCE` in both services' `.env`.

2. **Create the Worker application** for the extension service. In **Applications → Applications → + Add**, choose **Worker**:
   - **Name** — `BAATT Extension Service`
   - **Description** — e.g. "Token exchange client (RFC 8693) — Baseline Autonomous Agent to Tool"

3. **Enable the grant types.** On the application's **Configuration** tab, edit and enable both **Client Credentials** and **Token Exchange** (`urn:ietf:params:oauth:grant-type:token-exchange`).

4. **Assign the tool resource.** On the **Resources** tab, add the `supply-chain-mcp-tool` resource so this client is allowed to request the `supply-chain:restock` scope.

5. **Copy the credentials** into `services/agent-gateway-extension-service/.env` (step 2.2):
   - **Client ID** → `IDP_CLIENT_ID`
   - **Client Secret** → `IDP_CLIENT_SECRET`
   - Environment token endpoint (`https://auth.pingone.<region>/<env-id>/as/token`) → `IDP_TOKEN_ENDPOINT`
   - The tool resource audience → `IDP_AUDIENCE`

#### 2.2. Deploy the extension service

```bash
cd services/agent-gateway-extension-service
cp .env.sample .env          # set IDP_TOKEN_ENDPOINT / IDP_CLIENT_ID / IDP_CLIENT_SECRET / IDP_AUDIENCE
make deploy                  # creates ext-svc SA, stores the IdP secret in Secret Manager, deploys
cd ../..
```

### 3. Agent Gateway

The traffic extension needs the extension service's deployed URL, so this runs after step 2.

```bash
make gateway EXT_SVC_URI=$(gcloud run services describe baatt-agent-gateway-extension-service \
    --region us-central1 --format 'value(status.url)')
```

Note the `agent_gateway_url` output — it goes into the agent's `.env` in step 4.

### 4. Agent

#### 4.1. Authorize the agent in the policy store

Add the CRM agent's service account to `allowedAgents` in `services/agent-gateway-extension-service/main.go`, then re-deploy the extension service so it takes effect:
```go
var allowedAgents = map[string][]string{
    "crm-agent@your-project.iam.gserviceaccount.com": {"supply-chain:restock"},
}
```
```bash
make deploy-agent-gateway-extension-service
```

#### 4.2. Deploy the agent and register it

Deploy the agent (creates its SA, deploys, and grants it invoke access on the tool):
```bash
cd services/agent
cp .env.sample .env          # set AGENT_GATEWAY_URL (gateway URL from step 3); GC_* default to the baatt- names
make deploy
cd ../..
```

Then register it in **Agent Platform → Registry → Add Agent**:

- **Type** — `A2A` (or `Non-A2A` for a plain HTTP agent)
- **Name** — `crm-agent`
- **Description** — e.g. "CRM restock agent (demo)"
- **Endpoint** — the agent's Cloud Run URL (`gcloud run services describe baatt-crm-agent --region us-central1 --format 'value(status.url)'`)

### 5. Run the demo

```bash
make trigger
```

This POSTs to the agent's `/trigger` endpoint. Watch Cloud Run logs in the GCP console to see each step light up in sequence:

**Extension Service logs:**
```
[ExtSvc] Authorize — agent: crm-agent@your-project.iam.gserviceaccount.com, path: /mcp
[ExtSvc] PERMIT — injecting OAuth token (scopes: supply-chain:restock)
```

**Supply Chain Tool logs:**
```
[SupplyChain] Received POST /mcp (X-Spiffe-Id: crm-agent@your-project.iam.gserviceaccount.com)
[SupplyChain] Bearer token received (first 40 chars): eyJhbGciOiJSUzI1NiIsInR5cCI6Ikp...
[SupplyChain] tools/call restock — 500 units of WIDGET-9000 for region us-west-2
```

## Tear down

`make destroy` removes only the Terraform-managed piece (the Agent Gateway + traffic extension):

```bash
make destroy
```

The Cloud Run services, service accounts, and secret were created with gcloud, so remove them the same way:

```bash
gcloud run services delete baatt-crm-agent baatt-agent-gateway-extension-service baatt-supply-chain-mcp-tool --region us-central1
gcloud secrets delete idp-client-secret
for sa in crm-agent ext-svc baatt-supply-chain-mcp-tool; do \
  gcloud iam service-accounts delete $sa@$(gcloud config get-value project).iam.gserviceaccount.com; done
```

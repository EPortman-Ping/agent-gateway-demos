# OBO Financial Agent

An ADK agent deployed to **Agent Runtime**. It is an MCP client: it connects to the Stripe MCP tool and calls financial operations on behalf of the user.

Unlike the baseline (which uses client credentials), this agent receives the user's delegated PingOne token via ADK session state — it never holds credentials. The `mcp_headers` function reads `ctx.state["delegated_token"]` and attaches it as the Authorization Bearer on every MCP request. Agent Runtime routes that egress through the Agent Gateway, where the extension service validates the delegated token before the request reaches the Stripe MCP tool.

## Configure

**1. No PingOne application needed** — the agent doesn't authenticate to PingOne directly. The Agent Bridge does the RFC 8693 exchange and stores the delegated token in session state before invoking the agent.

**2. Fill in `.env`:**

```bash
cp .env.sample .env
```

| Variable | Value |
|---|---|
| `GC_PROJECT_ID` | Target project ID |
| `GC_REGION` | Deploy region, e.g. `us-central1` |
| `AGENT_DISPLAY_NAME` | Display name for the Reasoning Engine, e.g. `obo-financial-agent` |
| `GC_AGENT_GATEWAY` | Full gateway path: `projects/<id>/locations/<region>/agentGateways/<name>` |
| `TOOL_MCP_URL` | The Stripe MCP tool's `/mcp` endpoint |

## Deploy

```bash
make deploy
```

`deploy.py` creates the Reasoning Engine with `identity_type = AGENT_IDENTITY`, binds it to the gateway, and grants `roles/iap.egressor` on the Agent Registry so the engine can reach all registered endpoints.

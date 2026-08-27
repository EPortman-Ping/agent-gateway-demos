# Support Agent

The user-facing agent in the Agent Chaining reference architecture. It delegates order-status requests to the Order Status Agent over A2A and does not call the order MCP server directly.

## Configure

```bash
cp .env.sample .env
```

| Variable | Purpose |
|---|---|
| `GC_PROJECT_ID` | Google Cloud project |
| `GC_REGION` | Agent Runtime region |
| `AGENT_DISPLAY_NAME` | Support Agent Reasoning Engine display name |
| `GC_AGENT_GATEWAY` | Shared Agent-to-Anywhere gateway resource |
| `A2A_ORDER_STATUS_AGENT_URL` | Order Status Agent A2A endpoint |
| `A2A_ORDER_STATUS_SCOPE` | Delegated A2A scope |
| `AGENT_GATEWAY_AUDIENCE` | Shared intermediate PingOne audience this agent's own exchange targets — the gateway extension performs the real exchange to `order-status-agent` on top of this one (see [the gateway extension's README](../agent-gateway-extension-service/README.md#pingone-resource-setup)) |
| `AGENT_IDP_TOKEN_ENDPOINT` | PingOne token endpoint |
| `AGENT_IDP_CLIENT_ID` | Support Agent exchange client |
| `AGENT_IDP_CLIENT_SECRET` | Support Agent exchange secret |

## Deploy

```bash
make deploy
```

`deploy.py` creates an Agent Runtime Reasoning Engine with `AGENT_IDENTITY` and binds it to the shared Agent Gateway. `teardown.py` removes engines matching `AGENT_DISPLAY_NAME`.

Local mode is explicitly development-only. Set `LOCAL_DELEGATION_MODE=false` and configure the PingOne values for a real RFC 8693 exchange.

# Order Status Agent

The specialized downstream agent in the Agent Chaining reference architecture. It receives `get_order_status` requests from the Support Agent over A2A and is the only agent allowed to call the Order Status MCP Server.

## Configure

```bash
cp .env.sample .env
```

| Variable | Purpose |
|---|---|
| `GC_PROJECT_ID` | Google Cloud project |
| `GC_REGION` | Agent Runtime region |
| `AGENT_DISPLAY_NAME` | Order Status Agent Reasoning Engine display name |
| `GC_AGENT_GATEWAY` | Shared Agent-to-Anywhere gateway resource |
| `A2A_ORDER_STATUS_AUDIENCE` | This agent's token audience |
| `A2A_ORDER_STATUS_SCOPE` | Scope accepted from Support Agent |
| `MCP_ORDER_STATUS_SERVER_URL` | Order Status MCP Server endpoint |
| `MCP_ORDER_STATUS_AUDIENCE` | MCP token audience |
| `MCP_ORDER_STATUS_SCOPE` | Scope requested for the MCP hop |
| `AGENT_IDP_TOKEN_ENDPOINT` | PingOne token endpoint |
| `AGENT_IDP_CLIENT_ID` | Order Status Agent exchange client |
| `AGENT_IDP_CLIENT_SECRET` | Order Status Agent exchange secret |

## Deploy

```bash
make deploy
```

The agent uses the same `agent.py`, `pingone.py`, `deploy.py`, `teardown.py`, and `Makefile` shape as the existing Agent Runtime demos. Its MCP token must be audience-bound to the Order Status MCP Server and scoped only to `order:read`.

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

## PingOne application grant (critical, easy to get backwards)

This agent doesn't own a PingOne resource of its own — it only needs `Client Credentials` + `Token Exchange` grants on its own application (client ID `2fe7b82c-8739-420c-8790-401d4a6c2065`). But **which resource's copy of `order-status:invoke` that application is granted matters more than it looks**: it must be granted the scope **from `ac-google-cloud-agent-gateway`**, not from `order-status-agent` — even though `order-status-agent` is the resource this agent's `.env` (`A2A_ORDER_STATUS_SCOPE`) and its own exchange call ultimately talk to. `order-status:invoke` exists as two separate scope objects with the same name, one per resource; check under **Applications → AC Support Agent → Resources** in the console directly.

Getting this backwards doesn't produce an error anywhere obvious: this agent's own `client_credentials` actor-token fetch still "succeeds" (just audienced to whichever resource the grant actually points at), and its `pingone.py::get_delegated_token` exchange call — which explicitly sets `audience=ac-google-cloud-agent-gateway` — silently resolves to the wrong resource instead of honoring the requested `audience`. The failure only surfaces one hop later, at the gateway extension, as `act is configured as required for the Access token but does not have a value`. See [CLAUDE.md](../../CLAUDE.md#pingone-client-scope-requirements-critical-non-obvious) for the full explanation.

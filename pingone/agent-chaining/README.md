# Agent Chaining: Customer Order Status Chain

**Customer Journey 3:** a concise two-agent reference architecture demonstrating how an authenticated user's identity is carried through an agent chain to an MCP server.

```text
Browser / Chat UI
  │ PingOne PKCE → user_token
  ▼
Agent Bridge
  │ validates user_token; stores it in Support Agent session
  ▼
Support Agent (Agent Runtime)
  │ RFC 8693: user_token + Support Agent actor
  │ aud=order-status-agent, scope=order-status:invoke
  ▼
Agent Gateway / A2A Adapter
  ▼
Order Status Agent (Agent Runtime)
  │ RFC 8693: delegated token + Order Status Agent actor
  │ aud=order-status-mcp-server, scope=order:read
  ▼
Agent Gateway / MCP
  ▼
Order Status MCP Server
  │ validates final token; returns order status
```

MCP is for agent-to-tool calls. A2A is for agent-to-agent calls. The user's identity remains the token `sub` through both RFC 8693 exchanges while the `act` chain records which agent performed each delegation.

## Why this example

A support agent can answer an order question, but it never receives direct access to the order backend. It delegates to the specialized Order Status Agent, which alone owns the MCP capability. This is a small, useful pattern that can be extended with more agents or tools.

## Services

- `services/chat-ui/` — React/Vite PingOne PKCE browser.
- `services/agent-bridge/` — FastAPI Cloud Run user/session boundary.
- `services/support-agent/` — Agent Runtime Support Agent (`agent.py`, `pingone.py`, `deploy.py`, `teardown.py`).
- `services/order-status-a2a-adapter/` — authenticated A2A ingress to the Order Status Agent.
- `services/order-status-agent/` — Agent Runtime Order Status Agent with the only MCP capability.
- `services/order-status-mcp-server/` — deployable protected MCP server with deterministic mock data.

## Deployment

Deploy the services in this order. Each service has its own README with the configuration and command for that service.

### 1. Order Status MCP Server

Follow [Order Status MCP Server](services/order-status-mcp-server/README.md) to build and deploy the protected Go MCP server to Cloud Run. Record the resulting `/mcp` URL; it becomes `MCP_ORDER_STATUS_SERVER_URL` for the Order Status Agent.

### 2. Agent Gateway Extension Service

Follow the Agent Gateway extension-service deployment instructions to deploy the ext_proc service to Cloud Run. Configure it for the Order Status MCP Server and the Order Status A2A Adapter. The extension service validates delegated token audience/scope, permits requests in explicit plumbing mode, and performs the final RFC 8693 downscoping. Actor checks and real PingOne Authorize policy are added after the path is working.

> This service is not yet present in the `agent-chaining` directory. The existing implementation to adapt is [agent-gateway-extension-service](../agent-on-behalf-of-user/services/agent-gateway-extension-service/README.md). Do not deploy that existing service unchanged; its current policy attributes and target matching are specific to the on-behalf-of-user Stripe journey.

### 3. Agent Gateway — create and attach

Create the Google-managed Agent Gateway and attach the extension service (`make attach` in `services/agent-gateway/`). This only depends on the extension service's Cloud Run URL from step 2 — the authorization policy matches by path prefix (`/a2a`, `/mcp`), not by backend host, so no destination needs to exist yet.

This has to happen **before** either Agent Runtime engine deploys, because each engine's `deploy.py` binds its egress to `GC_AGENT_GATEWAY` by resource name.

Register the Order Status MCP Server (from step 1) as an egress destination now, since its URL is already known.

### 4. Order Status Agent

Follow [Order Status Agent](services/order-status-agent/README.md) to create its PingOne application and deploy the Agent Runtime Reasoning Engine. Configure:

- `MCP_ORDER_STATUS_SERVER_URL` from step 1
- `GC_AGENT_GATEWAY` from step 3
- The Order Status Agent RFC 8693 client and `order:read` scope

Record the resulting Reasoning Engine resource name for the A2A Adapter.

### 5. Order Status A2A Adapter

Follow [Order Status A2A Adapter](services/order-status-a2a-adapter/README.md) to deploy the Cloud Run `/a2a` endpoint. Configure it with the Order Status Agent engine resource from step 4. Record the adapter URL.

### 6. Agent Gateway — register the adapter destination

Return to `services/agent-gateway/` and register the Order Status A2A Adapter URL from step 5 as the second egress destination. The gateway resource and extension attachment from step 3 stay unchanged; this only adds an Agent Registry endpoint entry.

### 7. Support Agent

Follow [Support Agent](services/support-agent/README.md) to create its PingOne application and deploy the second Agent Runtime Reasoning Engine. Configure:

- `A2A_ORDER_STATUS_AGENT_URL` with the adapter URL from step 5
- `GC_AGENT_GATEWAY` with the same gateway resource from step 3
- The Support Agent RFC 8693 client and `order-status:invoke` scope

### 8. Agent Bridge

Follow [Agent Bridge](services/agent-bridge/README.md) to deploy the Cloud Run user/session boundary. Configure `AGENT_ENGINE_NAME` with the Support Agent engine resource from step 7 and `CORS_ORIGIN` with the future Chat UI URL.

### 9. Chat UI

Follow [Chat UI](services/chat-ui/README.md) to configure the PingOne PKCE application and deploy the browser application. Set `VITE_AGENT_BRIDGE_URL` to the Agent Bridge URL from step 8.

Both Agent Runtime engines must use the same Agent-to-Anywhere gateway in the same project and region. No Gemini Enterprise resources are required.

## Local/development mode

Local token helpers may produce clearly marked `local-rfc8693.*` claims-shaped tokens. They are development substitutes only. Production uses real PingOne JWT validation and RFC 8693 subject-plus-actor exchanges.

Do not implement a custom `x-id-jag` token for this reference architecture.

## Verify identity propagation

Ask the browser:

> What is the status of order ORD-123?

Verify the logs and token claims at each boundary:

```text
Browser user token:       sub=user
Support → Order Status:   sub=user, act.sub=support-agent,
                          aud=order-status-agent,
                          scope=order-status:invoke
Order Status → MCP:       sub=user, act.sub=order-status-agent,
                          aud=order-status-mcp-server,
                          scope=order:read
```

The MCP server should return `shipped` for `ORD-123` and reject tokens with the wrong audience, scope, or actor.

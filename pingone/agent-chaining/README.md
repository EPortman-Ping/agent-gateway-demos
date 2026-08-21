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

## Deployment order

1. Deploy the Order Status MCP Server and record its Cloud Run URL.
2. Configure the Agent Gateway/ext_proc policy for the A2A adapter and MCP server.
3. Deploy the Order Status Agent with the MCP URL and shared gateway.
4. Deploy the Order Status A2A Adapter with the Order Status Agent engine resource.
5. Deploy the Support Agent with the A2A adapter URL and shared gateway.
6. Deploy the Agent Bridge with the Support Agent engine resource.
7. Deploy the Chat UI with the bridge URL and PingOne PKCE settings.

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

# Agent Chaining

**Customer Journey 3:** a support agent answers a customer's order-status question by delegating to a specialized Order Status Agent over native [A2A](https://a2aprotocol.ai/) — but it never holds a credential for that agent or the [MCP](https://modelcontextprotocol.io/docs/2026-07-28/getting-started/intro) server behind it, and the customer's identity is carried through both hops.

The user logs in via PKCE; their token is exchanged via [RFC 8693 token exchange](https://docs.pingidentity.com/pingone/use_cases/p1_oauth_2_token_exchange_delegation.html) at every hop — first by Support Agent, then again by the [GCP Agent Gateway](https://docs.cloud.google.com/gemini-enterprise-agent-platform/govern/gateways/agent-gateway-overview)'s extension service via [Envoy ext_proc](https://www.envoyproxy.io/docs/envoy/latest/configuration/http/http_filters/ext_proc_filter), which also injects a separate Google-minted credential on this hop — Order Status Agent's native A2A endpoint is itself a Google-hosted Agent Runtime surface with its own independent IAM check, on top of the PingOne delegation. Order Status Agent repeats the same pattern one hop further, delegating to the Order Status MCP Server. Four RFC 8693 exchanges carry one identity end to end.

## Architecture

```text
Browser / Chat UI (PingOne PKCE)
  │  Requests `order-status:invoke` scope directly at login.
  ▼
Agent Bridge
  │  Validates the browser token (signature + issuer + sub — not audience).
  │  Stores it in Support Agent's ADK session state.
  ▼
Support Agent (Agent Runtime, ADK)
  │  RFC 8693 exchange #1: swaps the actor to Support Agent, re-audiences to
  │  the shared intermediate resource ac-google-cloud-agent-gateway (same scope).
  │  Sends it in the native A2A request body's `metadata` field.
  ▼
Agent Gateway — native A2A (egress via the mTLS host)
  │  RFC 8693 exchange #2: extension remints on top of #1, re-audiences to
  │  the real final resource order-status-agent (same scope).
  │  Authorization header carries a separate Google credential for this hop.
  ▼
Order Status Agent (Agent Runtime, native A2A executor)
  │  Validates the reminted token from the request body.
  │  RFC 8693 exchange #3: swaps the actor to itself, re-audiences back to
  │  the shared intermediate resource, downscoped to order:read.
  ▼
Agent Gateway — MCP
  │  RFC 8693 exchange #4: extension remints on top of #3, re-audiences to
  │  the real final resource order-status-mcp-server (same scope).
  ▼
Order Status MCP Server
  │  Validates the final token; returns order status.
```

Support Agent's and Order Status Agent's own exchanges (#1 and #3) both target the same shared intermediate resource, `ac-google-cloud-agent-gateway` — never the real final resource directly. Only the gateway extension's exchanges (#2 and #4) ever touch the real final resources (`order-status-agent`, `order-status-mcp-server`). See [CLAUDE.md](CLAUDE.md#token-exchange--remint-pattern--4-rfc-8693-exchanges-per-request) for why.

Following the diagram: the user authenticates through the Chat UI and sends a message to the Agent Bridge, which validates the token and stores it in Support Agent's session state. Support Agent performs its own RFC 8693 exchange and calls Order Status Agent over native A2A; Agent Gateway intercepts that call, remints the token again with its own actor, and separately injects a Google credential the call needs to reach a Google-hosted Agent Runtime endpoint. Order Status Agent validates the reminted token, then performs its own RFC 8693 exchange to call the Order Status MCP Server; Agent Gateway intercepts that call too and remints once more before the MCP server validates the final token and returns the order.

## Token Chain

Every hop gets a **freshly reminted** token, not just a validated passthrough — Support Agent and Order Status Agent each exchange for the shared intermediate resource, and the gateway extension exchanges *again* on top of that with its own actor, to the real final resource, before forwarding. The block below is a real, decoded token chain captured end to end from a live run (`ORD-123`, browser through to the MCP server) — client IDs are real (they're not secrets; see the table in [CLAUDE.md](CLAUDE.md#pingone-setup--configuring-the-act-claim-delegation-proof)), `sub` is genericized since it's a real user identifier, and `iat`/`exp`/`jti`/`sid` are omitted since they're just noise for this purpose:

**0. Raw browser token** — issued at PKCE login. The Chat UI's SPA client requests `order-status:invoke` scope directly, so PingOne has already derived `aud=order-status-agent` from that scope before any exchange happens. `may_act` names Support Agent as the only actor this token may be exchanged with next — that's what makes hop 1's `act` claim below possible:
```json
{
  "sub": "<user-id>",
  "client_id": "0800e2cb-cfb8-46bb-8a5a-0f2c6ced2b0a",
  "aud": ["order-status-agent"],
  "scope": "order-status:invoke openid profile email",
  "may_act": {"sub": "2fe7b82c-8739-420c-8790-401d4a6c2065"}
}
```

**1. Support Agent's exchange** (hop 1 of 4) — minted by Support Agent via RFC 8693, sent in the outbound native A2A request body. Targets the **shared intermediate resource** `ac-google-cloud-agent-gateway`, not `order-status-agent` directly:
- Still represents the user (`sub`)
- `act.sub` names Support Agent as the party that performed this exchange — proven by PingOne itself, not just asserted by the client
- `may_act.sub` licenses the gateway extension as the only actor allowed to exchange this token further, for hop 2
```json
{
  "sub": "<user-id>",
  "client_id": "2fe7b82c-8739-420c-8790-401d4a6c2065",
  "aud": ["ac-google-cloud-agent-gateway"],
  "scope": "order-status:invoke",
  "act": {"sub": "2fe7b82c-8739-420c-8790-401d4a6c2065"},
  "may_act": {"sub": "fbd9fb33-9134-4dc3-b2b1-3411bb8cf336"}
}
```

**2. Gateway remint, A2A hop** (hop 2 of 4) — minted by the extension service via RFC 8693 on top of token 1, re-audienced to the **real final resource** `order-status-agent`. This is the token Order Status Agent actually validates:
- Still represents the user (`sub`)
- `act.sub` names the extension — matches token 1's `may_act`, which is why this exchange was permitted
- `may_act.sub` licenses Order Status Agent as the only actor allowed to exchange this token further, for hop 3
- Carried in the A2A request **body** (`message.metadata.delegatedAuthorization`), not a header — `Authorization` on this hop instead carries a Google-minted credential, since the target is a Google-hosted `aiplatform.googleapis.com` surface with its own IAM check, independent of PingOne delegation
```json
{
  "sub": "<user-id>",
  "client_id": "fbd9fb33-9134-4dc3-b2b1-3411bb8cf336",
  "aud": ["order-status-agent"],
  "scope": "order-status:invoke",
  "act": {"sub": "fbd9fb33-9134-4dc3-b2b1-3411bb8cf336"},
  "may_act": {"sub": "6f716c87-e33d-4981-b632-6dd357f03f14"}
}
```

**3. Order Status Agent's exchange** (hop 3 of 4) — minted by Order Status Agent via RFC 8693 after validating token 2, attached to the outbound MCP request. Targets the **shared intermediate resource** again, downscoped to `order:read`:
- Still represents the user (`sub`)
- `act.sub` names Order Status Agent — matches token 2's `may_act`
- `may_act.sub` licenses the gateway extension again, for hop 4
```json
{
  "sub": "<user-id>",
  "client_id": "6f716c87-e33d-4981-b632-6dd357f03f14",
  "aud": ["ac-google-cloud-agent-gateway"],
  "scope": "order:read",
  "act": {"sub": "6f716c87-e33d-4981-b632-6dd357f03f14"},
  "may_act": {"sub": "fbd9fb33-9134-4dc3-b2b1-3411bb8cf336"}
}
```

**4. Gateway remint, MCP hop** (hop 4 of 4) — minted by the extension service via RFC 8693 on top of token 3, re-audienced to the **real final resource** `order-status-mcp-server`. This is the token the MCP server actually validates, and the chain ends here — no `may_act`, since nothing is ever exchanged further:
- Still represents the user (`sub`)
- `act.sub` names the extension — matches token 3's `may_act`
```json
{
  "sub": "<user-id>",
  "client_id": "fbd9fb33-9134-4dc3-b2b1-3411bb8cf336",
  "aud": ["order-status-mcp-server"],
  "scope": "order:read",
  "act": {"sub": "fbd9fb33-9134-4dc3-b2b1-3411bb8cf336"}
}
```

**On the `act`/`may_act` claims:** this PingOne environment does populate them, once the resources involved (`ac-google-cloud-agent-gateway`, `order-status-agent`, `order-status-mcp-server`) each have the right attribute mappings — see [CLAUDE.md](CLAUDE.md#pingone-setup--configuring-the-act-claim-delegation-proof) for the exact expressions and the debugging path that found the gaps. Every `act` claim above is checked by PingOne itself at exchange time (required attributes fail the exchange closed rather than minting an unproven delegation), not just asserted by whichever service is calling — that's what makes this a stronger attestation than `client_id` alone.

## Components

| Component | Role |
|---|---|
| [**Chat UI**](services/chat-ui) | React/Vite SPA, PingOne PKCE login |
| [**Agent Bridge**](services/agent-bridge) | Google Cloud entry point; validates the user token, stores it in Support Agent's session state |
| [**Support Agent**](services/support-agent) | ADK agent on Agent Runtime; reads the user token, exchanges it, calls Order Status Agent over native A2A |
| [**Order Status Agent**](services/order-status-agent) | Native A2A agent on Agent Runtime; validates the inbound delegation, exchanges it again, calls the MCP server |
| [**Order Status MCP Server**](services/order-status-mcp-server) | MCP server (`get_order_status`); validates the final delegated token |
| [**Agent Gateway Extension Service**](services/agent-gateway-extension-service) | ext_proc handler; validates each hop's token, remints a fresh delegation, injects a Google credential for the A2A hop |
| [**Agent Gateway**](services/agent-gateway) | Google-managed policy enforcement point; governs both the A2A and MCP hops |
| **PingOne Authorize** | External policy decision point — wired in code but not yet enforced (extension runs `AUTHZ_MODE=permit-all`; see [CLAUDE.md](CLAUDE.md)) |
| **PingOne** | Identity Provider |

## Prerequisites

- Google Cloud project with billing enabled
- `gcloud` CLI authenticated against the target project
- Docker (to build service images)
- A PingOne environment (PingOne Authorize is optional while `AUTHZ_MODE=permit-all`)

## Deployment

Deploy in this order — later steps need URLs/engine IDs from earlier ones. See [CLAUDE.md](CLAUDE.md) for the exact config keys and non-obvious gotchas (mTLS host, project-ID-string paths, engine-ID churn) not repeated here.

### 1. Order Status MCP Server
Follow [order-status-mcp-server](services/order-status-mcp-server/README.md) to build and deploy the Go MCP server to Cloud Run.

### 2. Agent Gateway Extension Service
Follow [agent-gateway-extension-service](services/agent-gateway-extension-service/README.md) to deploy the ext_proc service to Cloud Run. The native A2A target URL can be a placeholder until step 4.

### 3. Agent Gateway
Create the gateway and attach the extension service and authz policy from `services/agent-gateway/` (`make attach`) — this must happen **before** either Agent Runtime engine deploys, since each engine's `deploy.py` binds its egress to `GC_AGENT_GATEWAY` by resource name.

### 4. Order Status Agent
Follow [order-status-agent](services/order-status-agent/README.md) to deploy the native A2A Reasoning Engine. Update the extension's `A2A_TARGET_URL` with the resulting engine ID and redeploy the extension.

### 5. Support Agent
Follow [support-agent](services/support-agent/README.md) to deploy the second Reasoning Engine, bound to the same gateway. Needs Order Status Agent's A2A URL from step 4.

### 6. Agent Bridge
Follow [agent-bridge](services/agent-bridge/README.md) to deploy the Cloud Run session boundary. Needs Support Agent's engine name from step 5.

### 7. Chat UI
Follow [chat-ui](services/chat-ui/README.md) to deploy the browser app. Needs the Agent Bridge URL from step 6 and a PingOne PKCE client requesting `order-status:invoke`.

Both Agent Runtime engines must use the same Agent-to-Anywhere gateway in the same project and region.

## Verify

Open the Chat UI, sign in, and ask:

> What is the status of order ORD-123?

Watch the logs for both the extension service and Order Status Agent:

```text
# Extension service (A2A hop)
[ExtSvc] onHeaders authority="us-central1-aiplatform.mtls.googleapis.com" path=".../reasoningEngines/<id>/a2a/message:send" matched=true
[ExtSvc] delegated token minted target=A2A ttl=59m30s
[ExtSvc] target=A2A protocol=a2a subject=<sub> actor=delegated-agent (google-auth+remint)
[ExtSvc] PERMIT target=A2A action=get_order_status order=ORD-123

# Extension service (MCP hop)
[ExtSvc] onHeaders authority="<mcp-server-host>" path="/mcp" matched=true
[ExtSvc] delegated token minted target=MCP ttl=59m30s
[ExtSvc] target=MCP protocol=mcp subject=<sub> actor=delegated-agent
[ExtSvc] PERMIT target=MCP action=get_order_status order=ORD-123
```

The agent should reply with the order's status (`ORD-123` → shipped, `ORD-456` → processing) and reject requests with the wrong audience, scope, or a malformed/expired token at any hop.

## When to use this pattern

Use this pattern when one agent needs to delegate part of a task to a second, specialized agent that owns a capability the first agent shouldn't access directly — and the human user's identity needs to survive both the agent-to-agent hop and the agent-to-tool hop behind it. This gives you:

- **No standing inter-agent credentials.** Neither Support Agent nor Order Status Agent holds a token that works past its own next hop — each carries only what the previous exchange gave it, and the gateway remints a fresh one before forwarding.
- **Two independent trust layers cross the same wire.** The A2A hop needs both a Google credential (to reach a Google-hosted Agent Runtime endpoint at all) and a PingOne delegated token (for the receiving agent to know who's asking) — carried side by side in the same request, in different parts of it.
- **Least privilege compounds across hops.** Each of the four exchanges narrows scope to exactly the next hop's need; a token minted for Order Status Agent is worthless at the MCP server, and vice versa.

This pattern builds on the [agent on-behalf-of-user](../agent-on-behalf-of-user) demo, extending the delegation model from a single agent-to-tool hop to a full agent-to-agent-to-tool chain, and layering a second, independent Google-credential requirement on top of PingOne delegation for the agent-to-agent hop.

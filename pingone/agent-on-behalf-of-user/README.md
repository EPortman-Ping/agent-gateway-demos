# Agent On-Behalf-Of User

A financial agent purchases [Stripe](https://stripe.com) products on behalf of an authenticated human user — but it never holds a credential for the Stripe [MCP](https://modelcontextprotocol.io/docs/2026-07-28/getting-started/intro) server, and the user's identity is never lost along the way.

The user logs in via PKCE and their token is carried through every hop via [RFC 8693 token exchange](https://docs.pingidentity.com/pingone/use_cases/p1_oauth_2_token_exchange_delegation.html): the agent exchanges it for a delegated token, which the [GCP Agent Gateway](https://docs.cloud.google.com/gemini-enterprise-agent-platform/govern/gateways/agent-gateway-overview) intercepts and hands to an extension service via [Envoy ext_proc](https://www.envoyproxy.io/docs/envoy/latest/configuration/http/http_filters/ext_proc_filter). That service validates the token, resolves the user's email, then asks [PingOne Authorize](https://www.pingidentity.com/en/product/pingone-authorize.html) whether this agent is permitted to act for this user on this transaction. On PERMIT, it exchanges the token again — producing one scoped to the Stripe MCP server — and injects it before forwarding the request.

## Architecture

![Agent on Behalf of User reference architecture](../../_docs/agent-on-behalf-of-user/architecture.svg)

The user authenticates via the Chat UI (PingOne PKCE) and sends a message to the Agent Bridge. The bridge validates the user's token and stores it raw in the ADK session state, then invokes Agent Runtime. The agent reads the user token from session state and performs an RFC 8693 token exchange to produce a delegated token, which it attaches to each outbound MCP request. Agent Runtime routes those requests through the Agent Gateway, which calls the extension service. The extension service validates the delegated token, calls PingOne Authorize with the request context, and on PingOne Authorize approval performs a second RFC 8693 token exchange to produce a tool-scoped token before the request reaches the target MCP server.

## Token Chain

The user's identity is carried through every hop as a chain of RFC 8693 delegation exchanges. Each hop adds an `act` (actor) claim identifying the delegating party, so the final token at the MCP server shows the full chain: user → agent → extension service.

**Subject token** — user's PKCE token, stored in ADK session state by the agent bridge:
```json
{
  "iss": "https://auth.pingone.com/<env-id>/as",
  "sub": "<alice-user-id>",
  "aud": "<chat-ui-resource-id>",
  "scope": "openid profile email stripe_mcp:invoke"
}
```

**Exchanged token 1 (delegated token)** — minted by the agent via RFC 8693, attached to every outbound MCP request:
```json
{
  "iss": "https://auth.pingone.com/<env-id>/as",
  "sub": "<alice-user-id>",
  "aud": "stripe-mcp-server",
  "scope": "stripe_mcp:invoke",
  "act": {
    "sub": "<agent-client-id>"
  }
}
```

**Exchanged token 2 (tool token)** — minted by the extension service via RFC 8693, injected before forwarding to the Stripe MCP server:
```json
{
  "iss": "https://auth.pingone.com/<env-id>/as",
  "sub": "<alice-user-id>",
  "aud": "stripe-mcp-server",
  "scope": "stripe_mcp:invoke",
  "act": {
    "sub": "<ext-svc-client-id>",
    "act": {
      "sub": "<agent-client-id>"
    }
  }
}
```

## Components

| Component | Role |
|---|---|
| [**Chat UI**](services/chat-ui) | React/Vite SPA, PingOne PKCE login |
| [**Agent Bridge**](services/agent-bridge) | Google Cloud entry point; validates user token, stores it in ADK session state |
| [**Agent**](services/agent) | Financial agent acting as MCP client; reads user token and performs token exchange |
| [**Agent Gateway Extension Service**](services/agent-gateway-extension-service) | ext_proc handler, forwards request to PingOne Authorize, exchanges & injects IdP token |
| [**Stripe MCP Server**](services/stripe-mcp-server) | MCP server; validates injected token and calls Stripe API |
| [**Agent Gateway**](services/agent-gateway) | Google-managed policy enforcement point |
| **PingOne Authorize** | External policy decision point |
| **PingOne** | Identity Provider |

## Prerequisites

- Google Cloud project with billing enabled
- `gcloud` CLI authenticated against the target project
- Docker (to build service images)
- A PingOne environment with PingOne Authorize
- A Stripe account with products and customers configured

## Deployment

### 1. Stripe MCP Server
Follow the instructions in [stripe-mcp-server](services/stripe-mcp-server/README.md) to deploy this service to Cloud Run and register it in Agent Registry.

### 2. Agent Gateway Extension Service
Follow the instructions in [agent-gateway-extension-service](services/agent-gateway-extension-service/README.md) to deploy this service to Cloud Run.

### 3. Agent Gateway
Follow the instructions in [agent-gateway](services/agent-gateway/README.md) to create the gateway, attach the extension service, and register the egress destinations.

### 4. Agent
Follow the instructions in [agent](services/agent/README.md) to create the agent's PingOne app, deploy it to Agent Runtime, register it, and grant it egress.

### 5. Agent Bridge
Follow the instructions in [agent-bridge](services/agent-bridge/README.md) to deploy this service to Cloud Run.

### 6. Chat UI
Follow the instructions in [chat-ui](services/chat-ui/README.md) to deploy this service to Cloud Run.

## Verify

Open the Chat UI URL, sign in with a PingOne user who has a Stripe customer record, and ask the agent to make a purchase:

Watch the logs in Cloud Run for both the extension service and Stripe MCP server:

```
# Extension service (each MCP request: initialize, tools/list, tools/call)
[ExtSvc] aobou-stripe-mcp-server /mcp — user=<sub> agent=<client_id> email=alice@example.com

# Extension service (tools/call only — Authorize fires on body phase)
[ExtSvc] authorize user=<sub> agent=<client_id> tool=create_stripe_payment_intent amount_cents=2900 hour=14
[ExtSvc] PingOne Authorize PERMIT user=<sub> agent=<client_id>

# Stripe MCP server (tools/call only)
tool=create_stripe_payment_intent — success: caller=alice@example.com product_id=prod_... quantity=1
```

## When to use this pattern

Use this pattern when an agent must act on behalf of a specific human user and the tool needs to know who that user is. The user proves their identity at login; their identity is carried through every hop so the gateway can verify both who the agent is and who it is acting for before any tool call executes. This gives you:

- **No user credentials in the agent.** The agent never holds a token that grants the user's full authority — only a delegated token scoped to one tool, valid for one request.
- **Compound authorization.** PingOne Authorize evaluates the agent's identity and the user's identity together. A rogue agent cannot use a stolen user token, and a user cannot invoke a tool through an unauthorized agent.
- **User identity at the tool.** The MCP server receives the user's email, not just an agent token — enabling per-user Stripe lookups, receipts, and audit records.

This pattern builds on the [baseline autonomous agent-to-tool](../baseline-autonomous-agent-to-tool) demo, extending the delegation model to carry a human user's identity through the exchange.

# Agent On-Behalf-Of User

A financial agent purchases Stripe products on behalf of an authenticated human user — but the agent never holds the user's full authority. The user logs in via PKCE, and their identity is carried through every hop via RFC 8693 token delegation. The Agent Gateway enforces a compound authorization check that proves both the agent is trusted and the user has granted explicit permission. For high-value transactions it triggers a PingOne MFA push before allowing the request through.

## Architecture

The user authenticates via the Chat UI (PingOne PKCE) and sends a message to the Agent Bridge. The bridge validates the user's token and stores it raw in the ADK session state, then invokes Agent Runtime. The agent reads the user token from session state, obtains its own PingOne actor token via `client_credentials`, and performs an RFC 8693 exchange to produce a delegated token (`sub=user, act.client_id=agent`), which it attaches to each outbound MCP request. Agent Runtime routes those requests through the Agent Gateway, which calls the extension service. The extension service validates the delegation chain (`sub` + `act.client_id`), checks the payment amount against the step-up threshold, calls PingOne Authorize with compound attributes (user sub, agent client_id, tool name, amount), and performs a second RFC 8693 exchange to produce a tool-scoped token before the request reaches Stripe.

## Components

| Component | Role |
|---|---|
| [**Chat UI**](services/chat-ui) | React/Vite SPA, PingOne PKCE login, step-up MFA modal |
| [**Agent Bridge**](services/agent-bridge) | FastAPI Cloud Run service; validates user token, stores it in ADK session state, handles MFA step-up |
| [**Agent**](services/agent) | ADK agent on Agent Runtime; `pingone.py` reads `user_token` from session state and does RFC 8693 exchange at call time |
| [**Agent Gateway Extension Service**](services/agent-gateway-extension-service) | ext_proc handler; validates delegation chain, compound Authorize, step-up detection |
| [**Stripe MCP Server**](services/stripe-mcp-server) | MCP server; validates iss/aud/scope, resolves user email via PingOne management API, enforces step-up scope on high-value calls |
| [**Agent Gateway**](services/agent-gateway) | Google-managed policy enforcement point |
| **PingOne Authorize** | External policy decision point (compound: user + agent + amount) |
| **PingOne** | Identity Provider (PKCE, JWKS, RFC 8693 token exchange, MFA push) |

## Token Chain

```
user_token          sub=alice  aud=bridge-resource  scope=stripe_mcp:invoke
    ↓  stored in ADK session state by bridge
    ↓  RFC 8693 (Agent: AGENT_IDP_CLIENT_ID as actor)
delegated_token     sub=alice  act.client_id=agent  scope=stripe_mcp:invoke
    ↓  attached by agent's mcp_headers() → arrives at extension service
    ↓  RFC 8693 (Extension Service: IDP_CLIENT_ID as actor)
tool_token          sub=alice  act chain  aud=stripe-mcp-server  scope=stripe_mcp:invoke
    ↓  injected by extension service → received by Stripe MCP server
```

## Prerequisites

- Google Cloud project with billing enabled
- `gcloud` CLI authenticated against the target project
- Docker (to build service images)
- A PingOne environment with PingOne Authorize
- A Stripe account with products and customers configured

## Deployment

Deploy in this order — each step depends on the previous one having a live URL.

### 1. Stripe MCP Server

Follow [stripe-mcp-server](services/stripe-mcp-server/README.md) to deploy to Cloud Run and register it as an MCP Server in Agent Registry.

### 2. Agent Gateway Extension Service

Follow [agent-gateway-extension-service](services/agent-gateway-extension-service/README.md) to deploy to Cloud Run.

### 3. Agent Gateway

Follow [agent-gateway](services/agent-gateway/README.md) to create the gateway, attach the extension service, and register egress destinations (Stripe MCP server + PingOne endpoint).

### 4. Agent

Follow [agent](services/agent/README.md) to deploy to Agent Runtime. Note the Reasoning Engine resource name from the output — the Agent Bridge needs it.

### 5. Agent Bridge

Follow [agent-bridge](services/agent-bridge/README.md) to deploy to Cloud Run. Note the service URL — the Chat UI needs it.

### 6. Chat UI

Follow [chat-ui](services/chat-ui/README.md) to build and deploy.

## Verify

Open the Chat UI URL, sign in with a PingOne user who has a Stripe customer record, and ask the agent to make a purchase:

> "What products are available? Buy me one widget."

Watch the logs in Cloud Run for both the extension service and Stripe MCP server:

```
# Extension service (each MCP request)
[ExtSvc] request authority="obo-stripe-mcp-server-<number>.us-central1.run.app" path="/mcp"
[ExtSvc] injecting delegated tool token for obo-stripe-mcp-server-... (user=<sub> agent=<client_id>)

# Extension service (tools/call only — Authorize fires on body phase)
[ExtSvc] authorize user=<sub> agent=<client_id> tool=create_stripe_payment_intent amount_cents=2900 hour=14
[ExtSvc] PingOne Authorize PERMIT user=<sub> agent=<client_id>

# Stripe MCP server (each MCP request)
# (act claim validated, user email resolved from PingOne userinfo)

# Stripe MCP server (tools/call only)
tool=create_stripe_payment_intent — success: email=alice@example.com product_id=prod_... quantity=1
```

**Step-up flow** — for purchases above $1000, the extension service returns `401 step_up_required`. The bridge surfaces it as a 401 to the chat UI, which saves the pending message and redirects the user through a new PingOne PKCE flow requesting `stripe_mcp:high_value`. PingOne enforces MFA natively. After approval the user lands back in the chat UI with an elevated token, and the pending message is auto-submitted.

## When to use this pattern

Use this pattern when an autonomous agent must act with a human user's delegated authority rather than its own. The user proves their identity at login; the gateway is the single point where both agent trust and user consent are verified before any tool operation is executed. This gives you:

- **No user credentials in the agent.** The agent never holds a token that grants the user's full authority — only a scoped, short-lived delegated token valid for one tool.
- **Compound authorization.** PingOne Authorize evaluates both the agent's identity and the user's delegated scope together. A rogue agent cannot use a stolen user token, and a user cannot invoke a tool through an unauthorized agent.
- **Dynamic step-up.** High-value operations require proof of live user presence (MFA) before proceeding, enforced at the gateway without changing the agent or tool.
- **Full audit trail.** Every request is logged with `traceID`, `user_sub`, `agent_client_id`, delegated scopes, Authorize decision, and target service.

This pattern builds directly on the [baseline autonomous agent-to-tool](../baseline-autonomous-agent-to-tool) demo, extending the delegation model to carry a human user's identity through the exchange.

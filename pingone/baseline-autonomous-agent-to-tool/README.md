# Baseline Autonomous Agent to Tool

A CRM agent restocks inventory by calling an external supply-chain [MCP](https://modelcontextprotocol.io/docs/2026-07-28/getting-started/intro) tool - but it never holds a credential for that tool. Its egress is routed through the [GCP Agent Gateway](https://docs.cloud.google.com/gemini-enterprise-agent-platform/govern/gateways/agent-gateway-overview), which hands each request to an extension service over [Envoy External Processing (ext_proc)](https://www.envoyproxy.io/docs/envoy/latest/configuration/http/http_filters/ext_proc_filter). That service asks [PingOne Authorize](https://www.pingidentity.com/en/product/pingone-authorize.html) for a PERMIT/DENY decision, and on PERMIT exchanges the agent's own token for one scoped to the downstream tool, injecting it into the request.

## Architecture

![Baseline Autonomous Agent to Tool reference architecture](../../_docs/baseline-autonomous-agent-to-tool/architecture.svg)

Following the diagram: the agent authenticates to PingOne as its own client and carries that token on every MCP request, which Agent Runtime routes through the gateway to the extension service. The service asks PingOne Authorize for a decision; on PERMIT it performs a delegation token exchange, minting a token audienced for the tool, and injects it into the request before it's forwarded to the MCP server.

## Components

| Component | Role |
|---|---|
| [**Agent**](services/agent) | CRM agent acting as a MCP client, mints its own PingOne token as the delegation subject |
| [**Agent Gateway Extension Service**](services/agent-gateway-extension-service) | ext_proc handler, forwards request to PingOne Authorize, exchanges & injects IdP token |
| [**Supply Chain MCP Tool**](services/supply-chain-mcp-tool) | MCP server (`restock`), validates the injected token |
| [**Agent Gateway**](services/agent-gateway) | Google-managed policy enforcement point |
| **PingOne Authorize** | External policy decision point |
| **PingOne** | Identity Provider |

## Prerequisites

- Google Cloud project with billing enabled
- `gcloud` CLI authenticated against the target project
- Docker (to build service images)
- A PingOne environment with PingOne Authorize

## Deployment

### 1. Supply Chain MCP Tool
Follow the instructions in [supply-chain-mcp-tool](services/supply-chain-mcp-tool/README.md) to deploy this service to Cloud Run and register it in Agent Registry.

### 2. Agent Gateway Extension Service
Follow the instructions in [agent-gateway-extension-service](services/agent-gateway-extension-service/README.md) to deploy this service to Cloud Run.

### 3. Agent Gateway
Follow the instructions in [agent-gateway](services/agent-gateway/README.md) to create the gateway, attach the extension service, and register the egress destinations.

### 4. Agent
Follow the instructions in [agent](services/agent/README.md) to create the agent's PingOne app, deploy it to Agent Runtime, register it, and grant it egress.

## Verify

Trigger a test restock query from the baseline root:

```bash
services/agent/.venv/bin/python trigger.py
```

Watch the logs light up in sequence:

```
# Extension service (each MCP request: initialize, tools/list, tools/call)
[ExtSvc] request authority="baatt-supply-chain-mcp-tool-909399354423.us-central1.run.app" path="/mcp"
[ExtSvc] delegated tool token minted (ttl 59m30s)   # only on cache miss
[ExtSvc] injecting delegated token for baatt-supply-chain-mcp-tool-909399354423.us-central1.run.app

# Extension service (tools/call only — Authorize fires on body phase)
[ExtSvc] authorize agent=727857d8-4259-4500-877b-def8ee4663c8 quantity=500 hour=15
[ExtSvc] PingOne Authorize PERMIT agent=727857d8-4259-4500-877b-def8ee4663c8

# Supply chain tool (each MCP request)
[SupplyChain] Token verified — scope "supply-chain:restock" present, forwarding to MCP handler

# Supply chain tool (tools/call only)
[SupplyChain] tools/call restock — 500 units of WIDGET-9000 for region us-west-2
```

## When to use this pattern

This is the baseline for giving an autonomous agent access to a protected tool without embedding tool credentials in the agent. The agent proves only its own identity; the gateway is the single point where authorization is decided and a scoped, short-lived token is minted per request. This gives you:

- **No standing tool credentials in the agent.** A compromised agent cannot leak a token that works against the tool, because it never holds one.
- **Centralized, external authorization.** PingOne Authorize makes the PERMIT/DENY decision and the extension service fails closed, so policy lives in one auditable place and changes without redeploying the agent.
- **Least privilege.** Each token is scoped to a single tool, expires quickly, and carries the agent's delegated identity through to the tool.

Reach for it when tools enforce their own OAuth/JWT auth, when a security team must govern which agents reach which tools from central policy, or when authorization should be tied to the agent's identity rather than a shared key.

This journey is also the foundation for the on-behalf-of-user and multi-agent chaining patterns, which extend the same delegation model to carry a user's or another agent's identity through the exchange.

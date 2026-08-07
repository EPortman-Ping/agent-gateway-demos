# Agent Gateway

The Agent Gateway is a **Google-managed** resource. It's the policy enforcement point for all agent egress: every MCP request from Agent Runtime is routed through it, and it calls the [extension service](../agent-gateway-extension-service/README.md) via Envoy External Processing (`ext_proc`) on every request.

## 1. Create the gateway

In the console: **Agent Platform → Govern → Gateways → Add gateway**.

| Field | Value |
|---|---|
| **Name** | `obo-agent-gateway` |
| **Region** | Same as the two Cloud Run services |
| **Deployment mode** | Google-managed |
| **Governed Access Path** | Agent-to-Anywhere (egress) |
| **Access Authorization** | Enforce policies |

## 2. Attach the extension service

```bash
cp .env.sample .env
make attach
```

| Variable | Value |
|---|---|
| `GC_REGION` | Same region as the gateway |
| `GC_GATEWAY_NAME` | `obo-agent-gateway` |
| `GC_EXT_SVC_NAME` | Deployed extension service Cloud Run name |
| `GC_AUTHZ_EXTENSION` | e.g. `obo-ext-proc-authzext` |
| `GC_AUTHZ_POLICY` | e.g. `obo-ext-proc-authzpolicy` |

## 3. Register egress destinations

In **Agent Platform → Govern → Agent Registry**:

- **Stripe MCP Tool** — registered under **MCP Servers** when you deployed it.
- **PingOne** — under **Endpoints → Add endpoint**, Destination URL = `https://auth.pingone.com` (or your regional variant).
- Google APIs (`*.mtls.googleapis.com`) — auto-created with the gateway. Leave them alone.

## 4. Create the PingOne Resource for the gateway

In PingOne, create a **Resource** named `OBO Google Cloud Agent Gateway` with audience `google-agent-gateway` and two scopes:
- `stripe_mcp:invoke` — required on all requests
- `stripe_mcp:high_value` — granted after MFA step-up, required for transactions above the step-up threshold

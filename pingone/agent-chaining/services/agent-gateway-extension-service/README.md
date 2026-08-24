# Agent Gateway Extension Service

The Agent Chaining ext_proc service for the shared Google Agent Gateway. It governs both protocol hops:

```text
Support Agent → native A2A → Order Status Agent
Order Status Agent → MCP → Order Status MCP Server
```

For each matched target it validates the inbound PingOne JWT audience and scope, uses an explicit permit-all authorization stub during plumbing mode, forwards the native A2A delegation or performs the MCP RFC 8693 exchange, replaces the bearer token when required, and echoes the request body. Actor checks and real PingOne Authorize policy are intentionally deferred. It does not log raw tokens or resolve user email.

## Configure

```bash
cp .env.sample .env
```

Configure both target URLs with their expected audience and scope. `MCP_TARGET_URL` is the deployed Cloud Run MCP endpoint. `A2A_TARGET_URL` is the native Reasoning Engine A2A endpoint recorded after deploying Order Status Agent. The extension forwards the validated A2A delegation unchanged and only downscopes the MCP hop.

## Deploy

```bash
make deploy
```

The service runs gRPC ext_proc on port `50051` and is deployed with `--allow-unauthenticated`; the Agent Gateway calls it, while application validation and PingOne Authorize enforce the security boundary.

Attach this service to the Agent Chaining gateway using the templates in `services/agent-gateway`. Do not deploy the Stripe/on-behalf-of-user extension unchanged.

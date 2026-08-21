# Order Status A2A Adapter

Cloud Run ingress for the Order Status Agent. It accepts the A2A `message/send` request from Support Agent, validates the delegated user token, stores that token in an Order Status Agent session, and invokes the Order Status Agent Reasoning Engine.

The adapter is deliberately thin: it does not perform the RFC 8693 exchange and does not call the MCP server. The Order Status Agent owns the downstream capability.

## Configure and deploy

Copy `.env.sample` to `.env`, set the Order Status Agent engine resource, and deploy with the same Cloud Run service convention as `agent-on-behalf-of-user/services/agent-bridge`.

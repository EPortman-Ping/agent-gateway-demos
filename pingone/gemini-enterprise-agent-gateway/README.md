# Gemini Enterprise Agent Gateway

A Gemini Enterprise agent manages a live employee directory backed by the real [PingOne Users API](https://apidocs.pingidentity.com/pingone/platform/v1/api/#users) — but it never holds a credential for that API.

The user authenticates through Google Workspace (Gemini Enterprise's native identity). Gemini Enterprise's built-in [auth manager](https://docs.cloud.google.com/iam/docs/auth-with-3lo) handles the 3-legged OAuth flow against PingOne, minting a user-scoped token that travels with every MCP request. The [GCP Agent Gateway](https://docs.cloud.google.com/gemini-enterprise-agent-platform/govern/gateways/agent-gateway-overview) intercepts those requests via [Envoy ext_proc](https://www.envoyproxy.io/docs/envoy/latest/configuration/http/http_filters/ext_proc_filter) and calls an extension service. The extension service validates the inbound token, then asks [PingOne Authorize](https://www.pingidentity.com/en/product/pingone-authorize.html) whether this user is permitted to invoke this specific HR tool. Members of `hr_team` may list and look up employees; only members of `hr_admin` may create them. On PERMIT, the extension service performs an [RFC 8693 token exchange](https://docs.pingidentity.com/pingone/use_cases/p1_oauth_2_token_exchange_delegation.html) — minting a short-lived tool-scoped token — and injects it before the request reaches the MCP server.

## Architecture

![Gemini Enterprise Agent Gateway reference architecture](../../_docs/gemini-enterprise-agent-gateway/architecture.svg)

The user sends a message through the Gemini Enterprise UI. Gemini Enterprise's auth manager performs a 3-legged OAuth flow against PingOne to obtain a user token, which it attaches to each outbound MCP request. Agent Runtime routes those requests through the Agent Gateway, which calls the extension service. The extension service validates the token, calls PingOne Authorize with the request context, and on PERMIT performs an RFC 8693 token exchange to produce a tool-scoped token before the request reaches the HR MCP server.

## Components

| Component | Role |
|---|---|
| [**Agent Gateway Extension Service**](services/agent-gateway-extension-service) | ext_proc handler; validates token, calls PingOne Authorize, exchanges & injects IdP token |
| [**HR MCP Server**](services/hr-mcp-server) | MCP server; validates injected token and calls the PingOne Users API to list, look up, and create employees |
| [**Agent Gateway**](services/agent-gateway) | Google-managed policy enforcement point |
| **Gemini Enterprise** | Managed agent platform; handles 3LO auth manager flow against PingOne |
| **PingOne Authorize** | External policy decision point |
| **PingOne** | Identity Provider; issues user tokens via 3-legged OAuth |

## Prerequisites

- Google Cloud project with billing enabled and Gemini Enterprise provisioned
- `gcloud` CLI authenticated against the target project
- Docker (to build service images)
- A PingOne environment with PingOne Authorize

## Deployment

### 1. HR MCP Server
Follow the instructions in [hr-mcp-server](services/hr-mcp-server/README.md) to deploy this service to Cloud Run and register it in Agent Registry.

### 2. Agent Gateway Extension Service
Follow the instructions in [agent-gateway-extension-service](services/agent-gateway-extension-service/README.md) to deploy this service to Cloud Run.

### 3. Agent Gateway
Follow the instructions in [agent-gateway](services/agent-gateway/README.md) to create the gateway, attach the extension service, and register the egress destinations.

### 4. Gemini Enterprise App
Create a Gemini Enterprise app and bind it to the Agent Gateway following the [Route Gemini Enterprise traffic through Agent Gateway](https://docs.cloud.google.com/gemini-enterprise-agent-platform/govern/gateways/agent-gateway-ge-deploy) guide. Configure the auth manager with your PingOne authorization and token endpoints, then register the HR MCP server as a tool.

## Verify

Open the Gemini Enterprise app URL and try both access tiers:

**As an `hr_team` member** — list and look up employees:
> "List all employees in the directory."
> "Look up alice@example.com."

**As an `hr_admin` member** — create a new employee:
> "Create a new employee: Jane Doe, jane.doe@example.com."

**As a user in neither group** — any HR tool call is denied at the gateway before the MCP server is reached.

Watch the logs in Cloud Run:

```
# Extension service (tools/call — hr_team user listing employees)
[ExtSvc] authorize user=<sub> tool=list_employees hour=14
[ExtSvc] PingOne Authorize PERMIT user=<sub>

# Extension service (tools/call — unauthorized user attempting create)
[ExtSvc] authorize user=<sub> tool=create_employee hour=14
[ExtSvc] PingOne Authorize DENY user=<sub>

# HR MCP server (only reached on PERMIT)
[HRSvc] tool=list_employees — success: caller=grace@example.com count=12
[HRSvc] tool=create_employee — success: caller=admin@example.com created=<new-user-id>
```

## When to use this pattern

Use this pattern when you want to connect a Gemini Enterprise agent to a PingOne-protected tool without writing any token-fetching code in the agent itself. The auth manager handles the OAuth flow; PingOne issues the user token; the gateway enforces tiered access — `hr_team` for read operations, `hr_admin` for write operations — before any API call is made. This gives you:

- **No credential management in agent code.** Gemini Enterprise's auth manager owns the OAuth flow against PingOne. The agent never touches a client secret.
- **User identity at the tool.** The RFC 8693-exchanged token carries the authenticated user's `sub` through to the MCP server, enabling per-user audit records and fine-grained data access.
- **Compound authorization.** PingOne Authorize evaluates user identity, the requested tool, and any request attributes together. Policy changes without redeploying any service.

This pattern extends the [agent-on-behalf-of-user](../agent-on-behalf-of-user) demo to a fully managed agent platform, replacing the custom PKCE login UI and ADK agent with Gemini Enterprise's native auth manager and agent runtime.

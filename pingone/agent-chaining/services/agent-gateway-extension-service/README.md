# Agent Gateway Extension Service

The Agent Chaining ext_proc service for the shared Google Agent Gateway. It governs both protocol hops:

```text
Support Agent → native A2A → Order Status Agent
Order Status Agent → MCP → Order Status MCP Server
```

For each matched target it validates the inbound PingOne JWT against the shared `AGENT_GATEWAY_AUDIENCE`, uses an explicit permit-all authorization stub during plumbing mode, then performs its own RFC 8693 exchange — with its own actor — from that shared audience to the hop's real, final audience (`order-status-agent` for the A2A hop, `order-status-mcp-server` for the MCP hop). Every hop gets a freshly gateway-reminted token, never a passthrough. For the A2A hop it also injects a Google credential into `Authorization` (Order Status Agent's native endpoint is a Google-hosted surface with its own IAM check) and carries the reminted PingOne token in the request body instead. Actor checks and real PingOne Authorize policy are intentionally deferred. It does not log raw tokens.

## Configure

```bash
cp .env.sample .env
```

Configure both target URLs with their expected exchange audience and scope. `MCP_TARGET_URL` is the deployed Cloud Run MCP endpoint. `A2A_TARGET_URL` is the native Reasoning Engine A2A endpoint recorded after deploying Order Status Agent. `AGENT_GATEWAY_AUDIENCE` is the shared intermediate PingOne audience both agents' own exchanges target — see the PingOne setup below.

## Deploy

```bash
make deploy
```

The service runs gRPC ext_proc on port `50051` and is deployed with `--allow-unauthenticated`; the Agent Gateway calls it, while application validation and PingOne Authorize enforce the security boundary.

Attach this service to the Agent Chaining gateway using the templates in `services/agent-gateway`. Do not deploy the Stripe/on-behalf-of-user extension unchanged.

## PingOne setup

Two separate things are needed in PingOne, and they answer different questions: the **resource** (below) defines what a token minted for the shared gateway audience should contain; a **client** defines who this service *is* when it calls PingOne's `/token` endpoint at all. Without a client, `IDP_CLIENT_ID`/`IDP_CLIENT_SECRET` above have nothing to authenticate as, and neither the `client_credentials` actor-token fetch (`idp.go::refreshActor`) nor the `token-exchange` remint (`idp.go::exchange`) can happen.

### Client

Create an OIDC Web App application for this extension:

- **Grant types:** Client Credentials (to mint its own actor tokens) and Token Exchange (to perform the remint at each hop).
- **Resources/scopes assigned:** `order-status-agent` (scope `order-status:invoke`) and `order-status-mcp-server` (scope `order:read`) — the two *real final* resources it exchanges to and mints actor tokens for at each hop. It does **not** need to be assigned to the `ac-google-cloud-agent-gateway` resource itself — this service only ever *validates* tokens minted against that audience (a pure signature check against its JWKS, which any party with network access can do), it never requests one for itself.
- The resulting client ID/secret become `IDP_CLIENT_ID`/`IDP_CLIENT_SECRET` above.

This is one of three clients in the full chain — see the table in [CLAUDE.md](../../CLAUDE.md#pingone-setup--configuring-the-act-claim-delegation-proof) for how all three (Support Agent's, this extension's, Order Status Agent's) fit together, and why `refreshActor` requests an explicit scope per call rather than a single shared unscoped token (a client assigned scopes across more than one PingOne resource can't request `client_credentials` without naming a scope explicitly).

### Resource

`AGENT_GATEWAY_AUDIENCE` only works once a matching custom resource exists in PingOne. Support Agent's and Order Status Agent's own RFC 8693 exchanges target this resource instead of the real final audience directly — this extension is the only party that ever exchanges *from* it to a hop's real audience, which keeps `order-status-agent`/`order-status-mcp-server` each touched by exactly one exchange (see [CLAUDE.md](../../CLAUDE.md) for why that matters for the `act` claim).

As a PingOne administrator, add a custom resource for the shared gateway audience:

**Create Resource Profile**

1. In the PingOne admin console, go to **Applications > Resources** and click the **+** icon.
2. For **Resource Name**, enter `ac-google-cloud-agent-gateway`.
3. For **Audience**, enter `ac-google-cloud-agent-gateway` (must match `AGENT_GATEWAY_AUDIENCE` above exactly, and in Support Agent's and Order Status Agent's own configs).
4. Leave **Access token time to live** at its default.
5. Click **Next**.

**Attributes** — this is where the resource proves *who delegated to whom*. Token exchange works without it, but nothing would stop a caller from presenting an `actor_token` and having it accepted at face value; these attributes make PingOne itself attest to the actor, and refuse to mint a token if the actor isn't the one the subject token actually authorized (see [CLAUDE.md](../../CLAUDE.md) for why this lives on the resource, not the client).

1. Leave `sub` as `#root.context.requestData.subjectToken.sub`.
2. Click **Add**, name the attribute `act`, open its Advanced Expressions, enter, and mark **required**:
   ```text
   (#root.context.requestData.grantType == "client_credentials")?"noActor":((#root.context.requestData.subjectToken.may_act.sub == #root.context.requestData.actorToken.client_id)?#root.context.requestData.subjectToken.may_act:null)
   ```
3. Click **Add**, name the attribute `may_act`, open its Advanced Expressions, and enter:
   ```text
   {"sub":"<this extension's own PingOne client ID>"}
   ```
   This resource is only ever exchanged onto by Support Agent or Order Status Agent, and in both cases the only actor allowed to exchange further on top of the result is this extension — so it's a constant, not a branching expression.

**Scopes**

Add both `order-status:invoke` (used by Support Agent's exchange) and `order:read` (used by Order Status Agent's exchange) — this one resource is targeted by both hops.

See [order-status-agent's README](../order-status-agent/README.md#pingone-resource-setup) and [order-status-mcp-server's README](../order-status-mcp-server/README.md#pingone-resource-setup) for the two final resources, and [CLAUDE.md](../../CLAUDE.md) for how to verify the full 4-hop `act` chain once all three are configured.

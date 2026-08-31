# Agent Gateway Extension Service

The Agent Chaining ext_proc service for the shared Google Agent Gateway. It governs both protocol hops:

```text
Support Agent → native A2A → Order Status Agent
Order Status Agent → MCP → Order Status MCP Server
```

For each matched target it validates the inbound PingOne JWT against the shared `AGENT_GATEWAY_AUDIENCE`, uses an explicit permit-all authorization stub during plumbing mode, then performs its own RFC 8693 exchange — with its own actor — from that shared audience to the hop's real, final audience (`order-status-agent` for the A2A hop, `order-status-mcp-server` for the MCP hop). Every hop gets a freshly gateway-reminted token, never a passthrough. For the A2A hop it also injects a Google credential into `Authorization` (Order Status Agent's native endpoint is a Google-hosted surface with its own IAM check) and carries the reminted PingOne token in the request body instead. Actor checks are coded but disabled (`A2A_EXPECTED_ACTOR` unset); real PingOne Authorize policy is deferred (`AUTHZ_MODE=permit-all`). It does not log raw tokens.

## Configure

### 1. PingOne client (OIDC Web App)

Create an **OIDC Web App application** in PingOne:
- **Name:** AC Agent Gateway Extension
- **Grant Types:** enable both **Client Credentials** and **Token Exchange**
- **Resources/scopes assigned:** `order-status:invoke` **from `AC Order Status Agent`** and `order:read` **from `AC Order Status MCP Server`** — the two *real final* resources this extension exchanges to and mints actor tokens for at each hop. It does **not** need a grant on `ac-google-cloud-agent-gateway` itself — this service only ever *validates* tokens minted against that audience (a pure signature check against its JWKS), it never requests a `client_credentials`/exchange token scoped to it.
- The resulting client ID/secret become `IDP_CLIENT_ID`/`IDP_CLIENT_SECRET` below.

This is one of three clients in the full chain — see the table in [CLAUDE.md](../../CLAUDE.md#pingone-setup--configuring-the-act-claim-delegation-proof) for how all three (Support Agent's, this extension's, Order Status Agent's) fit together, and why `refreshActor` requests an explicit scope per call rather than a single shared unscoped token (a client assigned scopes across more than one PingOne resource can't request `client_credentials` without naming a scope explicitly).

### 2. PingOne resource (`ac-google-cloud-agent-gateway`)

`AGENT_GATEWAY_AUDIENCE` below only works once a matching custom resource exists in PingOne. Support Agent's and Order Status Agent's own RFC 8693 exchanges target this shared resource instead of the real final audience directly — this extension is the only party that ever exchanges *from* it to a hop's real audience, which keeps `order-status-agent`/`order-status-mcp-server` each touched by exactly one exchange (see [CLAUDE.md](../../CLAUDE.md) for why that matters for the `act` claim).

**Create Resource Profile**

1. In the PingOne admin console, go to **Applications > Resources** and click the **+** icon.
2. For **Resource Name**, enter `ac-google-cloud-agent-gateway`.
3. For **Audience**, enter `ac-google-cloud-agent-gateway` (must match `AGENT_GATEWAY_AUDIENCE` below exactly, and in Support Agent's and Order Status Agent's own configs).
4. Leave **Access token time to live** at its default.
5. Click **Next**.

**Attributes** — this is where the resource proves *who delegated to whom*. Token exchange works without it, but nothing would stop a caller from presenting an `actor_token` and having it accepted at face value; these attributes make PingOne itself attest to the actor, and refuse to mint a token if the actor isn't the one the subject token actually authorized (see [CLAUDE.md](../../CLAUDE.md) for why this lives on the resource, not the client).

1. Click the gear icon next to the `sub` attribute to open the Advanced Expressions modal, and enter:
   ```text
   (#root.context.requestData.grantType == "client_credentials") ? "no-subject" : #root.context.requestData.subjectToken.sub
   ```
   The `subjectToken.sub` half carries the customer's identity through both exchanges this resource handles (hops 1 and 3). **The `client_credentials` branch is required, not optional**: both Support Agent's and Order Status Agent's own actor-token fetches are plain `client_credentials` requests against this same resource, and that grant type has no `subjectToken` at all — leaving this expression unconditional 400s every actor-token fetch with `sub is configured as required for the Access token but does not have a value`, which blocks the entire chain before any exchange even runs.
2. Click **Add**, name the attribute `act`, open its Advanced Expressions, enter, and mark **required**:
   ```text
   (#root.context.requestData.grantType == "client_credentials")?"noActor":((#root.context.requestData.subjectToken.may_act.sub == #root.context.requestData.actorToken.client_id)?#root.context.requestData.subjectToken.may_act:null)
   ```
3. Click **Add**, name the attribute `may_act`, open its Advanced Expressions, and enter:
   ```text
   {"sub":"fbd9fb33-9134-4dc3-b2b1-3411bb8cf336"}
   ```
   This resource is only ever exchanged onto by Support Agent (hop 1) or Order Status Agent (hop 3), and in both cases the only actor allowed to exchange further on top of the result is this extension — so it's a constant, not a branching expression. `fbd9fb33-...` is this extension's own PingOne client ID; substitute your own if it differs.
4. Click **Next**.

**Scopes**

Add both `order-status:invoke` (used by Support Agent's exchange, hop 1) and `order:read` (used by Order Status Agent's exchange, hop 3) — this one resource is targeted by both hops, with different scopes.

### 3. Environment values

```bash
cp .env.sample .env
```

Configure both target URLs with their expected exchange audience and scope. `MCP_TARGET_URL` is the deployed Cloud Run MCP endpoint. `A2A_TARGET_URL` is the native Reasoning Engine A2A endpoint recorded after deploying Order Status Agent. `AGENT_GATEWAY_AUDIENCE` is the shared intermediate PingOne audience both agents' own exchanges target — this is the resource created in step 2 above.

## Deploy

```bash
make deploy
```

The service runs gRPC ext_proc on port `50051` and is deployed with `--allow-unauthenticated`; the Agent Gateway calls it, while application validation and PingOne Authorize enforce the security boundary.

Attach this service to the Agent Chaining gateway using the templates in `services/agent-gateway`. Do not deploy the Stripe/on-behalf-of-user extension unchanged.

## The two application-grant redirections this design depends on (critical, easy to get backwards)

This extension's own PingOne client is correctly granted the **terminal-resource** copies of its scopes (step 1 above) — but **Support Agent's and Order Status Agent's own applications must be granted the opposite: the `ac-google-cloud-agent-gateway` copies** of `order-status:invoke` and `order:read` respectively, not the terminal-resource copies. Each scope name exists as two separate scope objects (one per resource) specifically so both hops of each pair can be distinguished by `act`/`may_act` — see [order-status-agent's README](../order-status-agent/README.md#pingone-resource-setup) and [CLAUDE.md](../../CLAUDE.md#pingone-client-scope-requirements-critical-non-obvious) for the failure mode if this is backwards: nothing errors, `client_credentials` calls still "succeed" (just against the wrong resource), and the actual symptom is a `token-exchange` call that explicitly requests `audience=ac-google-cloud-agent-gateway` silently landing on a different resource instead — surfacing one hop later as an `act is configured as required` 400.

See [order-status-agent's README](../order-status-agent/README.md#pingone-resource-setup) and [order-status-mcp-server's README](../order-status-mcp-server/README.md#pingone-resource-setup) for the two final resources, and [CLAUDE.md](../../CLAUDE.md) for how to verify the full 4-hop `act` chain once all three resources and all three application grants are configured.

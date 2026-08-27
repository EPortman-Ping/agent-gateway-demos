# Order Status Agent

The specialized downstream agent in the Agent Chaining reference architecture. It receives `get_order_status` requests from the Support Agent over A2A and is the only agent allowed to call the Order Status MCP Server.

## Configure

```bash
cp .env.sample .env
```

| Variable | Purpose |
|---|---|
| `GC_PROJECT_ID` | Google Cloud project |
| `GC_REGION` | Agent Runtime region |
| `AGENT_DISPLAY_NAME` | Order Status Agent Reasoning Engine display name |
| `GC_AGENT_GATEWAY` | Shared Agent-to-Anywhere gateway resource |
| `A2A_ORDER_STATUS_AUDIENCE` | This agent's token audience |
| `A2A_ORDER_STATUS_SCOPE` | Scope accepted from Support Agent |
| `MCP_ORDER_STATUS_SERVER_URL` | Order Status MCP Server endpoint |
| `MCP_ORDER_STATUS_SCOPE` | Scope requested for the MCP hop |
| `AGENT_GATEWAY_AUDIENCE` | Shared intermediate PingOne audience this agent's own exchange targets — the gateway extension performs the real exchange to `order-status-mcp-server` on top of this one |
| `AGENT_IDP_TOKEN_ENDPOINT` | PingOne token endpoint |
| `AGENT_IDP_CLIENT_ID` | Order Status Agent exchange client |
| `AGENT_IDP_CLIENT_SECRET` | Order Status Agent exchange secret |

## Deploy

```bash
make deploy
```

The agent uses the same `agent.py`, `pingone.py`, `deploy.py`, `teardown.py`, and `Makefile` shape as the existing Agent Runtime demos. Its MCP token must be audience-bound to the Order Status MCP Server and scoped only to `order:read`.

## PingOne resource setup

`A2A_ORDER_STATUS_AUDIENCE`/`A2A_ORDER_STATUS_SCOPE` above only work once a matching custom resource exists in PingOne — this agent never creates it, it only validates tokens against whatever PingOne has already issued. This resource is also where the RFC 8693 `act` (actor) claim gets configured; without it, the delegated token this agent validates carries no proof of who it came from (see [CLAUDE.md](../../CLAUDE.md) for the full explanation). This agent's own *outbound* exchange (to call the MCP server) targets a separate, shared intermediate resource — see the gateway extension's README, linked at the bottom of this section.

As a PingOne administrator, add a custom resource for the Order Status Agent:

**Create Resource Profile**

1. In the PingOne admin console, go to **Applications > Resources** and click the **+** icon.
2. For **Resource Name**, enter `order-status-agent`.
3. For **Audience**, enter `order-status-agent` (this becomes the `aud` claim on every token minted against this resource, and must match `A2A_ORDER_STATUS_AUDIENCE` above exactly).
4. (Optional) For **Description**, enter a brief description of the resource.
5. For **Access token time to live**, leave the default or edit as needed.
6. Click **Next**.

**Attributes** — this is where the resource proves *who delegated to whom*. Token exchange works without it, but nothing would stop a caller from presenting an `actor_token` and having it accepted at face value; these attributes make PingOne itself attest to the actor, and refuse to mint a token if the actor isn't the one the subject token actually authorized (see [CLAUDE.md](../../CLAUDE.md) for why this lives on the resource, not the client).

1. Click the gear icon next to the `sub` attribute to open the Advanced Expressions modal.
2. Enter the following and click **Save**:
   ```text
   #root.context.requestData.subjectToken.sub
   ```
   This is what keeps the customer's identity intact through both exchanges on this resource — without it, `sub` on the exchanged token could default to something other than the original user.
3. Click **Add** to add another attribute. In the **Attribute** field, enter `act`, then click the gear icon to open the Advanced Expressions modal.
4. Enter the following, click **Save**, and select the checkbox to make `act` **required**:
   ```text
   (#root.context.requestData.grantType == "client_credentials")?"noActor":((#root.context.requestData.subjectToken.may_act.sub == #root.context.requestData.actorToken.client_id)?#root.context.requestData.subjectToken.may_act:null)
   ```
   Same logic as every other resource in this demo: `client_credentials` requests (each service minting its own actor token) get `noActor`; token-exchange requests get the subject token's `may_act` value only if it names the current actor, otherwise `null` — which fails the exchange closed rather than issuing an unproven delegation.
5. Click **Add** again. In the **Attribute** field, enter `may_act`, then click the gear icon to open the Advanced Expressions modal.
6. Enter the following and click **Save**:
   ```text
   (#root.context.requestData.grantType == "authorization_code")
     ? {"sub":"2fe7b82c-8739-420c-8790-401d4a6c2065"}
     : {"sub":"6f716c87-e33d-4981-b632-6dd357f03f14"}
   ```
   This resource mints tokens at **two** points, each needing to license a different next actor:
   - At login (`authorization_code` grant, the Chat UI's own token): the only allowed next actor is Support Agent (`2fe7b82c-...`) — this is what lets Support Agent's own exchange (hop 1, which targets the shared `ac-google-cloud-agent-gateway` audience, not this resource) get a valid `act` claim.
   - Anything else minted here is the gateway extension's own remint (hop 2, which *does* target this resource): the only allowed next actor is Order Status Agent itself (`6f716c87-...`) — needed for hop 3, which reads `may_act` off the token this resource just minted, even though hop 3 targets the shared gateway audience again, not this resource.
7. Click **Next**.

**Scopes**

1. Click **Add Scope**.
2. For **Scope Name**, enter `order-status:invoke`.
3. (Optional) Enter a **Description**.
4. Click **Save**.

See [the gateway extension's README](../agent-gateway-extension-service/README.md#pingone-resource-setup) for the shared intermediate `ac-google-cloud-agent-gateway` resource both agents' own exchanges target, [order-status-mcp-server's README](../order-status-mcp-server/README.md#pingone-resource-setup) for the final MCP-hop resource, and [CLAUDE.md](../../CLAUDE.md) for how to verify the full 4-hop `act` chain once all three are configured.

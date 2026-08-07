# Agent Bridge

A FastAPI Cloud Run service that acts as the entry point for the Chat UI. It:

1. Validates the user's PingOne token via JWKS
2. Performs RFC 8693 token exchange (user subject + bridge actor → delegated token with `sub=user, act.client_id=bridge`)
3. Creates (or reuses) an ADK session with the delegated token in state
4. Invokes Agent Runtime via Google Application Default Credentials (service account)
5. On `step_up_required` errors from the extension service: triggers a PingOne MFA push to the user, polls for approval, then re-exchanges for an elevated delegated token and retries

## Configure

**1. PingOne Bridge Application**

Create an **OIDC Web App** in PingOne:

- Name: OBO Agent Bridge
- Grant types: Client Credentials + Token Exchange
- Assign the `OBO Google Cloud Agent Gateway` resource with `stripe_mcp:invoke` scope

On the user's PingOne app (Chat UI PKCE app), add a `may_act` claim or configure the token exchange policy to allow the bridge's `client_id` to act.

**2. PingOne MFA Management Application**

Create a **Worker** application in PingOne for server-side MFA push:

- Name: OBO MFA Manager
- Grant type: Client Credentials
- Scopes: `p1:read:user`, `p1:create:device`, `p1:read:userMFAEnabled`

**3. Fill in `.env`:**

```bash
cp .env.sample .env
```

| Variable | Value |
|---|---|
| `GC_PROJECT_ID` | Target project ID |
| `GC_REGION` | Deploy region |
| `AGENT_ENGINE_NAME` | Full Reasoning Engine resource name from `make deploy` in the agent directory |
| `CORS_ORIGIN` | Chat UI Cloud Run URL |
| `PINGONE_ISSUER` | `https://auth.pingone.com/<ENV_ID>/as` |
| `BRIDGE_CLIENT_ID` | Bridge PingOne app Client ID |
| `BRIDGE_CLIENT_SECRET` | Bridge PingOne app Client Secret |
| `BRIDGE_SCOPE` | `stripe_mcp:invoke` |
| `PINGONE_ENV_ID` | PingOne Environment ID |
| `PINGONE_MGMT_CLIENT_ID` | MFA Manager app Client ID |
| `PINGONE_MGMT_CLIENT_SECRET` | MFA Manager app Client Secret |
| `PINGONE_API_BASE` | `https://api.pingone.com/v1` |

## Deploy

```bash
make deploy
```

`deploy` runs `setup` (creates SA, stores secrets, grants `aiplatform.user` role), then `push` (builds and pushes Docker image), then `gcloud run deploy`.

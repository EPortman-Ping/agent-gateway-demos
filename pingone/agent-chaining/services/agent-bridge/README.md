# Agent Bridge

A FastAPI Cloud Run service that acts as the entry point for the Chat UI. It:

1. Validates the user's PingOne token via JWKS (`iss`, `sub`, signature)
2. Creates (or reuses) an ADK session with `user_token` in state
3. Invokes Agent Runtime and streams the response back

The bridge holds no PingOne credentials and performs no token exchange — it simply validates the inbound token and passes it through to the agent via session state.

## Configure

**1. Fill in `.env`:**

```bash
cp .env.sample .env
```

| Variable | Value |
|---|---|
| `GC_PROJECT_ID` | Target project ID |
| `GC_REGION` | Deploy region, e.g. `us-central1` |
| `GC_CLOUD_RUN_SERVICE_NAME` | Cloud Run service name, e.g. `agent-chain-bridge` |
| `AGENT_ENGINE_NAME` | Full Reasoning Engine resource name from `make deploy` in the agent directory |
| `CORS_ORIGIN` | Chat UI Cloud Run URL |
| `PINGONE_ISSUER` | `https://auth.pingone.<region>/<env-id>/as` |

## Deploy

```bash
make deploy
```

`deploy` runs `setup`, then `push`, then `gcloud run deploy`.

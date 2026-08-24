# Chat UI

A React/Vite single-page app. Users log in via PingOne PKCE (Authorization Code + PKCE), then chat with the Support Agent.

## Configure

**1. Create the UI's PingOne application**

Create a **Single Page App** in PingOne:
- Name: Agent Chaining Chat UI
- Grant type: Authorization Code + PKCE
- Redirect URI: the exact value of `VITE_REDIRECT_URI` (currently `https://ac-agent-chain-chat-ui-f447x3emfq-uc.a.run.app/`, including the trailing slash)
- Signoff URI: the exact Chat UI Cloud Run URL, `https://ac-agent-chain-chat-ui-f447x3emfq-uc.a.run.app/`
- Scopes: `openid profile email order-status:invoke`

PingOne compares `redirect_uri` byte-for-byte. Register the deployed URL before signing in; a missing trailing slash, a different Cloud Run hostname, or a wildcard produces a redirect URI mismatch at the PingOne sign-on page.

**2. Fill in `.env`:**

```bash
cp .env.sample .env
```

| Variable | Value |
|---|---|
| `GC_REGION` | Deploy region, e.g. `us-central1` |
| `GC_CLOUD_RUN_SERVICE_NAME` | Cloud Run service name, e.g. `agent-chain-chat-ui` |
| `VITE_AIC_ISSUER` | `https://auth.pingone.<region>/<env-id>/as` |
| `VITE_CLIENT_ID` | Chat UI PingOne app Client ID |
| `VITE_REDIRECT_URI` | Chat UI Cloud Run URL |
| `VITE_SCOPES` | `openid profile email order-status:invoke` |
| `VITE_AGENT_BRIDGE_URL` | Agent Bridge Cloud Run URL |

## Deploy

```bash
make deploy
```

Builds the Vite app, packages it in an nginx Docker image, and deploys to Cloud Run.

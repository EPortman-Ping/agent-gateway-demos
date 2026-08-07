# Chat UI

A React/Vite single-page app. Users log in via PingOne PKCE (Authorization Code + PKCE), then chat with the OBO Financial Agent. For high-value transactions, a step-up modal is shown while the Agent Bridge handles a server-side PingOne MFA push in the background.

## Configure

**1. PingOne PKCE Application**

Create a **Single Page App** in PingOne:

- Name: OBO Chat UI
- Grant type: Authorization Code + PKCE
- Redirect URI: your Cloud Run URL + `/`
- Scopes: `openid profile email stripe_mcp:invoke`
- Configure the token exchange policy so the bridge's `client_id` appears in the `may_act` claim of issued tokens

**2. Fill in `.env`:**

```bash
cp .env.sample .env
```

| Variable | Value |
|---|---|
| `VITE_AIC_ISSUER` | `https://auth.pingone.com/<ENV_ID>/as` |
| `VITE_CLIENT_ID` | Chat UI PingOne app Client ID |
| `VITE_REDIRECT_URI` | Chat UI Cloud Run URL + `/` |
| `VITE_SCOPES` | `openid profile email stripe_mcp:invoke` |
| `VITE_AGENT_BRIDGE_URL` | Agent Bridge Cloud Run URL |

## Deploy

```bash
make deploy
```

Builds the Vite app, packages it in an nginx Docker image, and deploys to Cloud Run as `obo-chat-ui`.

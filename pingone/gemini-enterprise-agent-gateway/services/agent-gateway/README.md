# Agent Gateway

Configuration and templates to attach the extension service to the Google-managed Agent Gateway and bind it to the Gemini Enterprise app. This is not a deployed service — it produces the YAML resources that wire everything together.

## Prerequisites

- An Agent Gateway already exists in your GCP project (create it in the console or with `gcloud`).
- The extension service is deployed and its Cloud Run hostname is known.
- The Gemini Enterprise app exists and its resource name is known.

## Environment variables

Copy `.env.sample` to `.env` and fill in the values.

| Variable | Description |
|---|---|
| `PROJECT_ID` | GCP project ID (string form, e.g. `project-cc769feb-5628-4231-883`) |
| `REGION` | Region where the gateway is deployed (e.g. `us-central1`) |
| `GATEWAY_ID` | Agent Gateway resource ID |
| `EXT_SVC_HOST` | Cloud Run hostname of the extension service (without `https://`) |
| `GE_APP_ID` | Gemini Enterprise app resource name |

## Attach extension service

```bash
make attach
```

`make attach` renders `authz-policy.yaml.tmpl` and `extension.yaml.tmpl` with values from `.env`, then applies them via `gcloud`:

1. Creates or updates the `TrafficExtension` resource pointing at the extension service.
2. Creates or updates the `AuthzExtension` policy on the gateway with `policyProfile: CONTENT_AUTHZ`.
3. Registers the HR MCP server as an egress destination on the gateway.

## Bind Gemini Enterprise app

After attaching the extension service, bind the Gemini Enterprise app to the gateway following the [Route Gemini Enterprise traffic through Agent Gateway](https://docs.cloud.google.com/gemini-enterprise-agent-platform/govern/gateways/agent-gateway-ge-deploy) guide:

```bash
# Example — replace with your actual resource names
gcloud beta gemini-enterprise apps update $GE_APP_ID \
  --agent-gateway=projects/$PROJECT_ID/locations/$REGION/agentGateways/$GATEWAY_ID
```

Binding takes ~3 minutes to propagate. Transient `403 Egress request is not authorized` errors are normal during this window.

## Notes

- `policyProfile: CONTENT_AUTHZ` is required so the extension service can inspect and rewrite the request body. The extension service must echo body chunks back or the backend receives an empty body.
- The gateway must be deployed in the region that corresponds to the Gemini Enterprise app's setup: `global` → `us-central1`, `eu` → `europe-west1`, `us` → `us-central1`.
- Use the string project ID in all resource names, not the numeric project number.

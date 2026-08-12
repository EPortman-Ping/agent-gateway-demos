# HR MCP Server

Go [MCP](https://modelcontextprotocol.io/docs/2026-07-28/getting-started/intro) server that manages a live employee directory backed by the [PingOne Users API](https://apidocs.pingidentity.com/pingone/platform/v1/api/#users). Validates the injected PingOne JWT on every request and reads the caller's email from the `X-User-Email` header injected by the extension service.

## Tools

| Tool | Description |
|---|---|
| `list_employees` | Lists all employees in the PingOne directory (up to 100) |
| `get_employee` | Returns details for a single employee by email address or user ID |
| `create_employee` | Creates a new employee in PingOne — requires `hr_admin` group membership, enforced by PingOne Authorize at the gateway |

## Configure

### 1. Create the HR MCP Server resource in PingOne

In **Connections → Resources**, create a new resource:

| Field | Value |
|---|---|
| **Name** | `GE HR MCP Server` |
| **Audience** | `hr-mcp-server` |

Add a scope to the resource:

| Field | Value |
|---|---|
| **Name** | `hr_mcp:read` |

### 2. Create HR access groups in PingOne

In **Directory → Groups**, create two groups:

| Group | Purpose |
|---|---|
| `hr_team` | Members may call `list_employees` and `get_employee` |
| `hr_admin` | Members may also call `create_employee` |

Add your test users to the appropriate groups to demonstrate the two access tiers. An `hr_admin` user should also be in `hr_team` (or the Authorize policy should treat admin as a superset of team).

### 3. Create the HR Management Worker app in PingOne

In **Connections → Applications**, create a **Worker** application:

| Field | Value |
|---|---|
| **Name** | `GE HR Management Worker` |
| **Grant type** | Client Credentials |

On the **Roles** tab, grant **Identity Data Admin** scoped to this environment. This role is required so the MCP server can create users via the PingOne management API. Note the Client ID and Client Secret — these become `PINGONE_MGMT_CLIENT_ID` and `PINGONE_MGMT_CLIENT_SECRET`.

### 4. Configure environment values

```bash
cp .env.sample .env
```

| Variable | Value |
|---|---|
| `GC_CLOUD_RUN_SERVICE_NAME` | Cloud Run service name, e.g. `ge-hr-mcp-server` |
| `GC_REGION` | GCP region, e.g. `us-central1` |
| `IDP_ISSUER` | PingOne issuer URL, e.g. `https://auth.pingone.com/<env-id>/as` — JWKS and token URLs are derived from this |
| `IDP_REQUIRED_AUDIENCE` | `hr-mcp-server` — must match the resource audience from step 1 |
| `IDP_REQUIRED_SCOPE` | `hr_mcp:read` — scope the injected token must carry |
| `PINGONE_MGMT_CLIENT_ID` | HR Management Worker Client ID from step 3 |
| `PINGONE_MGMT_CLIENT_SECRET` | HR Management Worker Client Secret from step 3 — stored in Secret Manager |

## Deploy

```bash
make deploy
```

`deploy` runs `setup` (creates service account, stores `PINGONE_MGMT_CLIENT_SECRET` in Secret Manager), then `push`, then `gcloud run deploy`. Auth is enforced by JWT validation in `token_validator.go`, not by Cloud Run IAM.

After deploy, note the Cloud Run service URL — you need it for the extension service's `TOOL_URL`.

## Register in Gemini Enterprise

Before registering, create the **GE Auth Manager** app in PingOne — this is the application Gemini Enterprise uses to perform the 3-legged OAuth flow on behalf of your users:

In **Connections → Applications**, create a **Single Page Application**:

| Field | Value |
|---|---|
| **Name** | `GE Auth Manager` |
| **Grant type** | Authorization Code (PKCE) |
| **Redirect URI** | Gemini Enterprise auth manager callback URL (shown in the GE console after you start adding the MCP server) |

On the **Resources** tab, assign `openid`, `profile`, `email`, and the `hr_mcp:read` scope from the `GE HR MCP Server` resource. Note the Client ID and Client Secret.

Then in the Gemini Enterprise console, add the HR MCP server as a **Data Store** with OAuth 2.0 authentication:

| Field | Value |
|---|---|
| **MCP Server URL** | `<Cloud Run service URL>/mcp` |
| **Authorization URL** | `https://auth.pingone.com/<env-id>/as/authorize` |
| **Token URL** | `https://auth.pingone.com/<env-id>/as/token` |
| **Client ID** | GE Auth Manager app Client ID |
| **Client Secret** | GE Auth Manager app Client Secret |
| **Scopes** | `openid profile email hr_mcp:read` |
| **Enable PKCE** | Yes |

Paste the contents of `tool-spec.json` when prompted for the tool specification.

**MCP Server Description:**
```
HR employee directory server backed by PingOne. Supports listing all employees, looking up a specific employee by email or ID, and creating new employees. Read access (list and lookup) requires membership in the hr_team group. Write access (create) requires membership in the hr_admin group. All access is enforced by PingOne Authorize at the Agent Gateway before any tool call reaches this server.
```

**MCP Agent Instructions:**
```
Use this server to answer questions about employees and to onboard new employees. When a user asks about an employee, use get_employee with their email address. When a user asks to see all employees, use list_employees. When a user asks to add or create a new employee, use create_employee with their first name, last name, email, and username (username should match email). If a tool call is denied, inform the user they do not have the required HR permissions.
```

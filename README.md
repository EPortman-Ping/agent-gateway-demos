# Agent Gateway Demos

End-to-end Go demos of the [Google Cloud Agent Gateway](https://cloud.google.com/agent-gateway) security patterns, illustrating how autonomous AI agents authenticate and act securely across trust boundaries using third-party Identity Providers.

Each demo is self-contained and builds on the previous one.

## Customer Journeys

| Journey | Directory | Status |
|---|---|---|
| 1. Baseline Autonomous Agent to Tool | [`pingone/baseline-autonomous-agent-to-tool/`](./pingone/baseline-autonomous-agent-to-tool/) | Ready |
| 2. Agent Working On-Behalf-Of (OBO) User | `agent-obo-user/` | Coming soon |
| 3. Multi-Agent Chaining (A → B → C) | `multi-agent-chaining/` | Coming soon |

## Architecture

All three journeys share a common foundation:

- **Agent Gateway** (Envoy proxy) — the Policy Enforcement Point (PEP) that intercepts every request
- **Extension Service** — the Policy Decision Point adapter that calls your 3P IdP
- **3P Identity Provider** — the authoritative source for AuthN/AuthZ decisions (Okta, Auth0, Ping, etc.)
- **ID-JAG token** — a short-lived JWT minted by the extension service, carrying delegated authority claims through the agent chain

See the paper: *Agent Gateway: Secure Interoperability and Delegated Authority with Third-Party Identity Providers*

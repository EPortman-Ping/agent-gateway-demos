"""RFC 8693 token provider for Order Status Agent -> MCP calls."""

import os
import httpx


def exchange_for_mcp(user_delegated_token: str) -> str:
    """Exchange the inbound token for an Order Status MCP-scoped token."""
    response = httpx.post(
        os.environ["AGENT_IDP_TOKEN_ENDPOINT"],
        data={
            "grant_type": "urn:ietf:params:oauth:grant-type:token-exchange",
            "subject_token": user_delegated_token,
            "subject_token_type": "urn:ietf:params:oauth:token-type:access_token",
            "requested_token_type": "urn:ietf:params:oauth:token-type:access_token",
            "audience": os.environ["MCP_ORDER_STATUS_AUDIENCE"],
            "scope": os.environ["MCP_ORDER_STATUS_SCOPE"],
        },
        auth=(os.environ["AGENT_IDP_CLIENT_ID"], os.environ["AGENT_IDP_CLIENT_SECRET"]),
        timeout=15,
    )
    response.raise_for_status()
    return response.json()["access_token"]

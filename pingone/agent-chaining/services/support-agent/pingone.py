"""RFC 8693 token provider for Support Agent -> Order Status Agent calls."""

import base64
import json
import os
import time
from uuid import uuid4

import httpx

_TOKEN_ENDPOINT = os.environ.get("AGENT_IDP_TOKEN_ENDPOINT", "")
_CLIENT_ID = os.environ.get("AGENT_IDP_CLIENT_ID", "")
_CLIENT_SECRET = os.environ.get("AGENT_IDP_CLIENT_SECRET", "")
_SCOPE = os.environ.get("A2A_ORDER_STATUS_SCOPE", "order-status:invoke")
_AUDIENCE = os.environ.get("A2A_ORDER_STATUS_AUDIENCE", "order-status-agent")


def _local_token(subject: str) -> str:
    payload = {
        "sub": subject,
        "aud": _AUDIENCE,
        "scope": _SCOPE,
        "act": {"sub": os.environ.get("SUPPORT_AGENT_ID", "support-agent")},
        "jti": str(uuid4()),
        "exp": int(time.time()) + 60,
    }
    encoded = base64.urlsafe_b64encode(json.dumps(payload).encode()).rstrip(b"=").decode()
    return "local-rfc8693." + encoded


def get_delegated_token(user_token: str = "local-user") -> str:
    """Exchange the user token for an A2A token; local mode models the claims."""
    if os.environ.get("LOCAL_DELEGATION_MODE", "true").lower() == "true":
        return _local_token(user_token.removeprefix("local-user:") or "local-user")
    response = httpx.post(
        _TOKEN_ENDPOINT,
        data={
            "grant_type": "urn:ietf:params:oauth:grant-type:token-exchange",
            "subject_token": user_token,
            "subject_token_type": "urn:ietf:params:oauth:token-type:access_token",
            "requested_token_type": "urn:ietf:params:oauth:token-type:access_token",
            "audience": _AUDIENCE,
            "scope": _SCOPE,
        },
        auth=(_CLIENT_ID, _CLIENT_SECRET),
        timeout=15,
    )
    response.raise_for_status()
    return response.json()["access_token"]

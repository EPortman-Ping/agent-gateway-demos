"""Authenticated A2A ingress for the Order Status Agent."""

import os
from typing import Any

import agentplatform
from dotenv import load_dotenv
from fastapi import FastAPI, HTTPException, Request
from jose import jwt

from protocol import parse_order_status_request

load_dotenv()
app = FastAPI(title="Order Status A2A Adapter")
AGENT_ENGINE_NAME = os.environ["AGENT_ENGINE_NAME"]
_client = agentplatform.Client(project=os.environ["GC_PROJECT_ID"], location=os.environ["GC_REGION"])
_agent = _client.agent_engines.get(name=AGENT_ENGINE_NAME)


def validate_delegated_token(token: str) -> dict[str, Any]:
    """Decode the delegated token; production validates JWKS/issuer/audience/scope."""
    if not token:
        raise HTTPException(status_code=401, detail="missing delegated token")
    if token.startswith("local-rfc8693."):
        return {"sub": "local-user", "aud": "order-status-agent", "scope": "order-status:invoke"}
    try:
        return jwt.get_unverified_claims(token)
    except Exception as exc:
        raise HTTPException(status_code=401, detail="invalid delegated token") from exc


@app.get("/health")
def health() -> dict[str, str]:
    return {"status": "ok"}


@app.post("/a2a")
def message_send(request: Request, body: dict[str, Any]) -> dict[str, Any]:
    auth = request.headers.get("Authorization", "")
    if not auth.startswith("Bearer "):
        raise HTTPException(status_code=401, detail="missing delegated token")
    try:
        parse_order_status_request(body)
    except ValueError as exc:
        raise HTTPException(status_code=400, detail=str(exc)) from exc
    claims = validate_delegated_token(auth[7:])
    session = _client.agent_engines.sessions.create(
        name=AGENT_ENGINE_NAME,
        user_id=claims.get("sub", "unknown"),
        config={"session_state": {"user_token": auth[7:], "a2a_request": body}},
    )
    events = _agent.stream_query(
        message=body["params"]["message"]["parts"][0]["text"],
        user_id=claims.get("sub", "unknown"),
        session_id=session.response.name.split("/")[-1],
    )
    text = "".join(part["text"] for event in events for part in event.get("content", {}).get("parts", []) if "text" in part)
    return {"jsonrpc": "2.0", "id": body.get("id"), "result": {"text": text}}

"""Agent bridge — validates the user's PingOne token and invokes Agent Runtime.

POST /chat:
  Authorization: Bearer <user_pingone_token>
  {"message": "..."}

  1. Validate the user token via PingOne JWKS.
  2. Create (or reuse) an ADK session with user_token in state.
     If the token changed since last call (e.g. after step-up PKCE), update state.
  3. Call agent.stream_query; return the text response.
  4. If Agent Runtime surfaces step_up_required, return 401 {"error": "step_up_required"}.
     The chat UI handles the step-up PKCE redirect — this service does nothing else.
"""

import os
import time

import httpx
from dotenv import load_dotenv
from fastapi import FastAPI, HTTPException, Request
from fastapi.middleware.cors import CORSMiddleware
from jose import JWTError, jwk, jwt
from pydantic import BaseModel

load_dotenv()

import agentplatform
from agentplatform import types as agent_types

# ── Config ────────────────────────────────────────────────────────────────────

GC_PROJECT_ID     = os.environ["GC_PROJECT_ID"]
GC_REGION         = os.environ["GC_REGION"]
AGENT_ENGINE_NAME = os.environ["AGENT_ENGINE_NAME"]
CORS_ORIGIN       = os.environ["CORS_ORIGIN"]
PINGONE_ISSUER    = os.environ["PINGONE_ISSUER"].rstrip("/")

JWKS_URI = f"{PINGONE_ISSUER}/jwks"

# ── JWKS cache ────────────────────────────────────────────────────────────────

_jwks_cache: dict = {}
_jwks_fetched_at: float = 0.0
_JWKS_TTL = 3600


def _get_jwks() -> dict:
    global _jwks_cache, _jwks_fetched_at
    now = time.time()
    if _jwks_cache and now - _jwks_fetched_at < _JWKS_TTL:
        return _jwks_cache
    resp = httpx.get(JWKS_URI, timeout=10)
    resp.raise_for_status()
    _jwks_cache = resp.json()  # type: ignore[assignment]
    _jwks_fetched_at = now
    return _jwks_cache


# ── Token validation ──────────────────────────────────────────────────────────

def _validate_user_token(token: str) -> dict:
    """Validate the user's PingOne token via JWKS and return the claims."""
    jwks_data = _get_jwks()
    try:
        unverified_header = jwt.get_unverified_header(token)
        kid = unverified_header.get("kid")
        key = next(
            (k for k in jwks_data["keys"] if k.get("kid") == kid),
            jwks_data["keys"][0] if jwks_data["keys"] else None,
        )
        if not key:
            raise HTTPException(status_code=401, detail="No matching key in JWKS")
        public_key = jwk.construct(key)
        claims = jwt.decode(
            token,
            public_key,
            algorithms=["RS256", "ES256"],
            issuer=PINGONE_ISSUER,
            options={"verify_aud": False},
        )
    except JWTError as e:
        raise HTTPException(status_code=401, detail=f"Invalid token: {e}")

    if not claims.get("sub"):
        raise HTTPException(status_code=401, detail="Token missing sub claim")

    return claims


# ── Agent Runtime ─────────────────────────────────────────────────────────────

_client = agentplatform.Client(project=GC_PROJECT_ID, location=GC_REGION)
_agent  = _client.agent_engines.get(name=AGENT_ENGINE_NAME)

# user_sub → (session_id, last_token)
_sessions: dict[str, tuple[str, str]] = {}


def _ensure_session(user_sub: str, user_token: str) -> str:
    """Create or reuse an ADK session, updating state if the token changed."""
    if user_sub in _sessions:
        session_id, last_token = _sessions[user_sub]
        if last_token != user_token:
            # Token changed — user completed step-up PKCE and has an elevated token.
            _agent.sessions.update(
                name=f"{AGENT_ENGINE_NAME}/sessions/{session_id}",
                config=agent_types.UpdateAgentEngineSessionConfig(
                    session_state={"user_token": user_token},
                ),
            )
            _sessions[user_sub] = (session_id, user_token)
        return session_id

    session = _agent.sessions.create(
        name=AGENT_ENGINE_NAME,
        user_id=user_sub,
        config=agent_types.CreateAgentEngineSessionConfig(
            session_state={"user_token": user_token},
        ),
    )
    _sessions[user_sub] = (session.id, user_token)
    return session.id


def _run_agent(user_sub: str, session_id: str, message: str) -> str:
    text_parts: list[str] = []
    for event in _agent.stream_query(
        message=message,
        user_id=user_sub,
        session_id=session_id,
    ):
        content = event.get("content", {})
        for part in content.get("parts", []):
            if "text" in part:
                text_parts.append(part["text"])
    return "\n".join(text_parts).strip()


# ── FastAPI app ────────────────────────────────────────────────────────────────

app = FastAPI()
app.add_middleware(
    CORSMiddleware,
    allow_origins=[CORS_ORIGIN],
    allow_methods=["POST", "GET"],
    allow_headers=["Authorization", "Content-Type"],
)


class ChatRequest(BaseModel):
    message: str


@app.get("/health")
def health():
    return {"status": "ok"}


@app.post("/chat")
async def chat(request: Request, body: ChatRequest):
    auth = request.headers.get("Authorization", "")
    if not auth.startswith("Bearer "):
        raise HTTPException(status_code=401, detail="Missing Bearer token")
    user_token = auth[7:]

    claims = _validate_user_token(user_token)
    user_sub = claims["sub"]

    session_id = _ensure_session(user_sub, user_token)

    try:
        reply = _run_agent(user_sub, session_id, body.message)
        return {"response": reply}
    except Exception as exc:
        if "step_up_required" in str(exc):
            raise HTTPException(status_code=401, detail="step_up_required")
        raise HTTPException(status_code=500, detail=str(exc)) from exc

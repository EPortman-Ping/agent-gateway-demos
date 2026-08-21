"""Support Agent — delegates order questions to the Order Status Agent."""

import os
from uuid import uuid4

import httpx
from google.adk.agents import Agent
from google.adk.tools import FunctionTool

from protocol import build_a2a_request
from pingone import get_delegated_token

ORDER_STATUS_AGENT_URL = os.environ.get("A2A_ORDER_STATUS_AGENT_URL", "")
ORDER_STATUS_AGENT_AUDIENCE = os.environ.get("A2A_ORDER_STATUS_AUDIENCE", "order-status-agent")
ORDER_STATUS_SCOPE = os.environ.get("A2A_ORDER_STATUS_SCOPE", "order-status:invoke")
SUPPORT_AGENT_ID = os.environ.get("SUPPORT_AGENT_ID", "support-agent")


def get_order_status(order_id: str) -> dict:
    """Delegate an order-status request to the specialized agent."""
    request = build_a2a_request(order_id, str(uuid4()))
    # Local mode models the RFC 8693 result. Production uses pingone.py.
    token = get_delegated_token("local-user")
    response = httpx.post(
        ORDER_STATUS_AGENT_URL,
        json=request,
        headers={"Authorization": f"Bearer {token}"},
        timeout=15,
    )
    response.raise_for_status()
    return response.json()


root_agent = Agent(
    model="gemini-2.5-flash",
    name="support_agent",
    description="Support agent that delegates order-status requests to a specialized agent.",
    instruction=(
        "You are a customer support agent. When a user asks about an order, "
        "call get_order_status with the order ID and report the result. "
        "Do not access order data directly; the Order Status Agent owns that capability."
    ),
    tools=[FunctionTool(get_order_status)],
)

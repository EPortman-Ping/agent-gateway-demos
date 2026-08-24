"""Support Agent — delegates order questions to the native Order Status A2A agent."""

from __future__ import annotations

import json
import os
from uuid import uuid4

import httpx
from google.adk.agents import Agent
from google.adk.tools import FunctionTool
from google.adk.tools.tool_context import ToolContext
from google.genai import types as genai_types

from pingone import get_delegated_token

ORDER_STATUS_AGENT_URL = os.environ["A2A_ORDER_STATUS_AGENT_URL"]


def get_order_status(order_id: str, tool_context: ToolContext) -> dict:
    """Delegate an order-status request to the native A2A agent."""
    print("[support-agent] get_order_status start order_id=" + order_id, flush=True)
    user_token = tool_context.state.get("user_token")
    if not user_token:
        raise RuntimeError("authenticated user token is missing from session state")
    if not order_id.startswith("ORD-") or not order_id[4:].isdigit():
        return {
            "error": "Invalid order ID. Please use the format ORD-123, for example ORD-123 or ORD-456."
        }

    try:
        delegated = get_delegated_token(user_token)
        request_id = str(uuid4())
        response = httpx.post(
            f"{ORDER_STATUS_AGENT_URL}/message:send",
            json={
                "message": {
                    "messageId": request_id,
                    "role": "ROLE_USER",
                    "parts": [{"text": f"get_order_status:{order_id}"}],
                },
                # The gateway extension remints this into its own delegated
                # token before forwarding; sent here so the request is
                # well-formed even if the gateway hop is ever bypassed.
                "metadata": {"delegatedAuthorization": f"Bearer {delegated}"},
            },
            headers={
                "Authorization": f"Bearer {delegated}",
                "A2A-Version": "1.0",
            },
            timeout=30,
        )
        response.raise_for_status()
        payload = response.json()
    except Exception as exc:
        print(f"[support-agent] A2A call failed: {type(exc).__name__}: {exc}", flush=True)
        return {"error": f"order status lookup failed: {exc}"}
    message = payload.get("message") or (payload.get("task") or {}).get("status", {}).get("message")
    if not message:
        return payload
    texts = [part.get("text") for part in message.get("parts", []) if part.get("text")]
    return {"response": "\n".join(texts) if texts else json.dumps(message)}


root_agent = Agent(
    model="gemini-2.5-flash",
    name="support_agent",
    description="Support agent that delegates order-status requests to a specialized agent.",
    instruction=(
        "You are a customer support agent. When a user asks about an order, extract the order ID and call get_order_status. "
        "Order IDs must use the format ORD-123, such as ORD-123 or ORD-456. "
        "If the user gives an invalid or incomplete ID, explain the required format instead of calling the tool. "
        "Report the result clearly. Do not access order data directly; the Order Status Agent owns that capability."
    ),
    tools=[FunctionTool(get_order_status)],
    # Thinking (thought_signature) on function-call turns intermittently
    # crashed the deployed engine's VertexAiSessionService persistence with
    # zero events returned to the client. Disabled to keep tool calls reliable.
    generate_content_config=genai_types.GenerateContentConfig(
        thinking_config=genai_types.ThinkingConfig(thinking_budget=0),
    ),
)

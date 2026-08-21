"""Order Status Agent — specialized agent with the order MCP capability."""

import os
from uuid import uuid4

import httpx
from google.adk.agents import Agent
from google.adk.tools import FunctionTool

from protocol import parse_delegated_token, parse_order_status_request
from pingone import exchange_for_mcp

MCP_URL = os.environ.get("MCP_ORDER_STATUS_SERVER_URL", "")
AGENT_AUDIENCE = os.environ.get("A2A_ORDER_STATUS_AUDIENCE", "order-status-agent")
AGENT_SCOPE = os.environ.get("A2A_ORDER_STATUS_SCOPE", "order-status:invoke")


def get_order_status(order_id: str, delegated_token: str = "") -> dict:
    """Call the protected order MCP server with a downscoped token."""
    parent = parse_delegated_token(delegated_token, audience=AGENT_AUDIENCE, scope=AGENT_SCOPE, actor="support-agent")
    token = exchange_for_mcp(delegated_token)
    response = httpx.post(MCP_URL, json={"jsonrpc":"2.0","id":str(uuid4()),"method":"tools/call","params":{"name":"get_order_status","arguments":{"order_id":order_id}}}, headers={"Authorization":f"Bearer {token}"}, timeout=15)
    response.raise_for_status()
    return response.json()


root_agent = Agent(
    model="gemini-2.5-flash",
    name="order_status_agent",
    description="Specialized agent that retrieves order status through an MCP server.",
    instruction="When Support Agent delegates an order-status request, call get_order_status and return the result.",
    tools=[FunctionTool(get_order_status)],
)

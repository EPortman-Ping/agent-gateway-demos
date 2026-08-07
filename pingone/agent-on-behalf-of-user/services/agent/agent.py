"""OBO agent — ADK agent deployed on Agent Runtime (Agent Engine)."""

import os

from google.adk.agents import Agent
from google.adk.tools.mcp_tool import McpToolset
from google.adk.tools.mcp_tool.mcp_session_manager import StreamableHTTPConnectionParams
from google.genai import types as genai_types

from pingone import mcp_headers

TOOL_MCP_URL = os.environ.get("TOOL_MCP_URL", "")

stripe_tool = McpToolset(
    connection_params=StreamableHTTPConnectionParams(url=TOOL_MCP_URL),
    header_provider=mcp_headers,
)

root_agent = Agent(
    model="gemini-2.5-flash",
    name="obo_agent",
    description="Financial agent that purchases Stripe products on behalf of an authenticated user.",
    instruction=(
        "You are a financial agent acting on behalf of an authenticated user. "
        "When asked to purchase a product, first call get_stripe_customer to retrieve "
        "and confirm the user's card on file, then call create_stripe_payment_intent to "
        "complete the purchase. Report the result back to the user. Always confirm "
        "purchase details with the user before calling create_stripe_payment_intent."
    ),
    tools=[stripe_tool],
    generate_content_config=genai_types.GenerateContentConfig(
        thinking_config=genai_types.ThinkingConfig(thinking_budget=0),
    ),
)

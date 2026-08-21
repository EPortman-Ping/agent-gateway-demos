"""Protected deterministic MCP server for the order-status capability."""

from datetime import datetime, timezone
from typing import Any
import os

from fastapi import FastAPI, Header, HTTPException

from protocol import ORDER_STATUS_AGENT, ORDER_STATUS_MCP_SERVER, ORDER_STATUS_TOOL, bearer_token, parse_delegated_token, validate_order_id

app = FastAPI(title="Order Status MCP Server", version="1.0.0")
MCP_AUDIENCE = os.environ.get("MCP_ORDER_STATUS_SERVER_AUDIENCE", ORDER_STATUS_MCP_SERVER)
MCP_SCOPE = os.environ.get("MCP_ORDER_STATUS_SCOPE", "order:read")

ORDERS = {
    "ORD-123": {"status": "shipped", "summary": "Order shipped and awaiting delivery."},
    "ORD-456": {"status": "processing", "summary": "Order is being prepared."},
}


def _tool_result(order_id: str) -> dict[str, Any]:
    order = ORDERS.get(order_id)
    if order is None:
        raise HTTPException(status_code=404, detail="order not found")
    return {"order_id": order_id, **order, "last_updated": datetime.now(timezone.utc).isoformat()}


@app.post("/mcp")
def mcp(body: dict[str, Any], authorization: str = Header(default="")) -> dict[str, Any]:
    method = body.get("method")
    request_id = body.get("id")
    if method == "initialize":
        return {"jsonrpc": "2.0", "id": request_id, "result": {"protocolVersion": "2025-06-18", "capabilities": {"tools": {}}}}
    if method == "tools/list":
        return {"jsonrpc": "2.0", "id": request_id, "result": {"tools": [{"name": ORDER_STATUS_TOOL, "description": "Get the current status of an order.", "inputSchema": {"type": "object", "properties": {"order_id": {"type": "string"}}, "required": ["order_id"]}}]}}
    if method != "tools/call":
        raise HTTPException(status_code=400, detail="unsupported MCP method")
    try:
        token = parse_delegated_token(bearer_token(authorization), audience=MCP_AUDIENCE, scope=MCP_SCOPE, actor=ORDER_STATUS_AGENT)
        params = body.get("params", {})
        if params.get("name") != ORDER_STATUS_TOOL:
            raise ValueError("unsupported tool")
        order_id = params.get("arguments", {}).get("order_id", "")
        validate_order_id(order_id)
        if not token.subject:
            raise ValueError("delegated token missing user subject")
    except ValueError as exc:
        raise HTTPException(status_code=401, detail=str(exc)) from exc
    result = _tool_result(order_id)
    return {"jsonrpc": "2.0", "id": request_id, "result": {"content": [{"type": "text", "text": str(result)}], "structuredContent": result}}

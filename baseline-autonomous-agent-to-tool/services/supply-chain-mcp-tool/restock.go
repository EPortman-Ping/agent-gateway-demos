package main

import (
	"context"
	"log"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// RestockInput is the typed argument schema for the `restock` tool. The MCP SDK
// derives the tool's JSON input schema from these struct tags and validates
// incoming arguments against it before our handler runs.
type RestockInput struct {
	ProductID string `json:"product_id" jsonschema:"the product SKU to restock"`
	Quantity  int    `json:"quantity" jsonschema:"number of units to order"`
	Region    string `json:"region" jsonschema:"target fulfillment region"`
}

// RestockOutput is the typed structured result of the `restock` tool.
type RestockOutput struct {
	Status    string `json:"status"`
	OrderID   string `json:"order_id"`
	ProductID string `json:"product_id"`
	Quantity  int    `json:"quantity"`
}

// handleRestock is the MCP tool handler. Input is already unmarshaled and
// validated against the schema by the SDK; we just process it and return a
// typed result, which the SDK marshals into the tool call response.
//
// This is a mock — it returns a hardcoded accepted order. The demo's point is
// the security boundary (see auth.go), not the business logic.
func handleRestock(ctx context.Context, req *mcp.CallToolRequest, in RestockInput) (*mcp.CallToolResult, RestockOutput, error) {
	log.Printf("[SupplyChain] tools/call restock — %d units of %s for region %s",
		in.Quantity, in.ProductID, in.Region)

	out := RestockOutput{
		Status:    "accepted",
		OrderID:   "ORD-20240101-001",
		ProductID: in.ProductID,
		Quantity:  in.Quantity,
	}
	// Content is left nil; the SDK fills it with JSON text mirroring the
	// structured output.
	return nil, out, nil
}

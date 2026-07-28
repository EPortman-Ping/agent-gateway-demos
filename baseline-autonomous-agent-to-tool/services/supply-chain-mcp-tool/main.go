// Supply Chain MCP Tool — an MCP server deployed on Cloud Run.
//
// This is the "tool" the agent ultimately wants to reach. It speaks the Model
// Context Protocol (MCP) over the Streamable HTTP transport and exposes a single
// `restock` tool (see restock.go).
//
// Every request passes through token validation first (see auth.go): the tool
// independently verifies the OAuth token the gateway injected before the MCP
// handler runs, so it never trusts a caller the gateway hasn't authorized.
package main

import (
	"context"
	"log"
	"net/http"
	"os"

	"github.com/joho/godotenv"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func main() {
	// Load a local .env if present (for `go run` on a laptop). Absent in Cloud
	// Run, where env vars come from the service definition — so ignore the error.
	_ = godotenv.Load()

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	validator, err := newTokenValidator(context.Background())
	if err != nil {
		log.Fatalf("token validator: %v", err)
	}

	server := mcp.NewServer(
		&mcp.Implementation{Name: "supply-chain-mcp-tool", Version: "1.0.0"},
		nil,
	)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "restock",
		Description: "Place a restock order for a product in a given region.",
	}, handleRestock)

	// Streamable HTTP transport. Every MCP request (initialize, tools/list,
	// tools/call) arrives here as HTTP and is dispatched to the server above.
	mcpHandler := mcp.NewStreamableHTTPHandler(
		func(*http.Request) *mcp.Server { return server },
		nil,
	)

	mux := http.NewServeMux()
	mux.Handle("/mcp", validator.middleware(mcpHandler))
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	log.Printf("[SupplyChain] MCP server listening on port %s (endpoint /mcp)", port)
	if err := http.ListenAndServe(":"+port, mux); err != nil {
		log.Fatalf("server: %v", err)
	}
}

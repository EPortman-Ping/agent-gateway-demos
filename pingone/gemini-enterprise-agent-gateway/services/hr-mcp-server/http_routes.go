package main

import (
	"bytes"
	"context"
	"io"
	"log"
	"net/http"

	"github.com/mark3labs/mcp-go/server"
)

func newRouter(mcpServer *server.StreamableHTTPServer) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/mcp" && r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}

		// GE sends the user OAuth token only on tools/call, not on initialize or
		// tools/list. Enforce auth only when a tool is actually being invoked.
		if isToolCall(r) {
			if err := validateToken(r.Header.Get("Authorization")); err != nil {
				log.Printf("[HRSvc] auth rejected: %v", err)
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
		}

		// Email is injected by the extension service on tools/call; empty otherwise.
		email := r.Header.Get("X-User-Email")
		ctx := context.WithValue(r.Context(), ctxKeyCallerEmail, email)
		r = r.WithContext(ctx)
		mcpServer.ServeHTTP(w, r)
	})
}

// isToolCall returns true when the request body contains a tools/call method.
// The body is restored so the MCP handler can read it again.
func isToolCall(r *http.Request) bool {
	if r.Method != http.MethodPost {
		return false
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return true
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	return bytes.Contains(body, []byte(`"tools/call"`))
}

package main

import (
	"context"
	"fmt"
	"log"
	"net/http"

	"github.com/mark3labs/mcp-go/server"
)

func newRouter(mcpServer *server.StreamableHTTPServer) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/mcp" {
			http.NotFound(w, r)
			return
		}

		authHeader := r.Header.Get("Authorization")
		if err := validateToken(authHeader); err != nil {
			log.Printf("[HRSvc] token validation failed: %v", err)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			fmt.Fprintf(w, `{"error":"invalid_token","error_description":%q}`, err.Error())
			return
		}

		// Email is injected by the extension service; empty for tool-discovery requests.
		email := r.Header.Get("X-User-Email")

		ctx := context.WithValue(r.Context(), ctxKeyCallerEmail, email)
		r = r.WithContext(ctx)
		mcpServer.ServeHTTP(w, r)
	})
}

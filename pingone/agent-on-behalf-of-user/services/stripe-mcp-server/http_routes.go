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

		sub, grantedScope, err := validateToken(authHeader)
		if err != nil {
			log.Printf("token validation failed: %v", err)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			fmt.Fprintf(w, `{"error":"invalid_token","error_description":%q}`, err.Error())
			return
		}

		email, err := resolveUserEmail(sub)
		if err != nil {
			log.Printf("user lookup failed for sub=%s: %v", sub, err)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			fmt.Fprintf(w, `{"error":"invalid_token","error_description":"could not resolve user email: %s"}`, err.Error())
			return
		}

		ctx := context.WithValue(r.Context(), ctxKeyCallerEmail, email)
		ctx = context.WithValue(ctx, ctxKeyGrantedScope, grantedScope)
		r = r.WithContext(ctx)
		mcpServer.ServeHTTP(w, r)
	})
}

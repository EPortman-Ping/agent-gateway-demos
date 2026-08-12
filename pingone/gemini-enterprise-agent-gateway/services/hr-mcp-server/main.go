package main

import (
	"log"
	"net/http"
	"strings"
)

func main() {
	log.SetFlags(0)

	idpIssuer = strings.TrimSuffix(requireEnv("IDP_ISSUER"), "/")
	rawRequiredScopes = requireEnv("IDP_REQUIRED_SCOPE")
	mcpTokenAudience = requireEnv("IDP_REQUIRED_AUDIENCE")
	if err := initIdpJwksURL(); err != nil {
		log.Fatalf("invalid IDP_ISSUER: %v", err)
	}

	usersClient := newPingOneUsersClient()
	mcpSrv := newMCPServer(usersClient)
	router := newRouter(mcpSrv)

	port := "8080"
	log.Printf("[HRSvc] listening on :%s", port)
	if err := http.ListenAndServe(":"+port, router); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
